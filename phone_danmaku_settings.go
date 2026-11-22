package main

import (
	"context"
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

func (a *application) phoneDanmakuSettings(cx *core.Scope) core.Node {
	editor := a.editDanmaku(cx)
	prefs := editor.prefs.Get
	toggle := func(setting phoneSetting, id string, field func(*danmakuPreferences) *bool) core.Node {
		return setting.toggle(cx, id,
			func() bool { p := prefs(); return *field(&p) },
			func(checked bool) { editor.save(func(p *danmakuPreferences) { *field(p) = checked }) })
	}
	outlines := []ui.Option[int]{
		{Value: 0, Label: locales.DanmakuOutlineNone(cx)},
		{Value: 1, Label: locales.DanmakuOutlineRegular(cx)},
		{Value: 2, Label: locales.DanmakuOutlineStrong(cx)},
	}
	areas := []ui.Option[int]{
		{Value: 25, Label: locales.PhoneDanmakuQuarter(cx)},
		{Value: 50, Label: locales.PhoneDanmakuHalf(cx)},
		{Value: 75, Label: locales.PhoneDanmakuThreeQuarters(cx)},
		{Value: 100, Label: locales.DanmakuFullArea(cx)},
	}
	rows := []core.Node{
		toggle(phoneSetting{locales.DanmakuEnabledByDefault(cx), "", "bg-pink-500", lucide.MessageSquareText},
			"danmaku-default-enabled", func(p *danmakuPreferences) *bool { return &p.Enabled }),
	}
	for _, adjustment := range danmakuAdjustments(cx) {
		rows = append(rows, editor.phoneDanmakuSlider(cx, adjustment))
	}
	rows = append(rows,
		phoneSettingChoice(cx, phoneSetting{label: locales.DanmakuArea(cx)}, "danmaku-area", areas,
			func() int { return prefs().Area }, func(value int) { editor.save(func(p *danmakuPreferences) { p.Area = value }) }),
		phoneSettingChoice(cx, phoneSetting{label: locales.DanmakuOutline(cx)}, "danmaku-outline", outlines,
			func() int { return prefs().Outline }, func(value int) { editor.save(func(p *danmakuPreferences) { p.Outline = value }) }),
		phoneSettingChoice(cx, phoneSetting{label: locales.PhoneDanmakuFont(cx)}, "danmaku-font", []ui.Option[bool]{
			{Value: true, Label: locales.PhoneDanmakuFontBold(cx)},
			{Label: locales.PhoneDanmakuFontRegular(cx)},
		}, func() bool { return prefs().Bold }, func(value bool) { editor.save(func(p *danmakuPreferences) { p.Bold = value }) }),
		toggle(phoneSetting{label: locales.PhoneDanmakuColored(cx)}, "danmaku-colored", func(p *danmakuPreferences) *bool { return &p.Colored }),
		toggle(phoneSetting{label: locales.PhoneDanmakuReserve(cx)}, "danmaku-reserve-subtitles", func(p *danmakuPreferences) *bool { return &p.ReserveSubtitles }),
	)

	panel := core.NewSignal(cx, "")
	credentials := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (bool, error) {
		credentials, err := a.dandanCredentials()
		return credentials.AppID != "" && credentials.Secret != "", err
	})
	dandanName := locales.TvDanmakuDandan(cx)
	dandanStatus := func() string {
		if prefs().DisableDandan {
			return locales.SettingsDisabled(cx)
		}
		if yes, _ := credentials.Get(); yes {
			return locales.DesktopDanmakuConfigured(cx)
		}
		return locales.PhoneDanmakuUnconfigured(cx)
	}
	sources := setupGroup(locales.PhoneDanmakuSourcesTitle(cx), locales.PhoneDanmakuSourceHint(cx),
		(phoneSetting{dandanName, "", "bg-violet-500", lucide.Play}).linkValue(cx, "phone-danmaku-dandan", dandanStatus,
			func() { panel.Set("credentials") }),
		(phoneSetting{locales.DesktopDanmakuBilibili(cx), "", "bg-pink-500", lucide.Tv}).unavailable(cx),
		(phoneSetting{locales.DesktopDanmakuBahamut(cx), "", "bg-sky-600", lucide.MessageSquare}).unavailable(cx),
		(phoneSetting{locales.PhoneDanmakuCustom(cx), "", "bg-zinc-500", lucide.Link}).unavailable(cx),
	)
	matching := setupGroup(locales.PhoneDanmakuMatchTitle(cx), "",
		toggle(phoneSetting{locales.DanmakuAutoMatch(cx), locales.PhoneDanmakuMatchHint(cx), "bg-emerald-500", lucide.FingerprintPattern},
			"danmaku-auto-match", func(p *danmakuPreferences) *bool { return &p.AutoMatch }),
		phoneSettingChoice(cx, phoneSetting{locales.PhoneDanmakuOnMiss(cx), "", "bg-amber-500", lucide.CircleQuestionMark},
			"danmaku-match-failure", []ui.Option[bool]{
				{Value: true, Label: locales.PhoneDanmakuPrompt(cx)},
				{Label: locales.PhoneDanmakuIgnore(cx)},
			}, func() bool { return prefs().PromptOnMiss }, func(value bool) { editor.save(func(p *danmakuPreferences) { p.PromptOnMiss = value }) }),
		toggle(phoneSetting{locales.PhoneDanmakuDeduplicate(cx), "", "bg-indigo-500", lucide.Layers},
			"danmaku-deduplicate", func(p *danmakuPreferences) *bool { return &p.Deduplicate }),
	)
	filters := setupGroup(locales.DanmakuFilterTab(cx), "",
		(phoneSetting{locales.PhoneDanmakuBlockWords(cx), "", "bg-rose-500", lucide.Ban}).linkValue(cx, "phone-danmaku-filters", func() string {
			return locales.PhoneDanmakuRules(cx, strconv.Itoa(len(danmakuRules(prefs().Keywords))+len(danmakuRules(prefs().Patterns))))
		}, func() { panel.Set("filters") }),
		(phoneSetting{locales.PhoneDanmakuBlockedUsers(cx), "", "bg-rose-500", lucide.UserX}).linkValue(cx, "phone-danmaku-blocked-users", func() string {
			return locales.PhoneDanmakuUsers(cx, strconv.Itoa(len(danmakuRules(prefs().BlockedUsers))))
		}, func() { panel.Set("users") }),
		(phoneSetting{locales.PhoneDanmakuFilterLevel(cx), "", "bg-rose-500", lucide.Funnel}).unavailable(cx),
	)
	cache := phoneDanmakuCache(cx, func(rows ...core.Node) core.Node {
		return setupGroup(locales.PhoneDanmakuSendCache(cx), locales.PhoneDanmakuSendHint(cx),
			append([]core.Node{(phoneSetting{locales.PhoneDanmakuSendAccount(cx), "", "bg-violet-500", lucide.Send}).unavailable(cx)}, rows...)...)
	})
	return phoneSettingsPage(cx, locales.Danmaku(cx), a.back, "phone-danmaku-settings",
		phoneDanmakuPreview(cx, prefs),
		setupGroup(locales.PhoneDanmakuBasic(cx), "", rows...), sources, matching, filters, cache, editor.warning(),
		core.Dynamic(func(cx *core.Scope) core.Node {
			var title string
			var content core.Node
			close := func() {
				if panel.GetUntracked() == "credentials" {
					credentials.Refetch()
				}
				panel.Set("")
			}
			switch panel.Get() {
			case "credentials":
				title = dandanName
				content = core.View{Class: "gap-3", Children: []core.Node{
					toggle(phoneSetting{label: locales.PhoneDanmakuDisableSource(cx)}, "phone-danmaku-disable-dandan",
						func(p *danmakuPreferences) *bool { return &p.DisableDandan }),
					a.danmakuSettings(cx),
				}}
			case "filters":
				title, content = locales.DanmakuKeywords(cx), editor.filterEditor(cx, close)
			case "users":
				title, content = locales.PhoneDanmakuBlockedUsers(cx), editor.phoneDanmakuBlockedUsers(cx, close)
			default:
				return nil
			}
			return ui.Dialog{Open: func() bool { return true }, Title: title,
				Trailing: []core.Node{ui.IconButton{Label: locales.TracksDone(cx), Icon: lucide.X, Size: 36,
					TestID: "close-phone-danmaku-editor", OnPress: close}},
				OnOpenChange: func(open bool, _ base.Reason) {
					if !open {
						close()
					}
				}, Children: []core.Node{content, editor.warning()}}
		}),
	)
}

