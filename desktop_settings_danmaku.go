package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/danmaku"
	"waygu-video/locales"
)

func (a *application) desktopSettingsDanmaku(cx *core.Scope) core.Node {
	editor := a.editDanmaku(cx)
	prefs := a.danmakuPrefs.Get
	toggle := func(id, title string, checked func() bool, change func(*danmakuPreferences, bool)) core.Node {
		return desktopFormToggle(cx, id, title, checked, func(value bool) {
			editor.save(func(p *danmakuPreferences) { change(p, value) })
		})
	}
	var displayRows []core.Node
	for _, adjustment := range danmakuAdjustments(cx) {
		value := func() float64 { p := prefs(); return float64(*adjustment.field(&p)) }
		id := "desktop-settings-" + adjustment.id
		displayRows = append(displayRows, core.View{Class: "gap-2 bg-white px-4 py-2.5 dark:bg-[#1a1b25]", Children: []core.Node{
			core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
				core.Text{Content: adjustment.label, Class: "text-sm " + setupTitle},
				core.Text{ContentFunc: func() string {
					if adjustment.id == "danmaku-font-size" {
						return fmt.Sprintf("%d px", (danmaku.DefaultSettings().FontSize*int(value())+50)/100)
					}
					return adjustment.format(value())
				}, Class: "text-sm font-medium " + setupAccent},
			}},
			base.Slider{TestID: id, ThumbTestID: id + "-thumb", Label: adjustment.label, Class: "h-5 min-w-0 justify-center",
				TrackClass: "h-full justify-center", IndicatorClass: "h-1.5 rounded-full bg-zinc-900/10 dark:bg-white/10",
				RangeClass: "rounded-full bg-violet-500",
				ThumbClass: "size-5 rounded-full border border-black/5 bg-white shadow-md", ThumbStyle: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
				Min: func() float64 { return adjustment.min }, Max: func() float64 { return adjustment.max }, Step: func() float64 { return adjustment.step },
				Value: func() base.SliderValue { return base.SingleValue(value()) },
				OnChange: func(state base.SliderValue) {
					editor.save(func(p *danmakuPreferences) { *adjustment.field(p) = int(state.Start) })
				}},
		}})
	}
	area := locales.DanmakuArea(cx)
	displayRows = append(displayRows, desktopFormRow(area, desktopFormSegments(cx, "desktop-settings-danmaku-area", area, []ui.Option[int]{
		{Label: "1/4", Value: 25, TestID: "quarter"},
		{Label: locales.DesktopDanmakuHalf(cx), Value: 50, TestID: "half"},
		{Label: "3/4", Value: 75, TestID: "three-quarters"},
		{Label: locales.DesktopDanmakuFull(cx), Value: 100, TestID: "full"},
	}, func() int { return prefs().Area }, func(value int) { editor.save(func(p *danmakuPreferences) { p.Area = value }) })))
	dandan := "弹弹play"
	if !strings.HasPrefix(cx.Env().Locale(), "zh") {
		dandan = "DandanPlay"
	}
	credentialsOpen := core.NewSignal(cx, false)
	credentials := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (bool, error) {
		value, err := a.dandanCredentials()
		return value.AppID != "" && value.Secret != "", err
	})
	dandanLabel := func() string {
		if configured, _ := credentials.Get(); configured {
			return locales.DesktopDanmakuConfigured(cx)
		}
		return locales.DesktopDanmakuConfigure(cx)
	}
	dandanRow := desktopFormRowHeight(dandan, "h-10", core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
		base.Button{LabelFunc: dandanLabel, TestID: "desktop-danmaku-credentials", Class: "rounded",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { credentialsOpen.Set(true) },
			Children: []core.Node{core.Text{ContentFunc: dandanLabel, Class: "text-xs", ClassFunc: func() string {
				if configured, _ := credentials.Get(); configured {
					return "text-emerald-600 dark:text-emerald-400"
				}
				return setupAccent
			}}}},
		toggle("desktop-danmaku-dandan", dandan, func() bool { return !prefs().DisableDandan },
			func(p *danmakuPreferences, value bool) { p.DisableDandan = !value }),
	}})
	local := locales.DesktopDanmakuLocalFiles(cx)
	strategy := locales.DesktopDanmakuStrategy(cx)
	providers := desktopFormGroup(locales.DanmakuSources(cx),
		dandanRow,
		desktopFormRowHeight(locales.DesktopDanmakuBilibili(cx), "h-10", desktopFormToggle(cx, "", locales.DesktopDanmakuBilibili(cx), func() bool { return false }, nil)),
		desktopFormRowHeight(locales.DesktopDanmakuBahamut(cx), "h-10", desktopFormToggle(cx, "", locales.DesktopDanmakuBahamut(cx), func() bool { return false }, nil)),
		desktopFormRowHeight(local, "h-10", toggle("desktop-danmaku-local", local, func() bool { return !prefs().DisableLocal },
			func(p *danmakuPreferences, value bool) { p.DisableLocal = !value })),
		desktopFormRowHeight(strategy, "h-10", desktopFormSelect(cx, "desktop-danmaku-strategy", strategy, []ui.Option[bool]{
			{Label: locales.DesktopDanmakuAutoMatch(cx), Value: true},
			{Label: locales.DesktopDanmakuManualMatch(cx)},
		}, func() bool { return prefs().AutoMatch }, func(value bool) { editor.save(func(p *danmakuPreferences) { p.AutoMatch = value }) })),
	)
	enable := locales.DesktopDanmakuEnabled(cx)
	return core.View{Class: "gap-4", Children: []core.Node{
		core.View{Class: "flex-row items-center gap-4", Children: []core.Node{
			desktopSettingsHeading(locales.Danmaku(cx), locales.DesktopDanmakuHint(cx)),
			core.Text{Content: enable, Class: "text-sm " + setupTitle},
			toggle("desktop-settings-danmaku-enabled", enable, func() bool { return prefs().Enabled },
				func(p *danmakuPreferences, value bool) { p.Enabled = value }),
		}},
		desktopDanmakuPreview(cx, prefs),
		core.View{Class: "flex-row gap-5", Children: []core.Node{
			core.View{Class: "min-w-0 flex-1", Children: []core.Node{desktopFormGroup(locales.DesktopDanmakuDisplay(cx), displayRows...)}},
			core.View{Class: "min-w-0 flex-1 gap-2", Children: []core.Node{providers, desktopDanmakuFilters(cx, editor, cx.CacheDir())}},
		}},
		editor.warning(),
		ui.Dialog{Open: credentialsOpen.Get, OnOpenChange: func(open bool, _ base.Reason) { credentialsOpen.Set(open) },
			OnClose: credentials.Refetch, Title: dandan, Children: []core.Node{
				core.Show{When: credentialsOpen.Get, Then: a.danmakuSettings},
			}},
	}}
}

