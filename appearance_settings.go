package main

import (
	"math"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

func appearanceModeLabel(cx *core.Scope, mode string) string {
	switch mode {
	case "light":
		return locales.TvSettingsLight(cx)
	case "dark":
		return locales.TvSettingsDark(cx)
	default:
		return locales.SettingsFollowSystem(cx)
	}
}

func (a *application) phoneAppearanceSettings(cx *core.Scope) core.Node {
	return phoneSettingsPage(cx, locales.SettingsAppearance(cx), a.back, "phone-appearance-settings", a.appearanceSettings(cx))
}

func (a *application) appearanceSettings(cx *core.Scope) core.Node {
	prefs := a.appearancePrefs.Get
	failure := core.NewSignal[error](cx, nil)
	change := func(edit func(*appearancePreferences)) { failure.Set(a.saveAppearance(edit)) }
	theme := appearanceSwatches(locales.AppearanceTheme(cx), func() core.Node {
		var choices []core.Node
		for _, mode := range []string{"light", "dark", "system"} {
			title := appearanceModeLabel(cx, mode)
			choices = append(choices, base.Radio{Label: title, TestID: "appearance-theme-" + mode,
				Checked:  func() bool { return a.appearance.Get() == mode },
				OnChange: func(bool) { failure.Set(storeSettingValue(a.storage, appearanceKey, a.appearance, mode, mode)) },
				Class:    "group min-w-0 flex-1 items-center gap-2.5 rounded-xl", Style: core.Style{FocusVisible: ui.FocusRing(cx, 4)},
				Children: []core.Node{
					appearanceThemePreview(mode),
					core.Text{Content: title, Class: "text-sm group-selected:font-semibold group-selected:text-text " + setupMuted, MaxLines: 1},
					core.View{Class: "size-5 items-center justify-center rounded-full border-2 border-zinc-900/8 group-selected:border-waygu-control group-selected:bg-waygu-control group-selected:shadow-sm group-selected:shadow-waygu-control/40 dark:border-white/8",
						Children: []core.Node{core.Icon{SVG: lucide.Check(), Class: "size-3 text-transparent group-selected:text-white"}}},
				}})
		}
		return base.RadioGroup{Label: locales.AppearanceTheme(cx), Class: "flex-row gap-3 rounded-2xl p-4 shadow-sm shadow-black/5 " + desktopLibrarySurface, Children: choices}
	}())
	night := phoneSetting{locales.AppearanceNight(cx), locales.AppearanceNightHint(cx), "bg-indigo-500", lucide.MoonStar}
	oled := phoneSetting{locales.AppearanceOLED(cx), locales.AppearanceOLEDHint(cx), "bg-zinc-800", lucide.Contrast}
	watched := phoneSetting{locales.AppearanceWatchedMarks(cx), "", "bg-emerald-500", lucide.CircleCheck}
	quality := phoneSetting{locales.AppearanceQualityMarks(cx), "", "bg-sky-500", lucide.BadgeInfo}
	finished := phoneSetting{locales.AppearanceHideFinished(cx), "", "bg-zinc-500", lucide.EyeOff}
	ambient := phoneSetting{locales.AppearancePosterAmbient(cx), "", "bg-violet-500", lucide.Film}
	var swatches []core.Node
	names := []string{locales.AppearanceViolet(cx), locales.AppearanceBlue(cx), locales.AppearanceGreen(cx), locales.AppearanceAmber(cx), locales.AppearanceRose(cx), locales.AppearanceGradient(cx)}
	for i, option := range appearanceAccents {
		swatches = append(swatches, base.Radio{Label: names[i], TestID: "appearance-accent-" + option.id,
			Class: "group size-9 shrink-0 items-center justify-center rounded-full shadow-sm selected:shadow-md selected:ring-2 selected:ring-waygu-control/50 selected:ring-offset-2 selected:ring-offset-card " + option.swatch,
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 4)}, Checked: func() bool { return prefs().Accent == option.id },
			OnChange: func(bool) { change(func(p *appearancePreferences) { p.Accent = option.id }) },
			Children: []core.Node{core.Icon{SVG: lucide.Check(), Class: "size-4 text-transparent group-selected:text-white"}}})
	}
	accents := appearanceSwatches(locales.AppearanceAccent(cx), base.RadioGroup{Label: locales.AppearanceAccent(cx),
		Class: "flex-row justify-between rounded-2xl px-5 py-4 shadow-sm shadow-black/5 " + desktopLibrarySurface, Children: swatches})
	poster := phoneSetting{locales.AppearancePosterStyle(cx), "", "bg-orange-500", lucide.LayoutGrid}
	language := phoneSetting{locales.AppearanceLanguage(cx), "", "bg-blue-500", lucide.Languages}
	languages := []ui.Option[string]{
		{Label: locales.SettingsFollowSystem(cx)}, {Value: "zh-Hans", Label: "简体中文"}, {Value: "en", Label: "English"},
	}
	return core.View{Class: "gap-6", Children: []core.Node{
		theme,
		setupGroup("", "",
			night.toggle(cx, "appearance-night", func() bool { return prefs().Night }, func(value bool) {
				err := a.saveAppearance(func(p *appearancePreferences) {
					p.Night = value
					if !value {
						p.SolarLocation = nil
					}
				})
				failure.Set(err)
				if err == nil && value {
					a.findAppearanceSunset(cx, failure.Set)
				}
			}),
			oled.toggle(cx, "appearance-oled", func() bool { return prefs().OLED },
				func(value bool) { change(func(p *appearancePreferences) { p.OLED = value }) })),
		accents,
		appearanceAppIcons(cx),
		setupGroup(locales.AppearanceHomeModules(cx), locales.AppearanceReorderHint(cx), a.appearanceHomeRows(cx, failure.Set)),
		setupGroup(locales.AppearanceLibraryDisplay(cx), "",
			phoneSettingChoice(cx, poster, "appearance-poster-style", []ui.Option[string]{
				{Value: "portrait", Label: locales.AppearancePortrait(cx)},
				{Value: "landscape", Label: locales.AppearanceLandscape(cx)},
				{Value: "list", Label: locales.LibraryListView(cx)},
			}, func() string { return prefs().Poster }, func(value string) {
				err := a.saveAppearance(func(p *appearancePreferences) { p.Poster = value })
				failure.Set(err)
				if err == nil {
					a.desktopLibraryState.Update(func(state desktopLibraryState) desktopLibraryState {
						state.mode = preferredDesktopLibraryMode(a.appearancePrefs.GetUntracked())
						return state
					})
				}
			}),
			(phoneSetting{locales.AppearanceRatingSource(cx), "", "bg-amber-500", lucide.Star}).row(phoneSettingValue(locales.AppearanceServerRating(cx))),
			watched.toggle(cx, "appearance-watched", func() bool { return prefs().ShowWatched },
				func(value bool) { change(func(p *appearancePreferences) { p.ShowWatched = value }) }),
			quality.toggle(cx, "appearance-quality", func() bool { return prefs().ShowQuality },
				func(value bool) { change(func(p *appearancePreferences) { p.ShowQuality = value }) }),
			finished.toggle(cx, "appearance-hide-finished", func() bool { return prefs().HideFinished },
				func(value bool) { change(func(p *appearancePreferences) { p.HideFinished = value }) })),
		setupGroup(locales.DesktopSettingsGeneral(cx), "",
			phoneSettingChoice(cx, language, "appearance-language", languages, func() string { return a.desktopPrefs.Get().Language },
				func(value string) {
					err := a.saveDesktopPreferences(func(p *desktopPreferences) { p.Language = value })
					failure.Set(err)
					if err == nil {
						a.scope.SetLocale(value)
					}
				}),
			ambient.toggle(cx, "appearance-poster-ambient", func() bool { return prefs().PosterAmbient },
				func(value bool) { change(func(p *appearancePreferences) { p.PosterAmbient = value }) })),
		settingsFailure(cx, failure),
	}}
}