func (e danmakuEditor) phoneDanmakuSlider(cx *core.Scope, spec danmakuAdjustment) core.Node {
	value := func() float64 { p := e.prefs.Get(); return float64(*spec.field(&p)) }
	format, label := spec.format, spec.label
	switch spec.id {
	case "danmaku-speed":
		format = func(value float64) string {
			if value == 100 {
				return locales.TvDanmakuMedium(cx)
			}
			return spec.format(value)
		}
	case "danmaku-density":
		label = locales.PhoneDanmakuDensity(cx)
		format = func(value float64) string { return locales.PhoneDanmakuRules(cx, strconv.Itoa(int(value))) }
	}
	return core.View{Class: "gap-2 bg-white px-4 py-3 dark:bg-[#1a1b25]", Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between", Children: []core.Node{
			core.Text{Content: label, Class: "text-[15px] " + setupTitle},
			core.Text{ContentFunc: func() string { return format(value()) }, Class: "text-sm font-medium " + setupAccent},
		}},
		base.Slider{Label: label, TestID: spec.id, ThumbTestID: spec.id + "-thumb", Class: "h-5 min-w-0 justify-center",
			TrackClass: "h-full justify-center", IndicatorClass: "h-1.5 rounded-full bg-zinc-900/10 dark:bg-white/10",
			RangeClass: "rounded-full bg-waygu-control",
			ThumbClass: "size-5 rounded-full border border-black/5 bg-white shadow-md",
			ThumbStyle: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			Min:        func() float64 { return spec.min }, Max: func() float64 { return spec.max }, Step: func() float64 { return spec.step },
			Value: func() base.SliderValue { return base.SingleValue(value()) }, ValueText: format,
			OnChange: func(state base.SliderValue) {
				e.save(func(p *danmakuPreferences) { *spec.field(p) = int(state.Start) })
			}},
	}}
}

