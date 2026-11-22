package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func (a *application) tvDanmakuOptions(cx *core.Scope, l tvLayout, current route, dialogs *[]core.Node) []tvOption {
	edit := a.editDanmaku(cx)
	toggle := func(id, label string, checked func(danmakuPreferences) bool, set func(*danmakuPreferences, bool)) tvOption {
		get := func() bool { return checked(edit.prefs.Get()) }
		update := func(value bool) { edit.save(func(p *danmakuPreferences) { set(p, value) }) }
		return tvOption{id: id, label: label, checked: get, action: func() { update(!get()) },
			adjust: func(direction int) { update(direction > 0) }}
	}
	step := func(spec danmakuAdjustment, values []int, labels []string) tvOption {
		return tvOption{id: "tv-" + spec.id, label: spec.label,
			value: func() string {
				prefs := edit.prefs.Get()
				value := *spec.field(&prefs)
				if index := slices.Index(values, value); index >= 0 {
					return labels[index]
				}
				return spec.format(float64(value))
			},
			adjust: func(direction int) {
				edit.save(func(p *danmakuPreferences) {
					choice := spec.field(p)
					index, exact := slices.BinarySearch(values, *choice)
					if direction > 0 {
						if exact {
							index++
						}
					} else {
						index--
					}
					if index >= 0 && index < len(values) {
						*choice = values[index]
					}
				})
			},
		}
	}
	specs := danmakuAdjustments(cx)
	specs[3].label = locales.TvDanmakuDensity(cx)
	opacity := tvOption{id: "tv-danmaku-opacity", label: specs[0].label, large: true,
		adjust: func(direction int) {
			edit.save(func(p *danmakuPreferences) { p.Opacity = max(10, min(100, p.Opacity+direction*5)) })
		},
		content: func(func() bool) core.Node { return tvOpacitySetting(cx, l, edit.prefs.Get) }}
	area := tvOption{id: "tv-danmaku-area", label: locales.DanmakuArea(cx),
		adjust: func(direction int) {
			edit.save(func(p *danmakuPreferences) { p.Area = max(25, min(100, p.Area+direction*25)) })
		},
		content: func(focused func() bool) core.Node { return tvAreaSetting(cx, l, edit.prefs.Get, focused) }}
	filters := core.NewSignal(cx, false)
	credentials := core.NewSignal(cx, false)
	*dialogs = append(*dialogs,
		ui.Dialog{Open: filters.Get, OnOpenChange: func(open bool, _ base.Reason) { filters.Set(open) }, Title: locales.DanmakuKeywords(cx),
			Children: []core.Node{core.Func(func(cx *core.Scope) core.Node {
				return edit.filterEditor(cx, func() { filters.Set(false) })
			}), edit.warning()}},
		ui.Dialog{Open: credentials.Get, OnOpenChange: func(open bool, _ base.Reason) { credentials.Set(open) }, Title: locales.DanmakuSources(cx),
			Children: []core.Node{core.Func(a.danmakuSettings)}},
	)
	return []tvOption{
		toggle("tv-danmaku-visible", locales.TvDanmakuVisible(cx), func(p danmakuPreferences) bool { return p.Enabled },
			func(p *danmakuPreferences, value bool) { p.Enabled = value }),
		{id: "tv-danmaku-sources", label: locales.DanmakuSources(cx), value: func() string {
			return strings.Join([]string{locales.TvDanmakuDandan(cx), locales.TvDanmakuXML(cx)}, " · ")
		}, action: func() { credentials.Set(true) }},
		opacity,
		step(specs[1], []int{75, 100, 125, 150}, []string{locales.TvDanmakuSmall(cx), locales.TvDanmakuStandard(cx), locales.TvDanmakuLarge(cx), locales.TvDanmakuExtraLarge(cx)}),
		step(specs[2], []int{75, 100, 125, 150}, []string{locales.TvDanmakuSlow(cx), locales.TvDanmakuMedium(cx), locales.TvDanmakuFast(cx), locales.TvDanmakuVeryFast(cx)}),
		area,
		step(specs[3], []int{15, 40, 70}, []string{locales.TvDanmakuLow(cx), locales.TvDanmakuDensityMedium(cx), locales.TvDanmakuHigh(cx)}),
		toggle("tv-danmaku-deduplicate", locales.DanmakuDeduplicate(cx), func(p danmakuPreferences) bool { return p.Deduplicate },
			func(p *danmakuPreferences, value bool) { p.Deduplicate = value }),
		toggle("tv-danmaku-hide-fixed", locales.TvDanmakuHideFixed(cx), func(p danmakuPreferences) bool { return !p.Top && !p.Bottom },
			func(p *danmakuPreferences, value bool) { p.Top, p.Bottom = !value, !value }),
		{id: "tv-danmaku-keywords", label: locales.DanmakuKeywords(cx), value: func() string {
			p := edit.prefs.Get()
			return locales.TvDanmakuRuleCount(cx, strconv.Itoa(len(danmakuRules(p.Keywords))+len(danmakuRules(p.Patterns))))
		}, action: func() { filters.Set(true) }},
		a.tvDanmakuOffset(cx, current),
	}
}

