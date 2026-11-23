package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/danmaku"
	"waygu-video/locales"
)

const danmakuPreferencesKey = "danmaku-preferences-v1"

type danmakuPreferences struct {
	Opacity, FontScale, Speed, Density, Area, Outline       int
	Enabled, AutoMatch, Bold, ReserveSubtitles, Deduplicate bool
	Scrolling, Top, Bottom, Colored                         bool
	DisableDandan, DisableLogvar, DisableLocal              bool
	Keywords, Patterns, BlockedUsers                        string
	PromptOnMiss                                            bool
}

func readDanmakuPreferences(storage core.Storage) danmakuPreferences {
	defaults := danmaku.DefaultSettings()
	value := danmakuPreferences{
		Opacity: int(defaults.Opacity * 100), FontScale: 100, Speed: 100, Density: defaults.MaxOnScreen,
		Area: 50, Outline: 1, Enabled: true, AutoMatch: true, Bold: defaults.Bold,
		ReserveSubtitles: defaults.ReserveSubtitles, Deduplicate: defaults.Deduplicate,
		Scrolling: true, Top: true, Bottom: true, Colored: true,
	}
	if data, ok := storage.GetItem(danmakuPreferencesKey); ok {
		saved := value
		if json.Unmarshal([]byte(data), &saved) == nil {
			value = saved
		}
	}
	value.Opacity = min(100, max(10, value.Opacity))
	value.FontScale = min(150, max(50, value.FontScale))
	value.Speed = min(200, max(50, value.Speed))
	value.Density = min(80, max(5, value.Density))
	value.Outline = min(2, max(0, value.Outline))
	switch value.Area {
	case 25, 50, 75, 100:
	default:
		value.Area = 50
	}
	return value
}

func (p danmakuPreferences) rendererSettings(offset time.Duration) danmaku.Settings {
	settings := danmaku.DefaultSettings()
	settings.FontSize = settings.FontSize * p.FontScale / 100
	settings.Opacity = float64(p.Opacity) / 100
	settings.Speed = float64(p.Speed) / 100
	settings.Area = float64(p.Area) / 100
	settings.MaxOnScreen = p.Density
	settings.Offset = offset
	settings.Outline = float64(p.Outline) * 1.5
	settings.Bold, settings.ReserveSubtitles, settings.Deduplicate = p.Bold, p.ReserveSubtitles, p.Deduplicate
	settings.HideScrolling, settings.HideTop, settings.HideBottom, settings.HideColored = !p.Scrolling, !p.Top, !p.Bottom, !p.Colored
	settings.Keywords = danmakuRules(p.Keywords)
	if users := danmakuRules(p.BlockedUsers); len(users) > 0 {
		settings.BlockedUsers = make(map[string]bool, len(users))
		for _, user := range users {
			settings.BlockedUsers[user] = true
		}
	}
	for _, pattern := range danmakuRules(p.Patterns) {
		if compiled, err := regexp.Compile(pattern); err == nil {
			settings.Patterns = append(settings.Patterns, compiled)
		}
	}
	return settings
}

func danmakuRules(input string) []string {
	var rules []string
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(input, "\n") {
		if rule := strings.TrimSpace(line); rule != "" && !seen[rule] {
			rules = append(rules, rule)
			seen[rule] = true
		}
	}
	return rules
}

type danmakuEditor struct {
	prefs  *core.Signal[danmakuPreferences]
	failed *core.Signal[bool]
	save   func(func(*danmakuPreferences)) bool
}

func (a *application) editDanmaku(cx *core.Scope) danmakuEditor {
	failed := core.NewSignal(cx, false)
	return danmakuEditor{prefs: a.danmakuPrefs, failed: failed, save: func(edit func(*danmakuPreferences)) bool {
		current := a.danmakuPrefs.GetUntracked()
		next := current
		edit(&next)
		if next == current {
			return true
		}
		data, err := json.Marshal(next)
		if err == nil {
			err = a.storage.SetItem(danmakuPreferencesKey, string(data))
		}
		failed.Set(err != nil)
		if err == nil {
			a.danmakuPrefs.Set(next)
		}
		return err == nil
	}}
}

func (e danmakuEditor) warning() core.Node {
	return core.Show{When: e.failed.Get, Then: func(cx *core.Scope) core.Node {
		return notice(locales.DanmakuPreferencesError(cx))
	}}
}

