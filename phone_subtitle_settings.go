package main

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"waygu-video/locales"
)

func (a *application) phoneSubtitleSettings(cx *core.Scope) core.Node {
	failure := core.NewSignal[error](cx, nil)
	prefs := a.subtitlePrefs.Get
	save := func(change func(*subtitlePreferences)) {
		value := a.subtitlePrefs.GetUntracked()
		change(&value)
		failure.Set(storeSettings(a.storage, subtitlePreferencesKey, a.subtitlePrefs, value))
	}
	unavailable := func(label, tint string, icon func() *core.SVG) core.Node {
		return (phoneSetting{label, "", tint, icon}).unavailable(cx)
	}
	unavailableSwitch := func(label, tint string, icon func() *core.SVG) core.Node {
		return (phoneSetting{label, "", tint, icon}).toggle(cx, "", func() bool { return false }, nil)
	}
	return phoneSettingsPage(cx, locales.Subtitles(cx), a.back, "phone-subtitle-settings",
		subtitleLanguageSettings(cx, prefs, save, func() { a.push(route{screen: screenSubtitleLanguages}) }),
		setupGroup(locales.SettingsSubtitleStyle(cx), "", subtitleStyleRows(cx, prefs, save)...),
		setupGroup("ASS / SSA", locales.SettingsSubtitleASSDescription(cx),
			unavailableSwitch(locales.SettingsSubtitleASS(cx), "bg-fuchsia-500", lucide.Sparkles),
			unavailableSwitch(locales.SettingsSubtitleEmbeddedFonts(cx), "bg-fuchsia-500", lucide.Type),
		),
		setupGroup(locales.SettingsOnlineSubtitles(cx), "",
			unavailableSwitch(locales.SettingsDownloadSubtitles(cx), "bg-emerald-500", lucide.Download),
			unavailable("OpenSubtitles", "bg-emerald-600", lucide.Globe),
			unavailable(locales.SettingsSubtitleShooter(cx), "bg-blue-500", lucide.Globe),
			unavailable("SubHD", "bg-zinc-500", lucide.Globe),
			unavailable(locales.SettingsSubtitleSidecar(cx), "bg-zinc-500", lucide.FolderOpen),
		),
		settingsFailure(cx, failure),
	)
}

func subtitleLanguageSettings(cx *core.Scope, prefs func() subtitlePreferences, save func(func(*subtitlePreferences)), edit func()) core.Node {
	audio := []ui.Option[string]{{Label: locales.SettingsOriginalLanguage(cx)}}
	for _, code := range subtitleLanguages {
		audio = append(audio, ui.Option[string]{Value: code, Label: settingsLanguageName(cx, code)})
	}
	auto := []ui.Option[string]{
		{Value: "always", Label: locales.SettingsAlwaysOn(cx)},
		{Value: "foreign", Label: locales.SettingsSubtitleForeign(cx)},
		{Value: "off", Label: locales.SubtitlesOff(cx)},
	}
	languages := phoneSetting{locales.SettingsPreferredSubtitles(cx), "", "bg-sky-500", lucide.Languages}
	return setupGroup(locales.SettingsLanguages(cx), "",
		core.Dynamic(func(cx *core.Scope) core.Node {
			order := prefs().Languages
			names := make([]string, 0, len(order))
			for _, code := range order {
				names = append(names, settingsLanguageName(cx, code))
			}
			return languages.link(cx, "subtitle-language-order", strings.Join(names, " › "), edit)
		}),
		phoneSettingChoice(cx, phoneSetting{locales.SettingsPreferredAudio(cx), "", "bg-pink-500", lucide.AudioLines},
			"subtitle-audio-language", audio, func() string { return prefs().Audio },
			func(value string) { save(func(p *subtitlePreferences) { p.Audio = value }) }),
		phoneSettingChoice(cx, phoneSetting{locales.SettingsAutoSubtitle(cx), "", "bg-violet-500", lucide.WandSparkles},
			"subtitle-auto-mode", auto, func() string { return prefs().Mode },
			func(value string) { save(func(p *subtitlePreferences) { p.Mode = value }) }),
		(phoneSetting{locales.SettingsSubtitleConvert(cx), "", "bg-teal-500", lucide.Repeat}).toggle(cx, "subtitle-convert-chinese",
			func() bool { return !prefs().KeepOriginalChinese }, func(enabled bool) {
				save(func(p *subtitlePreferences) { p.KeepOriginalChinese = !enabled })
			}),
	)
}