func phoneDanmakuPreview(cx *core.Scope, prefs func() danmakuPreferences) core.Node {
	samples := []danmaku.Comment{
		{At: 2450 * time.Millisecond, Mode: danmaku.Scroll, Color: danmaku.White, Text: locales.PhoneDanmakuSampleScene(cx)},
		{At: 100 * time.Millisecond, Mode: danmaku.Scroll, Color: 0x7dd3fc, Text: locales.PhoneDanmakuSampleArt(cx)},
		{At: 2900 * time.Millisecond, Mode: danmaku.Scroll, Color: 0xf9a8d4, Text: "awsl"},
	}
	return core.View{Class: "px-4", Children: []core.Node{
		core.View{Class: "relative h-44 overflow-hidden rounded-2xl bg-[#f5f5f5] dark:bg-[#262626]", TestID: "phone-danmaku-preview",
			Children: []core.Node{
				core.View{Class: "absolute inset-0 items-center justify-center", Children: []core.Node{
					core.Icon{SVG: lucide.Image(), Class: "size-7 text-black/55 dark:text-white/55"},
				}},
				core.Canvas{Class: "absolute inset-0", TestID: "phone-danmaku-preview-canvas", Draw: func(d *core.Drawing, bounds core.Size) {
					width, height := float32(bounds.Width), float32(bounds.Height)
					if width < 1 || height < 1 {
						return
					}
					p := prefs()
					limit := height * float32(p.Area) / 100
					d.SetFillColor(core.RGBA(0x000000, .2)).FillRect(0, limit, width, height-limit)
					if p.Area < 100 {
						d.SetStrokeColor(core.RGBA(0xffffff, .3)).SetLineWidth(1).SetLineDash(4, 3).
							BeginPath().MoveTo(0, limit).LineTo(width, limit).Stroke().SetLineDash()
					}
					if p.Enabled {
						settings := p.rendererSettings(0)
						settings.Width, settings.Height = int(width), int(height)
						settings.FontSize = 15 * p.FontScale / 100
						settings.Outline *= .45
						track := danmaku.Schedule(samples, settings)
						for i := range track.Events {
							event := &track.Events[i]
							if event.Color == danmaku.White {
								event.Y = 6
							} else if event.Color == 0x7dd3fc {
								event.Y = min(28, max(0, int(limit)-settings.FontSize))
							} else {
								event.Y = min(50, max(0, int(limit)-settings.FontSize))
							}
						}
						drawDanmaku(d, core.Bounds{Size: bounds}, track, 5300*time.Millisecond)
					}
				}},
				core.Text{Class: "absolute bottom-2 right-3 text-[10px] text-white/70", TestID: "phone-danmaku-preview-area",
					ContentFunc: func() string {
						return locales.PhoneDanmakuPreviewArea(cx, map[int]string{25: "1/4", 50: "1/2", 75: "3/4", 100: "100%"}[prefs().Area])
					}},
			}},
	}}
}