func (e danmakuEditor) toggle(label, id string, field func(*danmakuPreferences) *bool) core.Node {
	return ui.Checkbox{Text: label, TestID: id,
		Checked:  func() bool { p := e.prefs.Get(); return *field(&p) },
		OnChange: func(value bool) { e.save(func(p *danmakuPreferences) { *field(p) = value }) }}
}

type danmakuAdjustment struct {
	label, id      string
	min, max, step float64
	field          func(*danmakuPreferences) *int
	format         func(float64) string
}

func danmakuAdjustments(cx *core.Scope) []danmakuAdjustment {
	percent := func(value float64) string { return locales.DanmakuPercent(cx, strconv.Itoa(int(value))) }
	count := func(value float64) string { return strconv.Itoa(int(value)) }
	return []danmakuAdjustment{
		{locales.DanmakuOpacity(cx), "danmaku-opacity", 10, 100, 5, func(p *danmakuPreferences) *int { return &p.Opacity }, percent},
		{locales.DanmakuFontSize(cx), "danmaku-font-size", 50, 150, 5, func(p *danmakuPreferences) *int { return &p.FontScale }, percent},
		{locales.DanmakuSpeed(cx), "danmaku-speed", 50, 200, 10, func(p *danmakuPreferences) *int { return &p.Speed }, percent},
		{locales.DanmakuDensity(cx), "danmaku-density", 5, 80, 1, func(p *danmakuPreferences) *int { return &p.Density }, count},
	}
}

func (e danmakuEditor) display(cx *core.Scope, visibility, offset core.Node) core.Node {
	adjustments := danmakuAdjustments(cx)
	rows := []core.Node{visibility, offset}
	sliders := make([]core.Node, 0, len(adjustments))
	for _, spec := range adjustments {
		value := func() float64 { prefs := e.prefs.Get(); return float64(*spec.field(&prefs)) }
		change := func(value float64) { e.save(func(p *danmakuPreferences) { *spec.field(p) = int(value) }) }
		var control core.Node
		if cx.Env().Idiom() == core.IdiomTV {
			adjust := func(delta float64) {
				p := e.prefs.GetUntracked()
				change(min(spec.max, max(spec.min, float64(*spec.field(&p))+delta)))
			}
			control = core.View{Class: "flex-row gap-3", Children: []core.Node{
				ui.IconButton{Label: locales.DanmakuDecrease(cx, spec.label), Icon: lucide.Minus, Size: 44, TestID: spec.id + "-decrease",
					OnPress: func() { adjust(-spec.step) }, Disabled: func() bool { return value() <= spec.min }},
				ui.IconButton{Label: locales.DanmakuIncrease(cx, spec.label), Icon: lucide.Plus, Size: 44, TestID: spec.id + "-increase",
					OnPress: func() { adjust(spec.step) }, Disabled: func() bool { return value() >= spec.max }},
			}}
		} else {
			control = ui.Slider{Min: spec.min, Max: spec.max, Step: spec.step, Value: value, ValueText: spec.format, Label: spec.label,
				TestID: spec.id, OnChange: change}
		}
		sliders = append(sliders, core.View{Class: "gap-1", Children: []core.Node{
			core.View{Class: "flex-row items-center justify-between gap-3", Children: []core.Node{
				core.Text{Content: spec.label, Class: "text-sm font-medium"},
				core.Text{ContentFunc: func() string { return spec.format(value()) }, Class: "text-sm text-muted-foreground"},
			}},
			control,
		}})
	}
	rows = append(rows,
		core.View{Class: "grid grid-cols-2 gap-4", Children: sliders},
		core.Text{Content: locales.DanmakuArea(cx), Class: "text-sm font-medium"},
		playbackChoice(cx, locales.DanmakuArea(cx), "danmaku-area", []ui.Option[int]{
			{Label: "¼", Value: 25}, {Label: "½", Value: 50}, {Label: "¾", Value: 75}, {Label: locales.DanmakuFullArea(cx), Value: 100},
		}, func() int { return e.prefs.Get().Area }, func(value int) { e.save(func(p *danmakuPreferences) { p.Area = value }) }),
		core.Text{Content: locales.DanmakuOutline(cx), Class: "text-sm font-medium"},
		playbackChoice(cx, locales.DanmakuOutline(cx), "danmaku-outline", []ui.Option[int]{
			{Label: locales.DanmakuOutlineNone(cx), Value: 0}, {Label: locales.DanmakuOutlineRegular(cx), Value: 1}, {Label: locales.DanmakuOutlineStrong(cx), Value: 2},
		}, func() int { return e.prefs.Get().Outline }, func(value int) { e.save(func(p *danmakuPreferences) { p.Outline = value }) }),
		e.toggle(locales.DanmakuBold(cx), "danmaku-bold", func(p *danmakuPreferences) *bool { return &p.Bold }),
		e.toggle(locales.DanmakuReserveSubtitles(cx), "danmaku-reserve-subtitles", func(p *danmakuPreferences) *bool { return &p.ReserveSubtitles }),
		core.Text{Content: locales.DanmakuTypes(cx), Class: "text-sm font-medium"},
		core.View{Class: "flex-row flex-wrap gap-3", Children: []core.Node{
			e.toggle(locales.DanmakuScrolling(cx), "danmaku-scrolling", func(p *danmakuPreferences) *bool { return &p.Scrolling }),
			e.toggle(locales.DanmakuTop(cx), "danmaku-top", func(p *danmakuPreferences) *bool { return &p.Top }),
			e.toggle(locales.DanmakuBottom(cx), "danmaku-bottom", func(p *danmakuPreferences) *bool { return &p.Bottom }),
			e.toggle(locales.DanmakuColored(cx), "danmaku-colored", func(p *danmakuPreferences) *bool { return &p.Colored }),
		}})
	return core.View{Class: "gap-3", Children: rows}
}

