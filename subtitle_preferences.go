package main

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"golang.org/x/text/language"

	"waygu-video/internal/media"
)

const subtitlePreferencesKey = "subtitle-preferences-v1"

var subtitleLanguages = []string{"zh-Hans", "zh-Hant", "en", "ja", "ko", "fr", "es"}
var subtitleColors = []uint32{0xffffff, 0xfde68a, 0xfde047, 0x7dd3fc, 0x6ee7b7, 0xf9a8d4}
var subtitleBackgrounds = []string{"none", "dim", "solid"}
var subtitlePositions = []int{6, 12, 20}
var subtitleScales = []int{50, 75, 100, 125, 150}
var subtitleModes = []string{"always", "foreign", "off"}

type subtitlePreferences struct {
	Languages           []string
	Audio               string
	Mode                string
	Color               uint32
	Scale               int
	Position            int
	Background          string
	KeepOriginalChinese bool
	DisableBilingual    bool
}

func readSubtitlePreferences(storage core.Storage) subtitlePreferences {
	p := subtitlePreferences{Languages: []string{"zh-Hans", "zh-Hant", "en"},
		Mode: "foreign", Color: 0xffffff, Scale: 100, Position: 6, Background: "none"}
	raw, ok := storage.GetItem(subtitlePreferencesKey)
	if !ok {
		return p
	}
	saved := subtitlePreferences{Color: p.Color}
	if json.Unmarshal([]byte(raw), &saved) != nil {
		return p
	}
	if len(saved.Languages) > 0 {
		var order []string
		for _, code := range saved.Languages {
			if slices.Contains(subtitleLanguages, code) && !slices.Contains(order, code) {
				order = append(order, code)
			}
		}
		if len(order) > 0 {
			p.Languages = order
		}
	}
	if slices.Contains(subtitleLanguages, saved.Audio) {
		p.Audio = saved.Audio
	}
	if slices.Contains(subtitleModes, saved.Mode) {
		p.Mode = saved.Mode
	}
	if saved.Color <= 0xffffff {
		p.Color = saved.Color
	}
	if slices.Contains(subtitleScales, saved.Scale) {
		p.Scale = saved.Scale
	}
	if slices.Contains(subtitlePositions, saved.Position) {
		p.Position = saved.Position
	}
	if slices.Contains(subtitleBackgrounds, saved.Background) {
		p.Background = saved.Background
	}
	p.KeepOriginalChinese = saved.KeepOriginalChinese
	p.DisableBilingual = saved.DisableBilingual
	return p
}

func (p subtitlePreferences) chineseCaptionScript() string {
	if !p.KeepOriginalChinese {
		for _, code := range p.Languages {
			if code == "zh-Hans" || code == "zh-Hant" {
				return code
			}
		}
	}
	return ""
}

func subtitleLanguage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chi", "zho", "chs", "sc", "zh-sg":
		return "zh-Hans"
	case "cht", "tc":
		return "zh-Hant"
	}
	tag, err := language.Parse(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
	if err != nil {
		return ""
	}
	base, _, _ := tag.Raw()
	if base.String() == "zh" {
		script, _ := tag.Script()
		if script.String() == "Hant" {
			return "zh-Hant"
		}
		return "zh-Hans"
	}
	if base.String() == "und" {
		return ""
	}
	canonical, err := language.All.Canonicalize(tag)
	if err == nil {
		base, _ = canonical.Base()
	}
	return base.String()
}

func activeAudio(tracks []media.Track) media.Track {
	var current media.Track
	for _, track := range tracks {
		if track.Type != "audio" {
			continue
		}
		if track.Selected {
			return track
		}
		if current.ID == "" || track.Default {
			current = track
		}
	}
	return current
}

func preferredAudio(tracks []media.Track, code string) *int {
	if code == "" || subtitleLanguage(activeAudio(tracks).Language) == code {
		return nil
	}
	audioIndex := -1
	for _, track := range tracks {
		if track.Type != "audio" || subtitleLanguage(track.Language) != code {
			continue
		}
		index, err := strconv.Atoi(track.ID)
		if err == nil && index >= 0 && (audioIndex < 0 || track.Default) {
			audioIndex = index
		}
	}
	if audioIndex >= 0 {
		return &audioIndex
	}
	return nil
}

func automaticSubtitle(tracks []media.Track, p subtitlePreferences) media.Track {
	if p.Mode == "off" {
		return media.Track{}
	}
	if p.Mode == "foreign" {
		if p.Audio == "" {
			return media.Track{}
		}
		audio := activeAudio(tracks)
		if lang := subtitleLanguage(audio.Language); lang == "" || lang == p.Audio {
			return media.Track{}
		}
	}
	target := p.chineseCaptionScript()
	for _, code := range p.Languages {
		if match := textSubtitleForPreference(tracks, code, target); match.ID != "" {
			return match
		}
	}
	return media.Track{}
}

func textSubtitleForPreference(tracks []media.Track, code, target string) media.Track {
	if match := textSubtitleInLanguage(tracks, code); match.ID != "" || code != target {
		return match
	}
	switch code {
	case "zh-Hant":
		return textSubtitleInLanguage(tracks, "zh-Hans")
	case "zh-Hans":
		return textSubtitleInLanguage(tracks, "zh-Hant")
	default:
		return media.Track{}
	}
}

func textSubtitleInLanguage(tracks []media.Track, code string) media.Track {
	var match media.Track
	for _, track := range tracks {
		if track.Type == "subtitle" && track.ID != "" && track.URL != "" && subtitleLanguage(track.Language) == code &&
			(match.ID == "" || track.Default && !match.Default) {
			match = track
		}
	}
	return match
}

func (p subtitlePreferences) captionStyle() core.Style {
	style := core.Style{Color: core.RGB(p.Color), FontSize: core.Points(20 * float64(p.Scale) / 100)}
	switch p.Background {
	case "dim":
		style.Background = core.RGBA(0, 0.45)
	case "solid":
		style.Background = core.RGB(0x000000)
	}
	style.TextShadowColor = core.RGBA(0, 0.9)
	style.TextShadowOffset = core.Point{Y: 1}
	style.TextShadowRadius = 2
	return style
}

func (p subtitlePreferences) secondaryCaptionStyle() core.Style {
	style := p.captionStyle()
	style.FontSize *= 0.7
	style.Color = core.RGB(0xfde68a)
	return style
}
