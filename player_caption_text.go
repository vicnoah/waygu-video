package main

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
	"waygu-video/internal/playback"
)

type captionLines struct {
	primary, secondary string
}

type activeCaptionLines struct {
	captionLines
	primaryLanguage, secondaryLanguage string
}

func fetchSubtitleTrack(ctx context.Context, stream media.Stream, track media.Track) (*playback.Captions, error) {
	if track.SavedSubtitle != nil {
		return playback.ReadCaptions(strings.NewReader(*track.SavedSubtitle))
	}
	if file := track.FileSubtitle; track.URL == "" && file != nil {
		if file.Container == "matroska" {
			return playback.FetchMatroskaCaptions(ctx, stream.Original(), stream.File.Size, file.Index)
		}
		return playback.FetchMP4Captions(ctx, stream.Original(), stream.File.Size, file.Index)
	}
	return playback.FetchCaptions(ctx, track.URL, stream.SubtitleHeaders, stream.Trust)
}

func playerCaptionText(cx *core.Scope, subtitles *core.Signal[subtitleSelection], stream func() media.Stream, prefs func() subtitlePreferences,
	position, delay *core.Signal[time.Duration], embedded func() activeCaptionLines) *core.Memo[captionLines] {
	bilingual := core.NewMemo(cx, func() bool { return !prefs().DisableBilingual })
	chinese := core.NewMemo(cx, func() string { return prefs().chineseCaptionScript() })
	order := core.NewMemoFunc(cx, func() []string { return prefs().Languages }, slices.Equal[[]string])
	choice := core.NewMemo(cx, func() media.Track {
		if primary := subtitles.Get(); bilingual.Get() && primary.cues != nil && primary.language != "" {
			return secondarySubtitleTrack(stream().Tracks, primary.language, order.Get(), chinese.Get())
		}
		return media.Track{}
	})

	type request struct {
		track  media.Track
		stream media.Stream
	}
	type downloadedTrack struct {
		track media.Track
		cues  *playback.Captions
	}
	companion := core.NewResource(cx, func() (request, bool) {
		track := choice.Get()
		if track.ID == "" {
			return request{}, false
		}
		return request{track, stream()}, true
	}, func(ctx context.Context, req request, _ core.ResourceInfo) (downloadedTrack, error) {
		cues, err := fetchSubtitleTrack(ctx, req.stream, req.track)
		return downloadedTrack{req.track, cues}, err
	})
	active := core.NewMemo(cx, func() activeCaptionLines {
		selection := subtitles.Get()
		if selection.cues == nil {
			return embedded()
		}
		at := position.Get() - delay.Get()
		cues := activeCaptionLines{primaryLanguage: selection.language}
		cues.primary = selection.cues.At(at)
		if track := choice.Get(); track.ID != "" && companion.State() == core.ResourceReady {
			if second, ok := companion.Get(); ok && second.track == track {
				cues.secondary = second.cues.At(at)
				cues.secondaryLanguage = second.track.Language
			}
		}
		return cues
	})
	return core.NewMemo(cx, func() captionLines {
		cues := active.Get()
		if bilingual.Get() {
			if first, second, ok := splitCaptionScripts(cues.primary); ok {
				cues.primary, cues.secondary = first, second
				cues.secondaryLanguage = cues.primaryLanguage
			}
		}
		target := chinese.Get()
		return captionLines{convertSubtitleText(cues.primary, cues.primaryLanguage, target),
			convertSubtitleText(cues.secondary, cues.secondaryLanguage, target)}
	})
}

func secondarySubtitleTrack(tracks []media.Track, primaryLanguage string, order []string, target string) media.Track {
	primary := subtitleLanguage(primaryLanguage)
	if primary == "" {
		return media.Track{}
	}
	for _, code := range order {
		if sameSubtitleLanguage(code, primary) {
			continue
		}
		if track := textSubtitleForPreference(tracks, code, target); track.ID != "" {
			return track
		}
	}
	return media.Track{}
}

func sameSubtitleLanguage(a, b string) bool {
	return a == b || strings.HasPrefix(a, "zh-") && strings.HasPrefix(b, "zh-")
}

func convertSubtitleText(text, sourceLanguage, target string) string {
	source := subtitleLanguage(sourceLanguage)
	if target == "" || text == "" || source != "" && !strings.HasPrefix(source, "zh-") {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if captionScript(line) == unicode.Han {
			lines[i] = playback.ConvertChinese(line, target)
		}
	}
	return strings.Join(lines, "\n")
}

// Keep multiline dialogue together unless its lines use distinct scripts.
func splitCaptionScripts(text string) (first, second string, split bool) {
	var top, bottom []string
	var primary *unicode.RangeTable
	for line := range strings.SplitSeq(text, "\n") {
		script := captionScript(line)
		if primary == nil {
			primary = script
		}
		if script == nil || script == primary {
			top = append(top, line)
		} else {
			bottom = append(bottom, line)
		}
	}
	if len(bottom) == 0 {
		return text, "", false
	}
	return strings.Join(top, "\n"), strings.Join(bottom, "\n"), true
}

func captionScript(line string) *unicode.RangeTable {
	var script *unicode.RangeTable
	for _, char := range line {
		switch {
		case unicode.In(char, unicode.Hiragana, unicode.Katakana):
			return unicode.Hiragana
		case unicode.Is(unicode.Hangul, char):
			return unicode.Hangul
		case unicode.Is(unicode.Han, char):
			script = unicode.Han
		case script == nil && unicode.Is(unicode.Latin, char):
			script = unicode.Latin
		}
	}
	return script
}
