package main

import (
	"cmp"
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"waygu-video/internal/media"
	"waygu-video/internal/playback"
	"waygu-video/internal/playback/audiomarker"
)

type audioMarkerRequest struct {
	key       markerSource
	stream    media.Stream
	kinds     []media.MarkerKind
	available parentalPolicy
}

type audioMarkerLookup struct {
	scope   string
	item    string
	version markerSource
	window  audioMarkerWindow
}

type audioMarkerCache struct {
	mu      sync.Mutex
	order   []audioMarkerLookup
	samples map[audioMarkerLookup]audiomarker.Fingerprint
}

type audioMarkerWindow struct{ start, span time.Duration }

func (cache *audioMarkerCache) get(key audioMarkerLookup) audiomarker.Fingerprint {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.samples[key]
}

func (cache *audioMarkerCache) put(key audioMarkerLookup, sample audiomarker.Fingerprint) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.samples == nil {
		cache.samples = make(map[audioMarkerLookup]audiomarker.Fingerprint)
	}
	if _, exists := cache.samples[key]; exists {
		return
	}
	if len(cache.order) == 24 {
		delete(cache.samples, cache.order[0])
		cache.order = cache.order[1:]
	}
	cache.order = append(cache.order, key)
	cache.samples[key] = sample
}

func audioWindow(kind media.MarkerKind, duration time.Duration) audioMarkerWindow {
	if kind == media.MarkerIntro {
		return audioMarkerWindow{0, min(duration/3, 3*time.Minute)}
	}
	span := min(duration/4, 90*time.Second)
	return audioMarkerWindow{duration - span, span}
}

func sourceForMarkers(stream media.Stream) markerSource {
	return markerSource{stream.VersionID(), stream.SourceID, stream.Duration}
}

func audioPeers(ctx context.Context, client media.Client, item media.Item, allowed func(media.Item) bool) ([]media.Item, error) {
	seasons, err := client.Children(ctx, media.Item{ID: item.SeriesID, Kind: media.Series})
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(seasons, func(season media.Item) bool { return season.Kind == media.Season && season.Season == item.Season })
	if index < 0 {
		return nil, nil
	}
	episodes, err := client.Children(ctx, seasons[index])
	if err != nil {
		return nil, err
	}
	episodes = slices.DeleteFunc(episodes, func(peer media.Item) bool {
		return peer.Kind != media.Episode || peer.ID == "" || peer.ID == item.ID || peer.Episode <= 0 || peer.Episode == item.Episode || !allowed(peer)
	})
	distance := func(peer media.Item) int { return max(peer.Episode-item.Episode, item.Episode-peer.Episode) }
	slices.SortStableFunc(episodes, func(first, second media.Item) int { return cmp.Compare(distance(first), distance(second)) })
	peers := make([]media.Item, 0, 2)
	for _, peer := range episodes {
		if len(peers) == 0 || (peers[0].Episode != peer.Episode && peers[0].ID != peer.ID) {
			peers = append(peers, peer)
		}
		if len(peers) == 2 {
			break
		}
	}
	return peers, nil
}

func desktopAudioDecoder() string {
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		path, _ := exec.LookPath("ffmpeg")
		return path
	}
	return ""
}

