package main

import (
	"strings"

	"waygu-video/internal/playback/mpv"
)

type mpvCaptionLayout struct {
	primary, secondary mpv.Track
}

func embeddedCaptionLayout(tracks []mpv.Track, prefs *subtitlePreferences) mpvCaptionLayout {
	var layout mpvCaptionLayout
	for _, track := range tracks {
		if selectedSubtitle(track) && decoderCaptionText(track, prefs.FlattenASS) {
			layout.primary = track
			break
		}
	}
	primaryLanguage := subtitleLanguage(layout.primary.Language)
	if primaryLanguage == "" || prefs.DisableBilingual {
		return layout
	}
	target := prefs.chineseCaptionScript()
	for _, code := range prefs.Languages {
		if sameSubtitleLanguage(code, primaryLanguage) {
			continue
		}
		for _, language := range subtitleLanguageFallback(code, target) {
			if track := embeddedSubtitleInLanguage(tracks, language, func(t mpv.Track) bool {
				return t.ID != layout.primary.ID && decoderCaptionText(t, prefs.FlattenASS)
			}); track.ID != 0 {
				layout.secondary = track
				return layout
			}
		}
	}
	return layout
}

func decoderCaptionText(track mpv.Track, flattenASS bool) bool {
	switch strings.ToLower(track.Codec) {
	case "ass", "ssa":
		return flattenASS
	case "srt", "subrip", "webvtt", "text", "mov_text", "microdvd", "mpl2", "jacosub", "sami", "realtext", "subviewer", "subviewer1":
		return true
	}
	return false
}

func isASSSubtitle(track mpv.Track) bool {
	return strings.EqualFold(track.Codec, "ass") || strings.EqualFold(track.Codec, "ssa")
}

func embeddedSubtitleInLanguage(tracks []mpv.Track, language string, include func(mpv.Track) bool) mpv.Track {
	var best mpv.Track
	for _, track := range tracks {
		if track.Kind == mpv.SubtitleTrack && subtitleLanguage(track.Language) == language &&
			(include == nil || include(track)) && (best.ID == 0 || track.Default && !best.Default) {
			best = track
		}
	}
	return best
}

func (layout mpvCaptionLayout) captions(player *mpv.Player) activeCaptionLines {
	var cue activeCaptionLines
	if layout.primary.ID != 0 {
		cue.primary, cue.primaryLanguage = player.SubtitleText(false), layout.primary.Language
	}
	if layout.secondary.ID != 0 {
		cue.secondary, cue.secondaryLanguage = player.SubtitleText(true), layout.secondary.Language
	}
	return cue
}