func (a *application) playerSubtitleStyle(cx *core.Scope, hasEmbeddedSubtitles bool) core.Node {
	failure := core.NewSignal[error](cx, nil)
	save := func(change func(*subtitlePreferences)) {
		value := a.subtitlePrefs.GetUntracked()
		change(&value)
		failure.Set(storeSettings(a.storage, subtitlePreferencesKey, a.subtitlePrefs, value))
	}
	prefs := a.subtitlePrefs.Get
	style := core.View{Class: "gap-px overflow-hidden rounded-xl bg-white/10", Children: subtitleStyleRows(cx, prefs, save)}
	if !hasEmbeddedSubtitles {
		return core.View{Class: "gap-2", Children: []core.Node{style, settingsFailure(cx, failure)}}
	}
	edit := core.NewSignal(cx, false)
	return core.View{Class: "gap-2", Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
		if edit.Get() {
			return core.View{Class: "gap-2", Children: []core.Node{
				ui.Button{Text: locales.SettingsLanguages(cx), Icon: lucide.ChevronLeft, Variant: ui.ButtonGhost,
					TestID: "player-subtitle-language-back", OnPress: func() { edit.Set(false) }},
				subtitleLanguageOrder(cx, prefs, func(order []string) { save(func(p *subtitlePreferences) { p.Languages = order }) }),
			}}
		}
		ass := setupGroup("ASS / SSA", "",
			(phoneSetting{locales.SettingsSubtitleASS(cx), "", "bg-fuchsia-500", lucide.Sparkles}).toggle(cx, "subtitle-keep-ass",
				func() bool { return !prefs().FlattenASS }, func(enabled bool) { save(func(p *subtitlePreferences) { p.FlattenASS = !enabled }) }),
			(phoneSetting{locales.SettingsSubtitleEmbeddedFonts(cx), "", "bg-fuchsia-500", lucide.Type}).toggle(cx, "subtitle-embedded-fonts",
				func() bool { return !prefs().DisableEmbeddedFonts }, func(enabled bool) { save(func(p *subtitlePreferences) { p.DisableEmbeddedFonts = !enabled }) }),
		)
		return core.View{Class: "gap-2", Children: []core.Node{
			subtitleLanguageSettings(cx, prefs, save, func() { edit.Set(true) }), style, ass,
		}}
	}), settingsFailure(cx, failure)}}
}

func subtitleStyleRows(cx *core.Scope, prefs func() subtitlePreferences, save func(func(*subtitlePreferences))) []core.Node {
	backgrounds := []ui.Option[string]{
		{Value: "none", Label: locales.SettingsSubtitleBackgroundNone(cx)},
		{Value: "dim", Label: locales.SettingsSubtitleBackgroundDim(cx)},
		{Value: "solid", Label: locales.SettingsSubtitleBackgroundSolid(cx)},
	}
	positions := make([]ui.Option[int], 0, len(subtitlePositions))
	for _, distance := range subtitlePositions {
		positions = append(positions, ui.Option[int]{Value: distance, Label: locales.SettingsSubtitleDistance(cx, strconv.Itoa(distance))})
	}
	return []core.Node{
		subtitleStylePreview(cx, prefs),
		(phoneSetting{label: locales.SettingsSubtitleFont(cx)}).row(core.Text{Content: locales.SettingsSubtitleSystemFont(cx), Class: "text-[15px] " + setupMuted}),
		subtitleSizeSetting(cx, prefs, func(value int) { save(func(p *subtitlePreferences) { p.Scale = value }) }),
		subtitleColorSetting(cx, prefs, func(value uint32) { save(func(p *subtitlePreferences) { p.Color = value }) }),
		(phoneSetting{label: locales.SettingsSubtitleStroke(cx)}).unavailable(cx),
		phoneSettingChoice(cx, phoneSetting{label: locales.SettingsSubtitleBackground(cx)}, "subtitle-background", backgrounds,
			func() string { return prefs().Background }, func(value string) { save(func(p *subtitlePreferences) { p.Background = value }) }),
		phoneSettingChoice(cx, phoneSetting{label: locales.SettingsSubtitlePosition(cx)}, "subtitle-position", positions,
			func() int { return prefs().Position }, func(value int) { save(func(p *subtitlePreferences) { p.Position = value }) }),
		(phoneSetting{label: locales.SettingsSubtitleBilingual(cx)}).toggle(cx, "subtitle-bilingual",
			func() bool { return !prefs().DisableBilingual }, func(enabled bool) {
				save(func(p *subtitlePreferences) { p.DisableBilingual = !enabled })
			}),
	}
}

func (a *application) phoneSubtitleLanguages(cx *core.Scope) core.Node {
	failure := core.NewSignal[error](cx, nil)
	save := func(order []string) {
		value := a.subtitlePrefs.GetUntracked()
		value.Languages = order
		failure.Set(storeSettings(a.storage, subtitlePreferencesKey, a.subtitlePrefs, value))
	}
	return phoneSettingsPage(cx, locales.SettingsPreferredSubtitles(cx), a.back, "phone-subtitle-languages",
		subtitleLanguageOrder(cx, a.subtitlePrefs.Get, save), settingsFailure(cx, failure))
}