func (e danmakuEditor) phoneDanmakuBlockedUsers(cx *core.Scope, close func()) core.Node {
	draft := core.NewSignal(cx, e.prefs.GetUntracked().BlockedUsers)
	return core.View{Class: "gap-3", Children: []core.Node{
		hint(locales.PhoneDanmakuUsersHint(cx)),
		ui.Input{Label: locales.PhoneDanmakuBlockedUsers(cx), Placeholder: locales.DanmakuOneRulePerLine(cx),
			Multiline: true, NoAutoCorrect: true, Value: draft.Get, OnChange: draft.Set, TestID: "danmaku-user-input",
			Style: core.Style{Height: core.Pt(96)}},
		ui.Button{Text: locales.DanmakuSaveFilters(cx), TestID: "danmaku-save-users", OnPress: func() {
			users := strings.Join(danmakuRules(draft.GetUntracked()), "\n")
			if e.save(func(p *danmakuPreferences) { p.BlockedUsers = users }) {
				close()
			}
		}},
	}}
}

func phoneDanmakuCache(cx *core.Scope, group func(...core.Node) core.Node) core.Node {
	dir := cx.CacheDir()
	bytes := core.NewResource(cx, nil, func(context.Context, struct{}, core.ResourceInfo) (int64, error) {
		return danmaku.CachedCommentBytes(dir)
	})
	confirm := core.NewSignal(cx, false)
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	label := locales.PhoneDanmakuClear(cx)
	return core.View{Children: []core.Node{
		group(
			(phoneSetting{locales.PhoneDanmakuCache(cx), "", "bg-zinc-500", lucide.Database}).row(phoneSettingValueFunc(func() string {
				if value, ready := bytes.Get(); ready {
					return mediaBytes(value)
				}
				return "—"
			})),
			base.Button{Label: label, TestID: "phone-danmaku-clear-cache", Class: "h-12 items-center justify-center bg-white dark:bg-[#1a1b25]",
				Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)}, Disabled: func() bool { return dir == "" || busy.Get() },
				OnPress:  func(core.PressEvent) { confirm.Set(true) },
				Children: []core.Node{core.Text{Content: label, Class: "text-[15px] text-rose-500"}}},
		),
		settingsFailure(cx, failure),
		ui.AlertDialog{Open: confirm.Get, OnOpenChange: func(open bool, _ base.Reason) { confirm.Set(open) },
			Title: label, Description: locales.DesktopDanmakuClearCacheHint(cx), Actions: []ui.AlertAction{
				{Text: locales.DesktopSettingsCancel(cx), Role: core.AlertCancel},
				{Text: label, Role: core.AlertDestructive, Variant: ui.ButtonDanger, OnPress: func() {
					busy.Set(true)
					go func() {
						err := danmaku.ClearCachedComments(dir)
						cx.Post(func() {
							failure.Set(err)
							busy.Set(false)
							bytes.Refetch()
						})
					}()
				}},
			}},
	}}
}