func appearanceSwatches(label string, content core.Node) core.Node {
	return core.View{Class: "gap-2 px-4", Children: []core.Node{
		core.Text{Content: label, Class: "px-4 text-xs font-medium uppercase tracking-wide " + setupMuted},
		content,
	}}
}

func appearanceThemePreview(mode string) core.Node {
	const preview = "h-32 w-full overflow-hidden rounded-xl shadow-sm group-selected:ring-2 group-selected:ring-waygu-control group-selected:ring-offset-2 group-selected:ring-offset-card"
	if mode == "system" {
		return core.View{Class: preview + " flex-row group-selected:shadow-md group-selected:shadow-waygu-control/20", Children: []core.Node{
			appearancePreviewPane(false, true), appearancePreviewPane(true, true),
		}}
	}
	border := "border-zinc-200"
	if mode == "dark" {
		border = "border-zinc-800"
	}
	return core.View{Class: preview + " border " + border, Children: []core.Node{appearancePreviewPane(mode == "dark", false)}}
}

func appearancePreviewPane(dark, half bool) core.Node {
	background, title, banner, tile, bar, dot := "bg-white", "bg-zinc-300", "from-violet-200 via-fuchsia-100 to-sky-100", "bg-zinc-200", "border-zinc-200 bg-zinc-50", "bg-zinc-300"
	if dark {
		background, title, banner, tile, bar, dot = "bg-zinc-950", "bg-zinc-700", "from-violet-900 via-fuchsia-950 to-zinc-900", "bg-zinc-800", "border-zinc-800 bg-zinc-900", "bg-zinc-600"
	}
	inset, text, corners, bottom, header := "p-2", "w-8 rounded-full", "rounded-md", "rounded-full border", "h-2.5 flex-row items-center gap-1"
	if half {
		inset, text, corners, bottom = "py-2 pl-2", "flex-1 rounded-l-full", "rounded-l-md", "rounded-l-full border-y border-l"
		if dark {
			inset, text, corners, bottom = "py-2 pr-2", "w-3 rounded-r-full", "rounded-r-md", "rounded-r-full border-y border-r"
			header = "flex-row items-center"
			banner = "from-fuchsia-950 to-zinc-900"
		}
	}
	mark := core.View{Class: "size-2.5 shrink-0 rounded-[3px] bg-linear-to-br from-violet-500 to-fuchsia-500"}
	var row []core.Node
	if !half || !dark {
		row = append(row, mark)
	}
	row = append(row, core.View{Class: "h-1.5 " + text + " " + title})
	var tiles []core.Node
	for i := 0; i < 3; i++ {
		if half && i == 2 {
			break
		}
		radius := "rounded-[4px]"
		if half {
			switch {
			case dark && i == 0:
				radius = "rounded-r-[4px]"
			case !dark && i == 1:
				radius = "rounded-l-[4px]"
			}
		}
		tiles = append(tiles, core.View{Class: "h-8 flex-1 " + radius + " " + tile})
	}
	var dots []core.Node
	if !half {
		color := "bg-violet-500"
		if dark {
			color = "bg-violet-400"
		}
		for _, tint := range []string{color, dot, dot} {
			dots = append(dots, core.View{Class: "size-1 rounded-full " + tint})
		}
	}
	return core.View{Class: "min-w-0 flex-1 gap-1.5 " + inset + " " + background, Children: []core.Node{
		core.View{Class: header, Children: row},
		core.View{Class: "h-9 bg-linear-to-br " + corners + " " + banner},
		core.View{Class: "flex-row gap-1", Children: tiles},
		core.View{Class: "mt-auto h-3 flex-row items-center justify-around " + bottom + " " + bar, Children: dots},
	}}
}

