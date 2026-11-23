package main

import (
	"context"
	"strconv"
	"time"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/downloads"
	"waygu-video/internal/media"
)

const downloadTracksKey = "offline-download-tracks-v1"

func (q *downloadQueue) setIncludeTracks(include bool) {
	if q.app.storage.SetItem(downloadTracksKey, strconv.FormatBool(include)) != nil {
		q.failure.Set("save")
		return
	}
	q.tracks.Set(include)
}

func (q *downloadQueue) trackDownload(entry downloadEntry, current source) func(context.Context, media.Stream) downloads.Tracks {
	if !entry.IncludeTracks {
		return nil
	}
	a := q.app
	currentRoute := route{source: current, item: entry.Item}
	subtitles := newOnlineSubtitlePlayback(a.scope, a, entry.Item, current.ID, true)
	subtitlePrefs, commentPrefs := a.subtitlePrefs.GetUntracked(), a.danmakuPrefs.GetUntracked()
	bindings := readDanmakuBindings(a.storage, danmakuBindingKey(currentRoute), danmakuSeasonKey(currentRoute))
	var config danmaku.LogvarConfig
	var logvarBinding danmaku.DandanEpisode
	if !commentPrefs.DisableLogvar {
		if value, err := a.logvarConfig(); err == nil && value.Address != "" {
			config = value
			logvarBinding = readDanmakuBindings(a.storage, logvarBindingKey(currentRoute, config), "").item
		}
	}
	cache := a.scope.CacheDir()
	return func(ctx context.Context, stream media.Stream) downloads.Tracks {
		ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
		defer cancel()
		comments := make(chan *downloads.CommentTrack, 1)
		go func() {
			dandan := a.downloadDanmaku(ctx, entry.Item, stream, commentPrefs, bindings, cache)
			custom := downloadLogvar(ctx, entry.Item, commentPrefs, config, logvarBinding, cache)
			if dandan == nil {
				dandan = custom
			} else if custom != nil {
				combined := appendShiftedDanmaku(nil, *dandan)
				combined = appendShiftedDanmaku(combined, *custom)
				dandan = &downloads.CommentTrack{Comments: combined}
			}
			comments <- dandan
		}()
		tracks := downloads.Tracks{AudioLanguage: activeAudio(stream.Tracks).Language}
		tracks.Subtitles = a.downloadSubtitles(ctx, stream, entry.Height > 0, subtitles, subtitlePrefs)
		tracks.Danmaku = <-comments
		return tracks
	}
}

func downloadLogvar(ctx context.Context, item media.Item, prefs danmakuPreferences, config danmaku.LogvarConfig,
	binding danmaku.DandanEpisode, cache string) *downloads.CommentTrack {
	if config.Address == "" || (binding.ID <= 0 && !prefs.AutoMatch) {
		return nil
	}
	client, err := danmaku.NewLogvarClient(config)
	if err != nil {
		return nil
	}
	match, err := findLogvarBinding(ctx, client, item, binding)
	if err != nil || match.bound.ID <= 0 {
		return nil
	}
	comments, err := client.CachedComments(ctx, cache, match.bound.ID)
	if err != nil || len(comments) == 0 {
		return nil
	}
	return &downloads.CommentTrack{Comments: comments, Shift: match.bound.Shift}
}

func (a *application) downloadSubtitles(ctx context.Context, stream media.Stream, transcoded bool, service *onlineSubtitlePlayback, prefs subtitlePreferences) []downloads.Subtitle {
	const maxSubtitleBytes = 12 << 20
	const maxSubtitles = 32
	var tracks []downloads.Subtitle
	var bytes int
	seen := make(map[string]bool)
	for _, track := range stream.Tracks {
		if ctx.Err() != nil || len(tracks) >= maxSubtitles || bytes >= maxSubtitleBytes {
			break
		}
		if !track.ReadableSubtitle() || track.ID == "" || seen[track.ID] {
			continue
		}
		request, cancel := context.WithTimeout(ctx, 10*time.Second)
		cues, err := fetchSubtitleTrack(request, stream, track)
		cancel()
		if err != nil {
			continue
		}
		text, err := cues.WebVTT(0)
		if err != nil || bytes+len(text) > maxSubtitleBytes {
			continue
		}
		bytes += len(text)
		seen[track.ID] = true
		tracks = append(tracks, downloads.Subtitle{ID: track.ID, Title: track.Title, Language: track.Language, Text: text,
			Default: track.Default, Forced: track.Forced, External: track.External || transcoded})
	}
	key := service.bind(stream)
	record, _ := service.cached(ctx, key)
	if record.cues == nil && prefs.DownloadMissing && automaticSubtitlesAllowed(stream.Tracks, prefs) {
		available := make([]media.Track, 0, len(tracks))
		for _, subtitle := range tracks {
			available = append(available, savedMediaSubtitle(subtitle))
		}
		if !hasPreferredSubtitle(available, prefs) {
			record, _ = service.downloadExact(ctx, key, stream, prefs)
		}
	}
	if record.cues != nil && len(tracks) < maxSubtitles {
		if text, err := record.cues.WebVTT(a.savedDownloadDelay(ctx, service, record)); err == nil && bytes+len(text) <= maxSubtitleBytes {
			tracks = append(tracks, downloads.Subtitle{ID: "offline-online", Title: record.Name, Language: record.Language,
				Text: text, Default: true, External: true})
		}
	}
	return tracks
}

func savedMediaSubtitle(track downloads.Subtitle) media.Track {
	return media.Track{ID: track.ID, Type: "subtitle", Title: track.Title, Language: track.Language, Codec: "webvtt",
		Default: track.Default, Forced: track.Forced, External: track.External, SavedSubtitle: &track.Text}
}

func (a *application) savedDownloadDelay(ctx context.Context, service *onlineSubtitlePlayback, record cachedOnlineSubtitle) time.Duration {
	delay := make(chan time.Duration, 1)
	a.scope.Post(func() { delay <- service.savedDelay(record) })
	select {
	case stored := <-delay:
		return stored
	case <-ctx.Done():
		return record.Delay
	}
}

func hasPreferredSubtitle(tracks []media.Track, prefs subtitlePreferences) bool {
	for _, code := range prefs.Languages {
		if textSubtitleForPreference(tracks, code, prefs.chineseCaptionScript()).ID != "" {
			return true
		}
	}
	return false
}

func (a *application) downloadDanmaku(ctx context.Context, item media.Item, stream media.Stream, prefs danmakuPreferences,
	bindings danmakuMatchBindings, cache string) *downloads.CommentTrack {
	if prefs.DisableDandan || (bindings.item.ID <= 0 && !prefs.AutoMatch) {
		return nil
	}
	credentials, err := a.dandanCredentials()
	if err != nil || credentials.AppID == "" || credentials.Secret == "" {
		return nil
	}
	client := a.newDandan(credentials)
	match, err := findDanmakuBinding(ctx, client, stream, item, bindings)
	if err != nil || match.bound.ID <= 0 {
		return nil
	}
	comments, err := client.CachedComments(ctx, cache, match.bound.ID)
	if err != nil || len(comments) == 0 {
		return nil
	}
	return &downloads.CommentTrack{Comments: comments, Shift: match.bound.Shift}
}
