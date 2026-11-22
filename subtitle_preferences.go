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
	Languages  []string
	Audio      string
	Mode       string
	Color      uint32
	Scale      int
	Position   int
	Background string
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
	return p
}

func subtitleLanguage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chs", "sc", "zh-sg":
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
	for _, code := range p.Languages {
		var match media.Track
		for _, t := range tracks {
			if t.Type == "subtitle" && t.URL != "" && subtitleLanguage(t.Language) == code {
				if match.ID == "" || (t.Default && !match.Default) {
					match = t
				}
			}
		}
		if match.ID != "" {
			return match
		}
	}
	return media.Track{}
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
