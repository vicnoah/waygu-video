package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func playerDanmakuBadge(cx *core.Scope) core.Node {
	return core.View{Class: "size-6 shrink-0 items-center justify-center rounded-md bg-linear-to-br from-violet-500 to-fuchsia-500",
		Children: []core.Node{core.Text{Content: locales.PhonePlayerDanmakuBadge(cx), Class: "text-xs font-bold text-white"}}}
}

func playerSwitch(cx *core.Scope, label, id string, checked func() bool, change func(bool)) core.Node {
	return base.Switch{TestID: id, Label: label, Checked: checked, OnChange: change,
		Class: "shrink-0 rounded-full", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		TrackClass: "h-[26px] w-[43px] flex-row items-center justify-start rounded-full bg-white/15 px-0.5 checked:justify-end checked:bg-violet-500",
		ThumbClass: "size-[22px] shrink-0 rounded-full bg-white shadow-md",
	}
}

func playerToggleRow(cx *core.Scope, label, id string, checked func() bool, change func(bool)) core.Node {
	return core.View{Class: "min-h-7 flex-row items-center gap-2", Children: []core.Node{
		core.Text{Content: label, Class: "min-w-0 flex-1 text-sm text-white"},
		playerSwitch(cx, label, id, checked, change),
	}}
}

func (e danmakuEditor) playerToggle(cx *core.Scope, label, id string, field func(*danmakuPreferences) *bool) core.Node {
	return playerToggleRow(cx, label, id,
		func() bool { p := e.prefs.Get(); return *field(&p) },
		func(value bool) { e.save(func(p *danmakuPreferences) { *field(p) = value }) })
}

func (e danmakuEditor) playerDisplay(cx *core.Scope, visible func() bool, setVisible func(bool), offset core.Node) core.Node {
	sliders := make([]core.Node, 0, 4)
	for _, spec := range danmakuAdjustments(cx) {
		value := func() float64 { p := e.prefs.Get(); return float64(*spec.field(&p)) }
		sliders = append(sliders, core.View{Class: "min-w-0 flex-1 gap-1.5", Children: []core.Node{
			core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
				core.Text{Content: spec.label, MaxLines: 1, Class: "min-w-0 shrink text-xs text-zinc-400"},
				core.Text{ContentFunc: func() string { return spec.format(value()) }, Class: "text-xs font-medium text-violet-400"},
			}},
			playerRail(cx, base.Slider{Label: spec.label, TestID: spec.id, ThumbTestID: spec.id + "-thumb", Class: "h-3",
				Min: func() float64 { return spec.min }, Max: func() float64 { return spec.max }, Step: func() float64 { return spec.step },
				Value: func() base.SliderValue { return base.SingleValue(value()) }, ValueText: spec.format,
				OnChange: func(v base.SliderValue) { e.save(func(p *danmakuPreferences) { *spec.field(p) = int(v.End) }) },
			}, "size-3", "h-1"),
		}})
	}
	areas := []ui.Option[int]{{Value: 25, Label: "1/4"}, {Value: 50, Label: "1/2"}, {Value: 75, Label: "3/4"}, {Value: 100, Label: locales.DanmakuFullArea(cx)}}
	outlines := []ui.Option[int]{{Value: 0, Label: locales.DanmakuOutlineNone(cx)}, {Value: 1, Label: locales.DanmakuOutlineRegular(cx)},
		{Value: 2, Label: locales.DanmakuOutlineStrong(cx)}}
	return core.View{Class: "gap-3.5", Children: []core.Node{
		playerToggleRow(cx, locales.ShowDanmaku(cx), "danmaku-display-enabled", visible, setVisible),
		core.View{Class: "flex-row gap-5", Children: sliders[:2]},
		core.View{Class: "flex-row gap-5", Children: sliders[2:]},
		e.playerChoices(cx, locales.DanmakuArea(cx), "danmaku-area", areas, func(p *danmakuPreferences) *int { return &p.Area }),
		e.playerChoices(cx, locales.DanmakuOutline(cx), "danmaku-outline", outlines, func(p *danmakuPreferences) *int { return &p.Outline }),
		e.playerToggle(cx, locales.DanmakuBold(cx), "danmaku-bold", func(p *danmakuPreferences) *bool { return &p.Bold }),
		e.playerToggle(cx, locales.DanmakuReserveSubtitles(cx), "danmaku-reserve-subtitles", func(p *danmakuPreferences) *bool { return &p.ReserveSubtitles }),
		offset,
		core.Text{Content: locales.DanmakuTypes(cx), Class: "text-xs text-zinc-400"},
		core.View{Class: "gap-2", Children: []core.Node{
			e.playerToggle(cx, locales.DanmakuScrolling(cx), "danmaku-scrolling", func(p *danmakuPreferences) *bool { return &p.Scrolling }),
			e.playerToggle(cx, locales.DanmakuTop(cx), "danmaku-top", func(p *danmakuPreferences) *bool { return &p.Top }),
			e.playerToggle(cx, locales.DanmakuBottom(cx), "danmaku-bottom", func(p *danmakuPreferences) *bool { return &p.Bottom }),
			e.playerToggle(cx, locales.DanmakuColored(cx), "danmaku-colored", func(p *danmakuPreferences) *bool { return &p.Colored }),
		}},
	}}
}

