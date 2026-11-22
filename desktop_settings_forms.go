package main

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

const desktopPreferencesKey = "desktop-preferences-v1"
const desktopLastPageKey = "desktop-last-page-v1"

type desktopPreferences struct {
	Language, DefaultSource  string
	StartLast                bool
	Back, Forward, Countdown int
}

type desktopLastPage struct {
	Tab      int
	Category libraryCategory
	Library  screen
}

func readDesktopPreferences(storage core.Storage) desktopPreferences {
	prefs := desktopPreferences{Back: 10, Forward: 10, Countdown: 10}
	if raw, ok := storage.GetItem(desktopPreferencesKey); ok {
		_ = json.Unmarshal([]byte(raw), &prefs)
	}
	if !slices.Contains([]string{"", "zh-Hans", "en"}, prefs.Language) {
		prefs.Language = ""
	}
	if !slices.Contains(seekIntervals, prefs.Back) {
		prefs.Back = 10
	}
	if !slices.Contains(seekIntervals, prefs.Forward) {
		prefs.Forward = 10
	}
	if !slices.Contains([]int{5, 10, 20}, prefs.Countdown) {
		prefs.Countdown = 10
	}
	return prefs
}

func (a *application) initDesktopPreferences(cx *core.Scope, first screen) {
	prefs := core.Untracked(cx, func() desktopPreferences { return readDesktopPreferences(a.storage) })
	a.desktopPrefs = core.NewSignal(cx, prefs)
	a.desktopSettingsTab = core.NewSignal(cx, desktopSettingsOverview)
	if prefs.Language != "" {
		cx.SetLocale(prefs.Language)
	}
	if appLayoutIdiom(cx) != core.IdiomDesktop {
		return
	}
	id := a.desktopDefaultSource()
	a.sourceFilter.Set(id)
	if prefs.StartLast && first == screenHome {
		var last desktopLastPage
		restored := core.Untracked(cx, func() bool {
			raw, ok := a.storage.GetItem(desktopLastPageKey)
			return ok && json.Unmarshal([]byte(raw), &last) == nil
		})
		if restored {
			switch last.Tab {
			case 1:
				category := last.Category
				switch category {
				case libraryMovies, librarySeries, libraryAnime, libraryDocumentaries:
				default:
					category = libraryMovies
				}
				a.openDesktopLibrary(category, id)
				if last.Library == screenFavorites || last.Library == screenHistory {
					a.libraryView.Set(last.Library)
				}
			case 2:
				a.sourceFilter.Set("")
				a.tab.Set(2)
			}
		}
	}
	cx.Effect(func() {
		if appLayoutIdiom(cx) != core.IdiomDesktop {
			return
		}
		tab := a.tab.Get()
		if tab < 0 || tab > 2 {
			return
		}
		page := desktopLastPage{Tab: tab}
		if tab == 1 {
			page.Category = a.desktopCategory.Get()
			page.Library = a.libraryView.Get()
		}
		encoded, _ := json.Marshal(page)
		_ = a.storage.SetItem(desktopLastPageKey, string(encoded))
	})
}

func (a *application) desktopDefaultSource() string {
	id := a.desktopPrefs.GetUntracked().DefaultSource
	if id != "" && slices.ContainsFunc(a.sources.GetUntracked(), func(entry source) bool { return entry.ID == id }) {
		return id
	}
	return ""
}

func (a *application) saveDesktopPreferences(change func(*desktopPreferences)) error {
	current := a.desktopPrefs.GetUntracked()
	change(&current)
	return storeSettings(a.storage, desktopPreferencesKey, a.desktopPrefs, current)
}

const desktopSettingsRow = "min-w-0 flex-row items-center gap-3 bg-white px-4 dark:bg-[#1a1b25]"

func desktopSettingsHeading(title, description string) core.Node {
	rows := []core.Node{core.Text{Content: title, Class: "text-2xl font-bold " + setupTitle, Role: core.RoleHeader}}
	if description != "" {
		rows = append(rows, core.Text{Content: description, Class: "text-sm " + setupMuted})
	}
	return core.View{Class: "min-w-0 flex-1 gap-1", Children: rows}
}