func desktopDanmakuPreview(cx *core.Scope, prefs func() danmakuPreferences) core.Node {
	samples := []danmaku.Comment{
		{At: 2700 * time.Millisecond, Mode: danmaku.Scroll, Color: danmaku.White, Text: locales.DesktopDanmakuSampleScore(cx)},
		{At: 700 * time.Millisecond, Mode: danmaku.Scroll, Color: 0x7dd3fc, Text: locales.DesktopDanmakuSampleDetails(cx)},
		{At: 4100 * time.Millisecond, Mode: danmaku.Scroll, Color: 0xfcd34d, Text: locales.DesktopDanmakuSampleScene(cx)},
		{At: 3400 * time.Millisecond, Mode: danmaku.Scroll, Color: danmaku.White, Text: locales.DesktopDanmakuSampleActing(cx)},
		{At: 1700 * time.Millisecond, Mode: danmaku.Scroll, Color: danmaku.White, Text: locales.DesktopDanmakuSampleLaugh(cx)},
	}
	return core.View{Class: "relative h-36 overflow-hidden rounded-2xl bg-[#ababab] shadow-lg shadow-black/20 dark:bg-[#1b1b1b]", TestID: "desktop-danmaku-preview", Children: []core.Node{
		core.View{Class: "absolute inset-0 items-center justify-center", Children: []core.Node{
			core.Icon{SVG: lucide.Image(), Class: "size-7 text-black/55 dark:text-white/55"},
		}},
		core.Canvas{Class: "absolute inset-0", Draw: func(d *core.Drawing, bounds core.Size) {
			p := prefs()
			if !p.Enabled || bounds.Width < 1 || bounds.Height < 1 {
				return
			}
			settings := p.rendererSettings(0)
			settings.Width, settings.Height = int(bounds.Width), int(bounds.Height)
			settings.FontSize = 18 * p.FontScale / 100
			settings.Outline = 0
			track := danmaku.Schedule(samples, settings)
			drawDanmaku(d, core.Bounds{Size: bounds}, track, 5500*time.Millisecond)
		}},
		core.Text{Content: locales.DesktopDanmakuPreview(cx), Class: "absolute bottom-2.5 right-3 rounded bg-black/60 px-1.5 py-0.5 text-[10px] text-zinc-300"},
	}}
}