func (a *application) inferredMarkers(ctx context.Context, current route, client media.Client, request audioMarkerRequest) []media.PlaybackMarker {
	provider, _ := client.(media.AudioSampleClient)
	decoder := ""
	if provider == nil {
		decoder = desktopAudioDecoder()
		if decoder == "" {
			return nil
		}
	}
	var kinds []media.MarkerKind
	for _, kind := range request.kinds {
		if audioWindow(kind, request.key.duration).span >= 8*time.Second {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return nil
	}
	peers, err := audioPeers(ctx, client, current.item, func(peer media.Item) bool {
		if peer.LibraryID == "" {
			peer.LibraryID = current.item.LibraryID
		}
		return request.available.visible(current.source.ID, peer)
	})
	if err != nil || len(peers) != 2 {
		return nil
	}
	type episodeSamples map[media.MarkerKind]audiomarker.Fingerprint
	var data [3]episodeSamples
	var group sync.WaitGroup
	for i, episode := range append([]media.Item{current.item}, peers...) {
		collect := func() {
			stream := request.stream
			if i != 0 {
				peerStream, err := client.Playback(ctx, episode, media.PlaybackOptions{})
				if err != nil {
					return
				}
				stream = peerStream
			}
			data[i] = a.audioMarkersForEpisode(ctx, current.source, provider, decoder, episode, stream, kinds)
		}
		if decoder == "" {
			group.Go(collect)
		} else {
			// Limit desktop extraction to one decoder while the video is playing.
			collect()
		}
	}
	group.Wait()
	var markers []media.PlaybackMarker
	for _, kind := range kinds {
		match, found := audiomarker.Shared(ctx, data[0][kind], data[1][kind], data[2][kind])
		if !found {
			continue
		}
		window := audioWindow(kind, request.key.duration)
		if match.Start < 250*time.Millisecond && window.span-match.End <= 250*time.Millisecond {
			continue
		}
		end := window.start + match.End
		if kind == media.MarkerCredits && request.key.duration-end <= 250*time.Millisecond {
			end = request.key.duration
		}
		markers = append(markers, media.PlaybackMarker{Kind: kind, Start: window.start + match.Start, End: end})
	}
	return markers
}

func (a *application) audioMarkersForEpisode(ctx context.Context, source source, provider media.AudioSampleClient, decoder string,
	item media.Item, stream media.Stream, kinds []media.MarkerKind) map[media.MarkerKind]audiomarker.Fingerprint {
	result := make(map[media.MarkerKind]audiomarker.Fingerprint, len(kinds))
	for _, kind := range kinds {
		window := audioWindow(kind, stream.Duration)
		if window.span < 8*time.Second {
			continue
		}
		key := audioMarkerLookup{source.Address + "\x00" + source.ID + "\x00" + source.AccountID, item.ID, sourceForMarkers(stream), window}
		sample := a.audioSamples.get(key)
		if sample == nil {
			sample = readMarkerAudio(ctx, provider, decoder, item, stream, window)
			if len(sample) == 0 {
				continue
			}
			a.audioSamples.put(key, sample)
		}
		result[kind] = sample
	}
	return result
}

func readMarkerAudio(ctx context.Context, provider media.AudioSampleClient, decoder string,
	item media.Item, stream media.Stream, window audioMarkerWindow) audiomarker.Fingerprint {
	attempts := 1
	if decoder != "" {
		attempts = 2
	}
	for range attempts {
		if ctx.Err() != nil {
			break
		}
		reader, err := openMarkerAudio(ctx, provider, decoder, item, stream, window)
		if err != nil {
			continue
		}
		sample, err := audiomarker.ReadWAV(ctx, reader, window.span)
		_ = reader.Close()
		if err == nil && len(sample) > 0 && ctx.Err() == nil {
			return sample
		}
	}
	return nil
}

func openMarkerAudio(ctx context.Context, provider media.AudioSampleClient, decoder string,
	item media.Item, stream media.Stream, window audioMarkerWindow) (io.ReadCloser, error) {
	if provider != nil {
		return provider.OpenAudioSample(ctx, item, stream, window.start)
	}
	if stream.File.URL == "" {
		return nil, media.ErrNoStream
	}
	address, closeProxy, err := playback.OpenWithReadLimit(stream.File.URL, stream.File.Headers, 24<<20, stream.Trust)
	if err != nil {
		return nil, err
	}
	windowContext, cancel := context.WithCancel(ctx)
	seconds := func(at time.Duration) string { return strconv.FormatFloat(at.Seconds(), 'f', 3, 64) }
	command := exec.CommandContext(windowContext, decoder, "-nostdin", "-v", "error", "-filter_threads", "1", "-protocol_whitelist", "http,tcp",
		"-ss", seconds(window.start), "-threads", "1", "-i", address, "-t", seconds(window.span), "-vn", "-sn", "-dn",
		"-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "wav", "pipe:1")
	stdout, err := command.StdoutPipe()
	if err == nil {
		err = command.Start()
	}
	if err != nil {
		cancel()
		closeProxy()
		if stdout != nil {
			_ = stdout.Close()
		}
		return nil, err
	}
	return &markerAudioProcess{Reader: stdout, stop: func() error {
		cancel()
		closeProxy()
		return errors.Join(stdout.Close(), command.Wait())
	}}, nil
}

type markerAudioProcess struct {
	io.Reader
	stop func() error
}

func (stream *markerAudioProcess) Close() error { return stream.stop() }