func desktopFormGroup(title string, rows ...core.Node) core.Node {
	return core.View{Class: "min-w-0 gap-2", Children: []core.Node{
		core.Text{Content: title, Class: "px-1 text-xs font-medium " + setupMuted},
		core.View{Class: "gap-px overflow-hidden rounded-xl border border-zinc-900/8 bg-zinc-200 shadow-sm shadow-black/5 dark:border-white/8 dark:bg-white/7", Children: rows},
	}}
}

func desktopFormRow(label string, trailing core.Node) core.Node {
	return desktopFormRowHeight(label, "h-11", trailing)
}

func desktopFormRowHeight(label, height string, trailing core.Node) core.Node {
	return core.View{Class: height + " " + desktopSettingsRow, Children: []core.Node{
		core.Text{Content: label, Class: "min-w-0 flex-1 text-sm " + setupTitle, MaxLines: 2}, trailing,
	}}
}

func desktopFormDetail(label, detail string, trailing core.Node) core.Node {
	return core.View{Class: "min-h-14 " + desktopSettingsRow, Children: []core.Node{
		core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
			core.Text{Content: label, Class: "text-sm " + setupTitle},
			core.Text{Content: detail, Class: "text-xs " + setupMuted},
		}},
		trailing,
	}}
}

func desktopFormSelect[T comparable](cx *core.Scope, id, label string, choices []ui.Option[T], value func() T, change func(T)) core.Node {
	items := make([]base.SelectItem, 0, len(choices))
	for i, item := range choices {
		items = append(items, base.SelectItem{Value: strconv.Itoa(i), Title: item.Label})
	}
	selected := func() *string {
		chosen := value()
		for i, item := range choices {
			if item.Value == chosen {
				key := strconv.Itoa(i)
				return &key
			}
		}
		return nil
	}
	return base.Select{Label: label, TestID: id, RootClass: "shrink-0 max-w-[56%]",
		Class: "h-7 rounded-md shadow-sm shadow-black/5 " + desktopLibrarySurface,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup, ItemClass: tabletDetailOption,
		Items: func() []base.SelectItem { return items }, Value: selected,
		OnChange: func(key *string) {
			if key != nil && change != nil {
				if i, err := strconv.Atoi(*key); err == nil && i >= 0 && i < len(choices) {
					change(choices[i].Value)
				}
			}
		}, Disabled: func() bool { return change == nil }, RenderItem: tabletDetailOptionText,
		Trigger: core.View{Class: "min-w-0 flex-row items-center gap-2 px-2.5", Children: []core.Node{
			core.Text{ContentFunc: func() string {
				if key := selected(); key != nil {
					i, _ := strconv.Atoi(*key)
					return items[i].Title
				}
				return locales.MetadataUnavailable(cx)
			}, Class: "min-w-0 shrink text-[13px] " + setupTitle, MaxLines: 1},
			core.Icon{SVG: lucide.ChevronsUpDown(), Class: "size-3.5 shrink-0 " + setupMuted},
		}}}
}

func desktopFormToggle(cx *core.Scope, id, label string, value func() bool, change func(bool)) core.Node {
	return base.Switch{Label: label, TestID: id, Checked: value, OnChange: change, Disabled: func() bool { return change == nil },
		Class: "shrink-0 rounded-full disabled:opacity-60", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		TrackClass: "h-[22px] w-[38px] flex-row items-center justify-start rounded-full bg-zinc-900/15 px-0.5 checked:justify-end checked:bg-waygu-control dark:bg-white/15",
		ThumbClass: "size-[18px] rounded-full bg-white shadow-md"}
}

func desktopFormSegments[T comparable](cx *core.Scope, id, label string, choices []ui.Option[T], value func() T, change func(T)) core.Node {
	var nodes []core.Node
	for _, option := range choices {
		suffix := cmp.Or(option.TestID, strconv.Itoa(len(nodes)))
		nodes = append(nodes, base.Radio{Label: option.Label, TestID: id + "-" + suffix,
			Checked: func() bool { return value() == option.Value }, OnChange: func(bool) { change(option.Value) },
			Class: "group items-center justify-center rounded px-3 selected:bg-white selected:shadow-sm dark:selected:bg-white/15",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Children: []core.Node{
				core.Text{Content: option.Label, Class: "text-xs " + setupMuted + " group-selected:font-medium group-selected:text-zinc-900 dark:group-selected:text-zinc-50", MaxLines: 1},
			}})
	}
	return base.RadioGroup{Label: label, Class: "h-7 shrink-0 flex-row rounded-md bg-zinc-900/5 p-0.5 dark:bg-white/5", Children: nodes}
}