func desktopDanmakuFilters(cx *core.Scope, editor danmakuEditor, cacheDir string) core.Node {
	open := core.NewSignal(cx, false)
	rules := core.Dynamic(func(cx *core.Scope) core.Node {
		prefs := editor.prefs.Get()
		var chips []core.Node
		for _, group := range []struct {
			raw     string
			pattern bool
		}{{prefs.Keywords, false}, {prefs.Patterns, true}} {
			for _, rule := range danmakuRules(group.raw) {
				name := rule
				if group.pattern {
					name = "/" + name + "/"
				}
				chips = append(chips, desktopSettingsAction(cx, "", name+" ×", "rounded-full bg-zinc-900/10 px-2 py-0.5 dark:bg-white/10 "+setupTitle, func() {
					editor.save(func(p *danmakuPreferences) {
						raw := &p.Keywords
						if group.pattern {
							raw = &p.Patterns
						}
						*raw = strings.Join(slices.DeleteFunc(danmakuRules(*raw), func(value string) bool { return value == rule }), "\n")
					})
				}))
			}
		}
		return core.View{Class: "gap-2", Children: []core.Node{
			core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
				core.Text{Content: locales.DesktopDanmakuFilters(cx, strconv.Itoa(len(chips))), Class: "text-sm " + setupTitle},
				desktopSettingsAction(cx, "desktop-danmaku-add-filter", locales.DesktopDanmakuAddFilter(cx), setupAccent, func() { open.Set(true) }),
			}},
			core.View{Class: "flex-row flex-wrap gap-1.5", Children: chips},
		}}
	})
	return core.View{Class: "gap-2 rounded-xl border border-zinc-900/8 p-3 shadow-sm shadow-black/5 dark:border-white/8 " + desktopLibrarySurface, Children: []core.Node{
		rules,
		desktopDanmakuCache(cx, cacheDir),
		ui.Dialog{Open: open.Get, OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) },
			Title: locales.DanmakuFilterTab(cx), Children: []core.Node{core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
				return editor.filterEditor(cx, func() { open.Set(false) })
			}}}},
	}}
}

func desktopDanmakuCache(cx *core.Scope, cacheDir string) core.Node {
	confirm := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	clear := locales.DesktopDanmakuClearCache(cx)
	return core.View{Class: "gap-2 border-t border-zinc-900/8 pt-2 dark:border-white/8", Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
			core.Text{Content: locales.DesktopDanmakuCache(cx), Class: "min-w-0 flex-1 text-xs " + setupMuted},
			core.Show{When: func() bool { return cacheDir != "" && !busy.Get() }, Then: func(cx *core.Scope) core.Node {
				return desktopSettingsAction(cx, "desktop-danmaku-cache-clear", clear, "text-rose-500", func() { confirm.Set(true) })
			}},
		}},
		settingsFailure(cx, failure),
		ui.AlertDialog{Open: confirm.Get, OnOpenChange: func(open bool, _ base.Reason) { confirm.Set(open) },
			Title: clear, Description: locales.DesktopDanmakuClearCacheHint(cx), Actions: []ui.AlertAction{
				{Text: locales.DesktopSettingsCancel(cx), Role: core.AlertCancel},
				{Text: clear, Role: core.AlertDestructive, Variant: ui.ButtonDanger, OnPress: func() {
					busy.Set(true)
					go func() {
						err := danmaku.ClearCachedComments(cacheDir)
						cx.Post(func() { failure.Set(err); busy.Set(false) })
					}()
				}},
			}},
	}}
}