func subtitleLanguageOrder(cx *core.Scope, prefs func() subtitlePreferences, save func([]string)) core.Node {
	return core.Group([]core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node {
			order := prefs().Languages
			rows := make([]core.Node, 0, len(order))
			for index, code := range order {
				name := settingsLanguageName(cx, code)
				reorder := func(other int) {
					changed := slices.Clone(order)
					changed[index], changed[other] = changed[other], changed[index]
					save(changed)
				}
				rows = append(rows, (phoneSetting{label: name}).row(core.View{Class: "flex-row gap-1", Children: []core.Node{
					ui.IconButton{Label: locales.SettingsRaiseSubtitleLanguage(cx, name), Size: 44, Icon: lucide.ArrowUp,
						TestID: "subtitle-language-up-" + code, Disabled: func() bool { return index == 0 },
						OnPress: func() {
							if index > 0 {
								reorder(index - 1)
							}
						}},
					ui.IconButton{Label: locales.SettingsLowerSubtitleLanguage(cx, name), Size: 44, Icon: lucide.ArrowDown,
						TestID: "subtitle-language-down-" + code, Disabled: func() bool { return index+1 == len(order) },
						OnPress: func() {
							if index+1 < len(order) {
								reorder(index + 1)
							}
						}},
					ui.IconButton{Label: locales.SettingsRemoveSubtitleLanguage(cx, name), Size: 44, Icon: lucide.X,
						TestID: "subtitle-language-remove-" + code, Disabled: func() bool { return len(order) == 1 },
						OnPress: func() {
							if len(order) > 1 {
								save(slices.Delete(slices.Clone(order), index, index+1))
							}
						}},
				}}))
			}
			return setupGroup(locales.SettingsPreferredSubtitles(cx), "", rows...)
		}),
		core.Dynamic(func(cx *core.Scope) core.Node {
			order := prefs().Languages
			var rows []core.Node
			for _, code := range subtitleLanguages {
				if !slices.Contains(order, code) {
					setting := phoneSetting{label: settingsLanguageName(cx, code), icon: lucide.Plus, tint: "bg-violet-500"}
					rows = append(rows, setting.link(cx, "subtitle-language-add-"+code, "", func() { save(append(slices.Clone(order), code)) }))
				}
			}
			if len(rows) == 0 {
				return nil
			}
			return setupGroup(locales.SettingsAddSubtitleLanguage(cx), "", rows...)
		}),
	})
}

func settingsLanguageName(cx *core.Scope, code string) string {
	if code == "zh-Hans" {
		return locales.SettingsSimplifiedChinese(cx)
	}
	if code == "zh-Hant" {
		return locales.SettingsTraditionalChinese(cx)
	}
	names := display.Languages(language.Make(cx.Env().Locale()))
	if names == nil {
		names = display.Languages(language.English)
	}
	return cmp.Or(names.Name(language.Make(code)), code)
}

func subtitleStylePreview(cx *core.Scope, prefs func() subtitlePreferences) core.Node {
	sample := locales.SettingsSubtitleSample(cx)
	translation := "We choose hope."
	if !strings.HasPrefix(cx.Env().Locale(), "zh") {
		translation = "我们选择希望。"
	}
	text := core.NewMemo(cx, func() string { return convertSubtitleText(sample, "", prefs().chineseCaptionScript()) })
	return core.View{Class: "relative h-48 bg-white dark:bg-[#1a1b25]", Children: []core.Node{
		core.View{Class: "h-40 items-center justify-center bg-[#f3f3f3] dark:bg-[#262626]", Children: []core.Node{
			core.Icon{SVG: lucide.Image(), Class: "size-7 text-neutral-500"},
		}},
		core.Text{Content: locales.SettingsSubtitlePreview(cx), Class: "absolute left-2.5 top-2.5 rounded-md bg-black/50 px-2 py-0.5 text-[10px] text-white"},
		core.View{Class: "absolute inset-x-0 items-center gap-0.5", StyleFunc: func() core.Style {
			return core.Style{Bottom: core.Pt(core.Points(8 + float64(prefs().Position)*1.92))}
		}, Children: []core.Node{
			core.Text{ContentFunc: text.Get, TestID: "subtitle-preview-sample",
				Class: "rounded px-2 font-semibold text-center", StyleFunc: func() core.Style {
					p := prefs()
					style := p.captionStyle()
					if p.Background == "none" {
						style.Background = core.RGBA(0, 0.3)
						style.TextShadowColor = 0
						style.TextShadowOffset = core.Point{}
						style.TextShadowRadius = 0
					}
					return style
				}},
			core.Show{When: func() bool { return !prefs().DisableBilingual }, Then: func(*core.Scope) core.Node {
				return core.Text{ContentFunc: func() string { return convertSubtitleText(translation, "", prefs().chineseCaptionScript()) },
					TestID: "subtitle-preview-secondary", Class: "text-center font-medium", StyleFunc: func() core.Style {
						p := prefs()
						style := p.secondaryCaptionStyle()
						if p.Background == "none" {
							style.TextShadowColor, style.TextShadowOffset, style.TextShadowRadius = 0, core.Point{}, 0
						}
						return style
					}}
			}},
		}},
	}}
}