func (e danmakuEditor) playerChoices(cx *core.Scope, label, id string, values []ui.Option[int], field func(*danmakuPreferences) *int) core.Node {
	choices := make([]core.Node, 0, len(values))
	for _, value := range values {
		choices = append(choices, playerChoice(cx, id+"-"+strconv.Itoa(value.Value), value.Label, "h-7 flex-1",
			func() bool { p := e.prefs.Get(); return *field(&p) == value.Value }, nil,
			func(bool) { e.save(func(p *danmakuPreferences) { *field(p) = value.Value }) }))
	}
	return core.View{Class: "gap-1.5", Children: []core.Node{
		core.Text{Content: label, Class: "text-xs text-zinc-400"},
		base.RadioGroup{Label: label, Class: "flex-row gap-1.5", Children: choices},
	}}
}

const danmakuSourceRow = "min-h-11 flex-row items-center gap-3 bg-zinc-800/70 px-3"

func playerOnlineDanmakuRow(cx *core.Scope, label, id string, count func() int, enabled *core.Signal[bool]) core.Node {
	return core.View{Class: danmakuSourceRow, Children: []core.Node{
		playerDanmakuBadge(cx), core.Text{Content: label, Class: "flex-1 text-sm text-white"},
		core.Text{ContentFunc: func() string { return strconv.Itoa(count()) }, Class: "text-xs text-zinc-500"},
		playerSwitch(cx, label, id, enabled.Get, enabled.Set),
	}}
}

func (e danmakuEditor) playerSources(cx *core.Scope, current route, online, customRow core.Node, showMatch *core.Signal[bool],
	imported func() importedDanmaku, onlineCount, dandanCount func() int,
	local, remote *core.Signal[bool], invalid, busy func() bool, openFile func()) core.Node {
	poster := core.View{Class: "h-12 w-9 shrink-0 items-center justify-center overflow-hidden rounded-md bg-zinc-800", Children: []core.Node{
		core.Icon{SVG: lucide.Film(), Class: "size-4 text-zinc-400"},
		core.Show{When: func() bool { return current.item.Poster != "" }, Then: func(*core.Scope) core.Node {
			return core.Image{Class: "absolute inset-0", Source: core.Src(current.item.Poster), ResizeMode: core.Cover}
		}},
	}}
	localRow := core.Dynamic(func(cx *core.Scope) core.Node {
		file := imported()
		title, status := locales.PhonePlayerLocalDanmaku(cx), locales.PhonePlayerDanmakuNotImported(cx)
		if len(file.comments) > 0 {
			title, status = locales.PhonePlayerLocalDanmakuFormat(cx, file.format), strconv.Itoa(len(file.comments))
		}
		children := []core.Node{
			core.View{Class: "size-6 items-center justify-center rounded-md bg-zinc-600", Children: []core.Node{
				core.Icon{SVG: lucide.FileText(), Class: "size-3.5 text-white"},
			}},
			core.Text{Content: title, Class: "min-w-0 flex-1 text-sm text-white", MaxLines: 1},
			core.Text{Content: status, TestID: "danmaku-local-count", Class: "text-xs text-zinc-500"},
		}
		if len(file.comments) > 0 {
			return core.View{Class: danmakuSourceRow, Children: append(children, playerSwitch(cx, title, "danmaku-toggle-local", local.Get, local.Set))}
		}
		return base.Button{Label: title, TestID: "danmaku-open-local-file", Class: danmakuSourceRow + " disabled:opacity-50",
			Disabled: busy, Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { openFile() },
			Children: append(children, core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 text-zinc-600"})}
	})
	action := func(id, label string, icon *core.SVG, disabled func() bool, press func()) core.Node {
		return base.Button{Label: label, TestID: id, Disabled: disabled,
			Class: "h-9 min-w-0 flex-1 flex-row items-center justify-center gap-1.5 rounded-xl border border-white/10 bg-white/10 disabled:opacity-50",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { press() },
			Children: []core.Node{core.Icon{SVG: icon, Class: "size-3.5 shrink-0 text-white"},
				core.Text{Content: label, MaxLines: 1, Class: "min-w-0 shrink text-xs text-white"}}}
	}
	dandanLabel := locales.PhonePlayerDandan(cx)
	if current.downloadID != "" {
		dandanLabel = locales.LogvarSavedComments(cx)
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		core.View{Class: "flex-row items-center gap-3 rounded-2xl border border-violet-500/30 bg-linear-to-br from-violet-500/20 to-fuchsia-500/10 p-3",
			Children: []core.Node{poster, core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
				core.Text{Content: current.item.Title, MaxLines: 1, Class: "text-sm font-medium text-white"},
				core.Text{ContentFunc: func() string {
					if n := onlineCount(); n > 0 {
						return locales.PhonePlayerDanmakuLoaded(cx, strconv.Itoa(n))
					}
					return locales.MovieDanmakuUnmatched(cx)
				}, Class: "text-[11px] text-violet-300"},
			}}}},
		core.View{Class: "gap-px overflow-hidden rounded-xl border border-white/10 bg-white/10", Children: []core.Node{
			playerOnlineDanmakuRow(cx, dandanLabel, "danmaku-toggle-remote", dandanCount, remote),
			customRow,
			localRow,
		}},
		core.View{Class: "flex-row gap-2", Children: []core.Node{
			action("danmaku-manual-match", locales.PhonePlayerDanmakuManualMatch(cx), lucide.Search(),
				func() bool { return current.downloadID != "" }, func() { showMatch.Update(func(open bool) bool { return !open }) }),
			action("danmaku-import-file", locales.AddDanmaku(cx), lucide.Upload(), busy, openFile),
		}},
		core.Show{When: invalid, Then: func(cx *core.Scope) core.Node { return notice(locales.DanmakuFileError(cx)) }},
		e.playerToggle(cx, locales.DanmakuDeduplicate(cx), "danmaku-deduplicate", func(p *danmakuPreferences) *bool { return &p.Deduplicate }),
		core.Show{When: func() bool { return current.downloadID != "" || showMatch.Get() }, Then: func(*core.Scope) core.Node { return online }},
	}}
}