func appearanceAppIcons(cx *core.Scope) core.Node {
	type iconChoice struct{ label, id, shape, tint string }
	icons := []iconChoice{
		{locales.AppearanceIconDefault(cx), "default", "bg-linear-to-br from-violet-500 to-fuchsia-500 ring-2 ring-waygu-control ring-offset-2 ring-offset-card shadow-violet-500/30", "text-white"},
		{locales.AppearanceIconMidnight(cx), "midnight", "bg-zinc-950 ring-1 ring-black/10 dark:ring-white/10", "text-violet-400"},
		{locales.AppearanceIconWhite(cx), "white", "bg-white ring-1 ring-black/10", "text-violet-600"},
		{locales.AppearanceIconSunset(cx), "sunset", "bg-linear-to-br from-amber-400 to-rose-500", "text-white"},
	}
	var cards []core.Node
	for index, icon := range icons {
		label := setupMuted
		if index == 0 {
			label = "font-medium " + setupTitle
		}
		cards = append(cards, base.Radio{Label: icon.label, TestID: "appearance-icon-" + icon.id, Hint: locales.AppearanceAlternateIconUnavailable(cx),
			Checked: func() bool { return index == 0 }, Disabled: func() bool { return true }, Class: "items-center gap-1.5", Children: []core.Node{
				core.View{Class: "size-14 items-center justify-center rounded-2xl shadow-md " + icon.shape, Children: []core.Node{
					core.Icon{SVG: lucide.Play(), Class: "size-6 " + icon.tint},
				}},
				core.Text{Content: icon.label, Class: "text-xs " + label},
			}})
	}
	title := locales.AppearanceAppIcons(cx)
	return appearanceSwatches(title, base.RadioGroup{Label: title, Class: "flex-row justify-between rounded-2xl px-5 py-4 shadow-sm shadow-black/5 " + desktopLibrarySurface, Children: cards})
}