func (a *application) desktopAppearanceRow(cx *core.Scope, failure *core.Signal[error]) core.Node {
	title := locales.DesktopSettingsTheme(cx)
	return desktopFormRowHeight(title, "h-12", desktopFormSegments(cx, "desktop-appearance", title, []ui.Option[string]{
		{Label: locales.SettingsFollowSystem(cx), Value: "system", TestID: "system"},
		{Label: locales.TvSettingsDark(cx), Value: "dark", TestID: "dark"},
		{Label: locales.TvSettingsLight(cx), Value: "light", TestID: "light"},
	}, a.appearance.Get, func(value string) {
		failure.Set(storeSettingValue(a.storage, appearanceKey, a.appearance, value, value))
	}))
}

func (a *application) desktopSettingsGeneral(cx *core.Scope) core.Node {
	failure := core.NewSignal[error](cx, nil)
	prefs := a.desktopPrefs.Get
	language := locales.DesktopSettingsLanguage(cx)
	theSource := locales.DesktopSettingsDefaultSource(cx)
	start := locales.DesktopSettingsOpenOnLaunch(cx)
	login := locales.DesktopSettingsLogin(cx)
	launch := core.NewSignal(cx, false)
	var changeLogin func(bool)
	if desktop.IsAvailable(cx) {
		desktop.LaunchAtLogin(cx, launch.Set)
		changeLogin = func(value bool) {
			desktop.SetLaunchAtLogin(cx, value)
			desktop.LaunchAtLogin(cx, launch.Set)
		}
	}
	return core.View{Class: "gap-5", Children: []core.Node{
		desktopSettingsHeading(locales.DesktopSettingsGeneral(cx), locales.DesktopSettingsGeneralHint(cx)),
		desktopFormGroup(locales.DesktopSettingsBasics(cx),
			desktopFormRowHeight(language, "h-12", desktopFormSelect(cx, "desktop-settings-language", language, []ui.Option[string]{
				{Label: locales.SettingsFollowSystem(cx)}, {Label: "简体中文", Value: "zh-Hans"}, {Label: "English", Value: "en"},
			}, func() string { return prefs().Language }, func(value string) {
				err := a.saveDesktopPreferences(func(p *desktopPreferences) { p.Language = value })
				failure.Set(err)
				if err == nil {
					a.scope.SetLocale(value)
				}
			})),
			a.desktopAppearanceRow(cx, failure),
			core.Dynamic(func(cx *core.Scope) core.Node {
				choices := []ui.Option[string]{{Label: locales.AllSources(cx)}}
				for _, source := range a.sources.Get() {
					choices = append(choices, ui.Option[string]{Label: source.Name, Value: source.ID})
				}
				return desktopFormDetail(theSource, locales.DesktopSettingsDefaultSourceHint(cx), desktopFormSelect(cx, "desktop-settings-default-source", theSource, choices,
					func() string {
						_ = a.desktopPrefs.Get()
						return a.desktopDefaultSource()
					}, func(value string) {
						err := a.saveDesktopPreferences(func(p *desktopPreferences) { p.DefaultSource = value })
						failure.Set(err)
						if err == nil && a.tab.GetUntracked() == 0 {
							a.sourceFilter.Set(value)
						}
					}))
			}),
		),
		desktopFormGroup(locales.DesktopSettingsStartup(cx),
			desktopFormRowHeight(start, "h-12", desktopFormSegments(cx, "desktop-startup", start, []ui.Option[bool]{
				{Label: locales.Home(cx), TestID: "home"}, {Label: locales.DesktopSettingsLastPage(cx), Value: true, TestID: "last"},
			}, func() bool { return prefs().StartLast }, func(value bool) {
				failure.Set(a.saveDesktopPreferences(func(p *desktopPreferences) { p.StartLast = value }))
			})),
			desktopFormRowHeight(login, "h-12", desktopFormToggle(cx, "desktop-login-start", login, launch.Get, changeLogin)),
			desktopFormDetail(locales.DesktopSettingsTray(cx), locales.DesktopSettingsTrayHint(cx), desktopFormToggle(cx, "desktop-tray", locales.DesktopSettingsTray(cx), nil, nil)),
		),
		a.desktopSettingsUpdate(cx),
		settingsFailure(cx, failure),
	}}
}
