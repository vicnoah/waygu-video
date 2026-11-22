package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func (a *application) desktopPlayerDanmakuPanel(cx *core.Scope, p desktopPlayerControls) core.Node {
	editor := a.editDanmaku(cx)
	counter := func() string {
		return locales.DesktopDanmakuLoaded(cx, strconv.Itoa(p.comments.count()))
	}
	origins := func() string {
		local, remote := p.comments.rawCounts()
		var counts []string
		if local > 0 {
			counts = append(counts, locales.DesktopDanmakuLocalCount(cx, strconv.Itoa(local)))
		}
		if remote > 0 {
			counts = append(counts, locales.DesktopDanmakuRemoteCount(cx, strconv.Itoa(remote)))
		}
		return desktopMovieFacts(counts...)
	}
	rematch := locales.DesktopDanmakuRematch(cx)
	if p.current.downloadID != "" {
		rematch = locales.AddDanmaku(cx)
	}
	card := core.View{Class: "gap-1 rounded-xl border border-white/10 bg-white/5 p-3", Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
			core.Text{ContentFunc: counter, Class: "text-sm font-semibold text-white", TestID: "desktop-danmaku-count"},
			base.Button{Label: rematch, TestID: "desktop-danmaku-rematch", Class: "shrink-0 rounded",
				Style: core.Style{FocusVisible: ui.FocusRing(cx, 1)}, OnPress: func(core.PressEvent) {
					if p.current.downloadID != "" {
						p.comments.importFile()
						return
					}
					p.comments.openMatch()
				}, Children: []core.Node{core.Text{Content: rematch, Class: "text-xs text-violet-400"}}},
		}},
		core.Show{When: func() bool { return origins() != "" }, Then: func(*core.Scope) core.Node {
			return core.Text{ContentFunc: origins, Class: "text-xs text-zinc-500"}
		}},
	}}
	modes := []struct {
		id, label string
		read      func(danmakuPreferences) bool
		change    func(*danmakuPreferences)
	}{
		{"scroll", locales.DanmakuScrolling(cx), func(p danmakuPreferences) bool { return p.Scrolling }, func(p *danmakuPreferences) { p.Scrolling = !p.Scrolling }},
		{"top", locales.DanmakuTop(cx), func(p danmakuPreferences) bool { return p.Top }, func(p *danmakuPreferences) { p.Top = !p.Top }},
		{"bottom", locales.DanmakuBottom(cx), func(p danmakuPreferences) bool { return p.Bottom }, func(p *danmakuPreferences) { p.Bottom = !p.Bottom }},
		{"colored", locales.DanmakuColored(cx), func(p danmakuPreferences) bool { return p.Colored }, func(p *danmakuPreferences) { p.Colored = !p.Colored }},
	}
	var types []core.Node
	for _, mode := range modes {
		types = append(types, base.Button{Label: mode.label, TestID: "desktop-danmaku-filter-" + mode.id,
			Selected: func() bool { return mode.read(a.danmakuPrefs.Get()) },
			Class:    "group h-8 flex-1 items-center justify-center rounded-lg border border-white/10 bg-white/5 selected:border-violet-500/40 selected:bg-violet-500/20",
			Style:    core.Style{FocusVisible: ui.FocusRing(cx, 1)}, OnPress: func(core.PressEvent) { editor.save(mode.change) },
			Children: []core.Node{core.Text{Content: mode.label, Class: "text-xs font-medium text-zinc-400 group-selected:text-violet-200", MaxLines: 1}}})
	}
	prefs := a.danmakuPrefs.Get
	controls := core.View{Class: "gap-3", Children: []core.Node{
		desktopDanmakuSlider(cx, "opacity", locales.DanmakuOpacity(cx), 10, 100, 5,
			func() float64 { return float64(prefs().Opacity) }, func() string { return fmt.Sprintf("%d%%", prefs().Opacity) },
			func(value float64) { editor.save(func(p *danmakuPreferences) { p.Opacity = int(value) }) }),
		desktopDanmakuSlider(cx, "area", locales.DanmakuArea(cx), 25, 100, 25,
			func() float64 { return float64(prefs().Area) }, func() string { return fmt.Sprintf("%d%%", prefs().Area) },
			func(value float64) { editor.save(func(p *danmakuPreferences) { p.Area = int(value) }) }),
		desktopDanmakuSlider(cx, "offset", locales.PhonePlayerDanmakuTiming(cx), -60, 60, .5,
			func() float64 { return p.comments.offset().Seconds() }, func() string { return fmt.Sprintf("%+.1fs", p.comments.offset().Seconds()) },
			func(value float64) { p.comments.setOffset(time.Duration(value * float64(time.Second))) }),
	}}
	more := base.Button{Label: locales.DesktopDanmakuMore(cx), TestID: "desktop-danmaku-settings",
		Class: "flex-row items-center justify-between gap-3 rounded-xl border border-white/10 bg-white/5 px-3 py-2",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 1)}, OnPress: func(core.PressEvent) {
			if !a.showDesktopSettingsWindow(cx, desktopSettingsDanmaku) {
				p.comments.openPanel()
			}
		},
		Children: []core.Node{
			core.Text{Content: locales.DesktopDanmakuMore(cx), Class: "text-sm text-white"},
			core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 text-zinc-500"},
		}}
	return core.List{Class: "flex-1 min-h-0 p-4", TestID: "desktop-player-danmaku", EstimatedSize: 17, Gap: 10,
		Count: func() int { return len(p.comments.events()) },
		Header: core.View{Class: "gap-4 pb-3", Children: []core.Node{card,
			core.View{Class: "flex-row gap-2", Children: types}, controls, editor.warning(), more,
			core.Text{Content: locales.DesktopDanmakuList(cx), Class: "pt-1 text-xs font-medium text-zinc-500"},
		}},
		Row: func(cx *core.Scope, row core.ListRow) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				index, events := row.Index(), p.comments.events()
				if index < 0 || index >= len(events) {
					return nil
				}
				event := events[index]
				return base.Button{Label: event.Text, Class: "flex-row gap-3 rounded", TestID: "desktop-danmaku-row-" + strconv.Itoa(index),
					Style: core.Style{FocusVisible: ui.FocusRing(cx, 1)}, OnPress: func(core.PressEvent) { p.seek(event.Start) },
					Children: []core.Node{
						core.Text{Content: playbackClock(event.Start), Class: "w-10 shrink-0 text-xs text-zinc-500"},
						core.Text{Content: event.DisplayText(), Class: "min-w-0 flex-1 text-xs text-zinc-300", MaxLines: 1, Style: core.Style{
							Color: core.RGBA(event.Color, 1),
						}},
					}}
			})
		}}
}

func desktopDanmakuSlider(cx *core.Scope, id, label string, min, max, step float64, value func() float64, title func() string, edit func(float64)) core.Node {
	return core.View{Class: "gap-1.5", Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between", Children: []core.Node{
			core.Text{Content: label, Class: "text-xs text-zinc-400"},
			core.Text{ContentFunc: title, Class: "text-xs text-violet-400"},
		}},
		base.Slider{Class: "h-3 justify-center", TestID: "desktop-danmaku-" + id, ThumbTestID: "desktop-danmaku-" + id + "-thumb",
			TrackClass: "h-full justify-center", IndicatorClass: "h-1 rounded-full bg-white/10",
			RangeClass: "rounded-full bg-linear-to-r from-violet-500 to-fuchsia-500",
			ThumbClass: "size-2.5 rounded-full bg-transparent", ThumbStyle: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			Label: label, Value: func() base.SliderValue { return base.SingleValue(value()) },
			Min: func() float64 { return min }, Max: func() float64 { return max }, Step: func() float64 { return step },
			OnChange: func(v base.SliderValue) { edit(v.Start) }},
	}}
}