func subtitleSizeSetting(cx *core.Scope, prefs func() subtitlePreferences, change func(int)) core.Node {
	label := locales.SettingsSubtitleSize(cx)
	return core.View{Class: "gap-2 bg-white px-4 py-3 dark:bg-[#1a1b25]", Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between", Children: []core.Node{
			core.Text{Content: label, Class: "text-[15px] " + setupTitle},
			core.Text{ContentFunc: func() string { return strconv.Itoa(prefs().Scale) + "%" }, Class: "text-sm font-medium text-violet-500"},
		}},
		base.Slider{Label: label, TestID: "subtitle-font-size", ThumbTestID: "subtitle-font-size-thumb",
			Min: func() float64 { return 50 }, Max: func() float64 { return 150 }, Step: func() float64 { return 25 },
			Value:    func() base.SliderValue { return base.SingleValue(float64(prefs().Scale)) },
			OnChange: func(value base.SliderValue) { change(int(value.End)) },
			Class:    "h-5", TrackClass: "h-full justify-center",
			IndicatorClass: "h-1.5 rounded-full bg-black/10 dark:bg-white/10",
			RangeClass:     "rounded-l-full bg-violet-500",
			ThumbClass:     "size-5 rounded-full border border-black/5 bg-white shadow-md",
			ThumbStyle:     core.Style{FocusVisible: ui.FocusRing(cx, 2)}},
	}}
}

func subtitleColorSetting(cx *core.Scope, prefs func() subtitlePreferences, change func(uint32)) core.Node {
	swatches := make([]core.Node, 0, len(subtitleColors)+1)
	for index, color := range subtitleColors {
		title := locales.SettingsSubtitleColor(cx) + " #" + strconv.FormatUint(uint64(color), 16)
		swatches = append(swatches, base.Button{Label: title, TestID: "subtitle-color-" + strconv.Itoa(index),
			Class:    "size-8 items-center justify-center rounded-full border border-zinc-900/8 dark:border-white/8 selected:ring-2 selected:ring-violet-500 selected:ring-offset-2 selected:ring-offset-card",
			Selected: func() bool { return prefs().Color == color },
			Style:    core.Style{Background: core.RGB(color), FocusVisible: ui.FocusRing(cx, 2)},
			OnPress:  func(core.PressEvent) { change(color) }, Children: []core.Node{
				core.Show{When: func() bool { return prefs().Color == color }, Then: func(*core.Scope) core.Node {
					return core.Icon{SVG: lucide.Check(), Class: "size-4 text-zinc-900"}
				}},
			}})
	}
	open := core.NewSignal(cx, false)
	draft := core.NewSignal(cx, core.Color(0))
	title := locales.SettingsCustomSubtitleColor(cx)
	swatches = append(swatches, base.Button{Label: title, TestID: "subtitle-custom-color",
		Class:    "size-8 items-center justify-center rounded-full border border-zinc-900/8 dark:border-white/8",
		Style:    core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress:  func(core.PressEvent) { draft.Set(core.RGB(prefs().Color)); open.Set(true) },
		Children: []core.Node{core.Icon{SVG: lucide.Plus(), Class: "size-4 " + setupMuted}},
	})
	apply := func() {
		change(uint32(draft.GetUntracked()) >> 8)
		open.Set(false)
	}
	dialog := core.Show{When: open.Get, Then: func(*core.Scope) core.Node {
		return ui.Dialog{Title: title, Open: open.Get, TestID: "subtitle-custom-color-dialog",
			OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) }, OnConfirm: apply,
			Children: []core.Node{ui.ColorPicker{Value: draft.Get, NoAlpha: true, OnChange: func(color core.Color, _ string) { draft.Set(color) }}},
			Footer:   []core.Node{ui.Button{Text: locales.TracksDone(cx), TestID: "subtitle-custom-color-done", OnPress: apply}},
		}
	}}

	return core.View{Class: "gap-2.5 bg-white px-4 py-3 dark:bg-[#1a1b25]", Children: []core.Node{
		core.Text{Content: locales.SettingsSubtitleColor(cx), Class: "text-[15px] " + setupTitle},
		core.View{Class: "flex-row flex-wrap gap-3", Children: swatches}, dialog,
	}}
}