func (e danmakuEditor) filters(cx *core.Scope) core.Node {
	return core.View{Class: "gap-3", Children: []core.Node{
		e.filterEditor(cx, nil),
		e.toggle(locales.DanmakuDeduplicate(cx), "danmaku-deduplicate", func(p *danmakuPreferences) *bool { return &p.Deduplicate }),
	}}
}

func (e danmakuEditor) filterEditor(cx *core.Scope, saved func()) core.Node {
	prefs := e.prefs.GetUntracked()
	keywords := core.NewSignal(cx, prefs.Keywords)
	patterns := core.NewSignal(cx, prefs.Patterns)
	badPattern := core.NewSignal(cx, "")
	save := func() {
		words := strings.Join(danmakuRules(keywords.GetUntracked()), "\n")
		rules := danmakuRules(patterns.GetUntracked())
		for _, rule := range rules {
			if _, err := regexp.Compile(rule); err != nil {
				badPattern.Set(rule)
				return
			}
		}
		badPattern.Set("")
		regex := strings.Join(rules, "\n")
		if e.save(func(p *danmakuPreferences) { p.Keywords, p.Patterns = words, regex }) {
			keywords.Set(words)
			patterns.Set(regex)
			if saved != nil {
				saved()
			}
		}
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		core.Text{Content: locales.DanmakuKeywords(cx), Class: "text-sm font-medium"},
		ui.Input{Label: locales.DanmakuKeywords(cx), Placeholder: locales.DanmakuOneRulePerLine(cx),
			Multiline: true, NoAutoCorrect: true, Value: keywords.Get, OnChange: keywords.Set, TestID: "danmaku-keywords",
			Style: core.Style{Height: core.Pt(66)}},
		core.Text{Content: locales.DanmakuPatterns(cx), Class: "text-sm font-medium"},
		ui.Input{Label: locales.DanmakuPatterns(cx), Placeholder: locales.DanmakuOneRulePerLine(cx),
			Multiline: true, NoAutoCorrect: true, Value: patterns.Get, TestID: "danmaku-patterns",
			OnChange: func(value string) { patterns.Set(value); badPattern.Set("") }, Style: core.Style{Height: core.Pt(66)}},
		ui.Button{Text: locales.DanmakuSaveFilters(cx), TestID: "danmaku-save-filters", OnPress: save},
		core.Show{When: func() bool { return badPattern.Get() != "" }, Then: func(cx *core.Scope) core.Node {
			return core.Text{ContentFunc: func() string { return locales.DanmakuInvalidPattern(cx, badPattern.Get()) },
				Class: "text-sm", StyleFunc: func() core.Style { return core.Style{Color: cx.Palette().Danger} }}
		}},
	}}
}