func tvOpacitySetting(cx *core.Scope, l tvLayout, prefs func() danmakuPreferences) core.Node {
	label := l.text(locales.DanmakuOpacity(cx), 24, tvSettingsTitle+" "+tvSettingsFocusText)
	value := l.text("", 24, "font-semibold text-violet-500 dark:text-violet-400")
	value.TestID = "tv-danmaku-opacity-value"
	value.ContentFunc = func() string { return locales.DanmakuPercent(cx, strconv.Itoa(prefs().Opacity)) }
	return core.View{Style: core.Style{Gap: l.points(16)}, Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between", Children: []core.Node{label, value}},
		core.View{Class: "relative justify-center", Style: core.Style{Height: l.length(24)}, Children: []core.Node{
			core.View{Class: "rounded-full bg-zinc-900/10 dark:bg-white/15", Style: core.Style{Height: l.length(8)}},
			core.View{Class: "absolute left-0 rounded-full bg-violet-500", StyleFunc: func() core.Style {
				return core.Style{Top: l.length(8), Height: l.length(8), Width: core.Pct(float32(prefs().Opacity))}
			}},
			core.View{Class: "absolute top-0 rounded-full border border-black/5 bg-white shadow-md", StyleFunc: func() core.Style {
				return core.Style{Left: core.Pct(float32(prefs().Opacity)), Margin: core.Edges{Left: -l.points(12)}, Width: l.length(24), Height: l.length(24)}
			}},
		}},
	}}
}

func tvAreaSetting(cx *core.Scope, l tvLayout, prefs func() danmakuPreferences, focused func() bool) core.Node {
	names := []string{locales.TvDanmakuQuarter(cx), locales.TvDanmakuHalf(cx), locales.TvDanmakuThreeQuarters(cx), locales.DanmakuFullArea(cx)}
	parts := make([]core.Node, 0, len(names))
	for i, label := range names {
		area := 25 * (i + 1)
		selected := func() bool { return prefs().Area == area }
		text := l.text(label, 20, "group-selected:text-white group-selected:font-semibold")
		text.StyleFunc = func() core.Style {
			if selected() {
				return core.Style{Color: core.RGB(0xffffff)}
			}
			if focused() {
				if cx.Dark() {
					return core.Style{Color: core.RGBA(0x0f1017, .7)}
				}
				return core.Style{Color: core.RGBA(0xf4f4f7, .7)}
			}
			return core.Style{Color: cx.Palette().TextMuted}
		}
		parts = append(parts, core.View{Class: "group rounded-full selected:bg-violet-500 selected:shadow-md",
			Selected: selected, TestID: "tv-danmaku-area-" + strconv.Itoa(area),
			Style: core.Style{Padding: l.padding(20, 6)}, Children: []core.Node{text}})
	}
	return core.View{Class: "flex-row rounded-full", StyleFunc: func() core.Style {
		color := core.RGBA(0x18181b, .1)
		if cx.Dark() != focused() {
			color = core.RGBA(0xf4f4f7, .15)
		}
		return core.Style{Background: color, Padding: core.All(l.points(4)), Gap: l.points(6)}
	}, Children: parts}
}

func (a *application) tvDanmakuOffset(cx *core.Scope, current route) tvOption {
	option := tvOption{id: "tv-danmaku-offset", label: locales.TvDanmakuOffset(cx)}
	if current.item.ID == "" {
		option.value = func() string { return locales.SettingsUnavailable(cx) }
		option.disabled = func() bool { return true }
		return option
	}
	offset, set, _ := a.danmakuOffsetEditor(cx, current)
	option.value = func() string { return locales.SettingsSeconds(cx, fmt.Sprintf("%+.1f", offset.Get().Seconds())) }
	option.adjust = func(direction int) { set(offset.GetUntracked() + time.Duration(direction)*500*time.Millisecond) }
	return option
}