func homeModuleLabel(cx *core.Scope, id string) string {
	switch id {
	case homeContinue:
		return locales.ContinueWatching(cx)
	case homeRecent:
		return locales.Recent(cx)
	case homeSeries:
		return locales.HomeSeriesUpdates(cx)
	case homeFavorite:
		return locales.AppearanceFavorites(cx)
	case homeGrouped:
		return locales.AppearanceGroupSources(cx)
	default:
		return locales.AppearanceRandom(cx)
	}
}

func (a *application) appearanceHomeRows(cx *core.Scope, fail func(error)) core.Node {
	drag := core.NewSignal(cx, sourceCardDrag{})
	idiom := appLayoutIdiom(cx)
	groupedByDefault := idiom == core.IdiomDesktop || idiom == core.IdiomTV
	modules := func() []homeModule { return a.appearancePrefs.Get().forHome(groupedByDefault).Home }
	rows := core.Each(modules, func(module homeModule) string { return module.ID }, func(cx *core.Scope, module func() homeModule, index func() int) core.Node {
		id := core.Untracked(cx, module).ID
		label := homeModuleLabel(cx, id)
		move := func(delta int) { fail(a.moveHomeModule(id, delta)); drag.Set(sourceCardDrag{}) }
		handle := core.View{Role: core.RoleButton, Label: locales.AppearanceMoveModule(cx, label), TestID: "appearance-module-move-" + id,
			Class: "size-8 shrink-0 items-center justify-center rounded", Style: core.Style{FocusVisible: ui.FocusRing(cx, -2)},
			PanActivation: core.PanActivation{ActiveOffsetY: 8, FailOffsetX: 16},
			OnKeyDown: func(key core.KeyEvent) bool {
				switch key.Key {
				case "up":
					move(-1)
				case "down":
					move(1)
				default:
					return false
				}
				return true
			},
			OnPan: func(event core.PanEvent) {
				switch event.Phase {
				case core.GestureBegan, core.GestureChanged:
					drag.Set(sourceCardDrag{id: id, offset: event.Translation.Y})
				case core.GestureEnded:
					from := index()
					to := max(0, min(len(a.appearancePrefs.GetUntracked().Home)-1, from+int(math.Round(float64(event.Translation.Y)/52))))
					move(to - from)
				default:
					drag.Set(sourceCardDrag{})
				}
			}, Children: []core.Node{core.Icon{SVG: lucide.GripVertical(), Class: "size-4 " + setupMuted}}}
		return core.View{Class: "h-[51px] flex-row items-center gap-1 bg-white pl-4 pr-2 dark:bg-[#1a1b25]", StyleFunc: func() core.Style {
			if current := drag.Get(); current.id == id {
				return core.Style{ZIndex: 2, Transform: core.Transform{TranslateY: current.offset}}
			}
			return core.Style{}
		}, Children: []core.Node{
			core.Text{Content: label, Class: "flex-1 text-[15px] " + setupTitle},
			base.Switch{Label: label, TestID: "appearance-module-" + id, Checked: func() bool { return module().Enabled },
				OnChange: func(checked bool) { fail(a.saveHomeModule(id, checked)) },
				Style:    core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Class: "shrink-0 rounded-full",
				TrackClass: "h-[31px] w-[51px] flex-row items-center justify-start rounded-full bg-zinc-900/15 px-0.5 checked:justify-end checked:bg-waygu-control dark:bg-white/15",
				ThumbClass: "size-[27px] rounded-full bg-white shadow-md"},
			handle,
		}}
	})
	return core.View{Class: "gap-px", Children: []core.Node{rows}}
}