func (e danmakuEditor) playerFilters(cx *core.Scope) core.Node {
	open := core.NewSignal(cx, false)
	chips := core.Dynamic(func(cx *core.Scope) core.Node {
		p := e.prefs.Get()
		var nodes []core.Node
		for _, regex := range []bool{false, true} {
			value := p.Keywords
			if regex {
				value = p.Patterns
			}
			for _, rule := range danmakuRules(value) {
				class := "bg-white/10 text-zinc-200"
				if regex {
					class = "border border-amber-500/40 bg-amber-500/10 text-amber-300"
				}
				label := rule
				if regex {
					label = locales.PhonePlayerDanmakuRegex(cx, rule)
				}
				nodes = append(nodes, base.Button{Label: locales.PhonePlayerRemoveRule(cx, label),
					Class: "max-w-full flex-row items-center gap-1 rounded-full px-2.5 py-1 " + class,
					Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) {
						e.save(func(p *danmakuPreferences) {
							field := &p.Keywords
							if regex {
								field = &p.Patterns
							}
							remaining := slices.DeleteFunc(danmakuRules(*field), func(old string) bool { return old == rule })
							*field = strings.Join(remaining, "\n")
						})
					}, Children: []core.Node{core.Text{Content: label, MaxLines: 1, Class: "min-w-0 shrink text-xs"},
						core.Icon{SVG: lucide.X(), Class: "size-3 shrink-0 opacity-60"}},
				})
			}
		}
		return core.View{Class: "flex-row flex-wrap gap-1.5", Children: nodes}
	})
	return core.View{Class: "gap-3", Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
			panelHeading(locales.DanmakuKeywords(cx)),
			base.Button{Label: locales.PhonePlayerAddFilter(cx), TestID: "danmaku-edit-filters",
				Class: "h-8 justify-center rounded px-2", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
				OnPress:  func(core.PressEvent) { open.Update(func(previous bool) bool { return !previous }) },
				Children: []core.Node{core.Text{Content: locales.PhonePlayerAddFilter(cx), Class: "text-xs text-violet-400"}}},
		}},
		chips,
		core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
			return e.filterEditor(cx, func() { open.Set(false) })
		}},
		core.View{Class: "gap-px overflow-hidden rounded-xl border border-white/10 bg-white/10", Children: []core.Node{
			core.View{Class: "gap-3 bg-zinc-800/70 px-3 py-2.5", Children: []core.Node{
				e.playerToggle(cx, locales.DanmakuDeduplicate(cx), "danmaku-deduplicate", func(p *danmakuPreferences) *bool { return &p.Deduplicate }),
			}},
			core.View{Class: "bg-zinc-800/70 px-3 py-2.5", Children: []core.Node{
				playerToggleRow(cx, locales.PhonePlayerHideColored(cx), "danmaku-colored",
					func() bool { return !e.prefs.Get().Colored },
					func(value bool) { e.save(func(p *danmakuPreferences) { p.Colored = !value }) }),
			}},
		}},
	}}
}
