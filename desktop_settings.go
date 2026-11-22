package main

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

type desktopSettingsPage string

const (
	desktopSettingsOverview desktopSettingsPage = ""
	desktopSettingsGeneral  desktopSettingsPage = "general"
	desktopSettingsSources  desktopSettingsPage = "sources"
	desktopSettingsPlayback desktopSettingsPage = "playback"
	desktopSettingsDanmaku  desktopSettingsPage = "danmaku"
)

type desktopSettingsLink struct {
	page  desktopSettingsPage
	label string
	icon  func() *core.SVG
	color string
}

func desktopSettingsLinks(cx *core.Scope) []desktopSettingsLink {
	return []desktopSettingsLink{
		{desktopSettingsGeneral, locales.DesktopSettingsGeneral(cx), lucide.Settings, "bg-slate-500"},
		{desktopSettingsSources, locales.MediaSources(cx), lucide.Server, "bg-emerald-500"},
		{desktopSettingsPlayback, locales.PlaybackSettings(cx), lucide.Play, "bg-violet-500"},
		{"subtitles", locales.Subtitles(cx), lucide.Captions, "bg-sky-500"},
		{desktopSettingsDanmaku, locales.Danmaku(cx), lucide.MessageSquareText, "bg-fuchsia-500"},
		{"appearance", locales.SettingsAppearance(cx), lucide.Palette, "bg-pink-500"},
		{"library", locales.DesktopSettingsMetadata(cx), lucide.Library, "bg-orange-500"},
		{"sync", locales.DesktopSettingsSync(cx), lucide.RefreshCw, "bg-blue-500"},
		{"storage", locales.StorageDownloads(cx), lucide.HardDrive, "bg-teal-500"},
		{"parental", locales.DesktopSettingsParental(cx), lucide.Shield, "bg-rose-500"},
		{"keys", locales.DesktopSettingsShortcuts(cx), lucide.Keyboard, "bg-slate-500"},
		{"about", locales.TvSettingsAbout(cx), lucide.Info, "bg-slate-600"},
	}
}

func (a *application) openDesktopSettings(cx *core.Scope, page desktopSettingsPage) {
	if a.showDesktopSettingsWindow(cx, page) {
		return
	}
	a.desktopSettingsTab.Set(page)
	a.tab.Set(3)
}

func (a *application) showDesktopSettingsWindow(cx *core.Scope, page desktopSettingsPage) bool {
	if !desktop.IsAvailable(cx) {
		return false
	}
	a.desktopSettingsTab.Set(page)
	a.desktopSettingsWin = desktop.OpenWindow(a.scope, desktop.WindowOptions{
		Title: locales.Settings(cx), Key: "waygu-settings", Width: 1100, Height: 760,
		Constraints: core.WindowConstraints{MinWidth: 820, MinHeight: 560},
	}, func(cx *core.Scope) core.Node {
		cx.SetTheme(wayguTheme())
		observeAppearance(cx, a.appearance.Get)
		cx.SetTitleBar(core.TitleBarHidden, 48)
		return a.desktopSettings(cx, cx.CurrentWindow().Close)
	})
	return !a.desktopSettingsWin.Closed()
}

func (a *application) openDesktopSettingsFlow(next route) {
	if !a.desktopSettingsWin.Closed() {
		a.desktopSettingsWin.Hide()
		a.scope.CurrentWindow().Focus()
	}
	next.settingsFlow = true
	a.push(next)
}

func (a *application) restoreDesktopSettings() {
	if !a.desktopSettingsWin.Closed() {
		a.desktopSettingsWin.Show()
		a.desktopSettingsWin.Focus()
	}
}

func (a *application) desktopSettings(cx *core.Scope, close func()) core.Node {
	query := core.NewSignal(cx, "")
	links := desktopSettingsLinks(cx)
	pick := func(page desktopSettingsPage) {
		query.Set("")
		a.desktopSettingsTab.Set(page)
	}
	matches := func(link desktopSettingsLink) bool {
		value := strings.TrimSpace(strings.ToLower(query.Get()))
		return value == "" || strings.Contains(strings.ToLower(link.label), value)
	}
	search := core.View{Class: "mb-2.5 h-8 flex-row items-center gap-2 rounded-lg px-2.5 shadow-sm shadow-black/5 " + desktopLibrarySurface,
		Children: []core.Node{
			core.Icon{SVG: lucide.Search(), Class: "size-3.5 shrink-0 " + setupMuted},
			core.Input{Label: locales.DesktopSearchSettings(cx), Placeholder: locales.DesktopSearchSettings(cx), TestID: "desktop-settings-search",
				Class: "h-full min-w-0 flex-1 border-0 bg-transparent p-0 text-[13px] " + setupTitle,
				Value: query.Get, OnChange: query.Set, OnSubmit: func(string) {
					for _, link := range links {
						if matches(link) {
							pick(link.page)
							break
						}
					}
				}},
		}}
	nav := core.Dynamic(func(cx *core.Scope) core.Node {
		var rows []core.Node
		for _, link := range links {
			if matches(link) {
				rows = append(rows, base.Button{Label: link.label, TestID: "desktop-settings-tab-" + string(link.page),
					Selected: func() bool { return a.desktopSettingsTab.Get() == link.page },
					Class:    "group h-9 flex-row items-center gap-2.5 rounded-lg px-2.5 selected:bg-violet-500/12",
					Style:    core.Style{FocusVisible: ui.FocusRing(cx, -2)}, OnPress: func(core.PressEvent) { pick(link.page) },
					Children: []core.Node{
						desktopSettingsBadge(link, "size-6 rounded-md", "size-3.5"),
						core.Text{Content: link.label, Class: "text-[13px] text-zinc-900/80 group-selected:font-medium group-selected:text-zinc-900 dark:text-zinc-50/80 dark:group-selected:text-zinc-50", MaxLines: 1},
					}})
			}
		}
		if len(rows) == 0 {
			return core.Text{Content: locales.DesktopSettingsNoResults(cx), Class: "px-2.5 pt-2 text-xs " + setupMuted}
		}
		return core.View{Class: "gap-0.5", Children: rows}
	})
	page := core.Dynamic(func(cx *core.Scope) core.Node {
		var content core.Node
		switch selected := a.desktopSettingsTab.Get(); selected {
		case desktopSettingsOverview:
			content = a.desktopSettingsOverview(cx, links, pick)
		case desktopSettingsGeneral:
			content = a.desktopSettingsGeneral(cx)
		case desktopSettingsSources:
			content = a.desktopSettingsSources(cx)
		case desktopSettingsPlayback:
			content = a.desktopSettingsPlayback(cx)
		case desktopSettingsDanmaku:
			content = a.desktopSettingsDanmaku(cx)
		default:
			for _, link := range links {
				if selected == link.page {
					content = a.desktopSettingsSupplement(cx, link)
					break
				}
			}
		}
		padding := "p-16"
		if a.desktopSettingsTab.Get() == desktopSettingsOverview {
			padding = "p-8"
		}
		return core.Scroll{Class: "min-w-0 flex-1", TestID: "desktop-settings-page-" + string(a.desktopSettingsTab.Get()),
			Children: []core.Node{core.View{Class: padding, Children: []core.Node{content}}}}
	})
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyNotEditing}, close)
	return core.View{Class: "relative flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", TestID: "desktop-settings", Children: []core.Node{
		homeAmbient(false, true), homeAmbient(true, true),
		desktopSettingsTitleBar(cx, close, func() { pick(desktopSettingsOverview) }),
		core.View{Class: "min-h-0 flex-1 flex-row", Children: []core.Node{
			core.Scroll{Class: "w-56 shrink-0 border-r border-zinc-900/8 bg-[#e9e9f0]/40 dark:border-white/8 dark:bg-[#1a1b25]/40",
				TestID: "desktop-settings-sidebar", Children: []core.Node{core.View{Class: "p-3", Children: []core.Node{search, nav}}}},
			page,
		}},
	}}
}

func desktopSettingsTitleBar(cx *core.Scope, close, overview func()) core.Node {
	var controls []core.Node
	for _, item := range []struct {
		id, label, style string
		press            func()
	}{
		{"close", locales.DesktopSettingsClose(cx), "bg-[#ff5f57]", close},
		{"minimize", locales.DesktopSettingsMinimize(cx), "bg-[#febc2e]", cx.MinimizeWindow},
		{"maximize", locales.DesktopSettingsMaximize(cx), "bg-[#28c840]", cx.ToggleMaximize},
	} {
		controls = append(controls, base.Button{Label: item.label, TestID: "desktop-settings-" + item.id, Class: "size-3 rounded-full " + item.style,
			HitSlop: core.Edges{Top: 4, Right: 4, Bottom: 4, Left: 4}, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { item.press() }})
	}
	return core.View{Class: "h-12 shrink-0 flex-row items-center border-b border-zinc-900/8 bg-[#e9e9f0]/60 px-4 dark:border-white/8 dark:bg-[#1a1b25]/60",
		TestID: "desktop-settings-titlebar", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn},
		Children: []core.Node{
			core.View{Class: "w-20 flex-row items-center gap-2", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff}, Children: controls},
			core.View{Class: "flex-1 items-center", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff}, Children: []core.Node{
				base.Button{Label: locales.Settings(cx), TestID: "desktop-settings-overview", Class: "rounded-sm",
					Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { overview() },
					Children: []core.Node{core.Text{Content: locales.Settings(cx), Class: "text-center text-sm font-semibold " + setupTitle}}},
			}},
			core.View{Class: "w-20 flex-row justify-end", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff}},
		}}
}

func desktopSettingsBadge(link desktopSettingsLink, shape, size string) core.Node {
	return core.View{Class: "shrink-0 items-center justify-center " + shape + " " + link.color,
		Children: []core.Node{core.Icon{SVG: link.icon(), Class: size + " text-white"}}}
}

func (a *application) desktopSettingsOverview(cx *core.Scope, links []desktopSettingsLink, pick func(desktopSettingsPage)) core.Node {
	a.watchSourceConnections(cx)
	account := func() string {
		for _, entry := range a.sources.Get() {
			if name := strings.TrimSpace(entry.Username); name != "" {
				return name
			}
		}
		return locales.AppName(cx)
	}
	sourceSummary := func() string {
		entries, checks := a.sources.Get(), a.sourceChecks.Get()
		online := 0
		for _, entry := range entries {
			if check := checks[entry.ID]; check.connection == entry.connection() && !check.at.IsZero() && !check.checking && check.err == nil {
				online++
			}
		}
		return locales.DesktopSettingsSourceCounts(cx, strconv.Itoa(len(entries)), strconv.Itoa(online))
	}
	summaries := map[desktopSettingsPage]func() string{
		desktopSettingsSources: sourceSummary,
		desktopSettingsPlayback: func() string {
			if a.mpvPreferred.Get() {
				return locales.DesktopBuiltinMPV(cx)
			}
			return locales.DesktopPlayerNativeEngine(cx)
		},
		desktopSettingsDanmaku: func() string {
			if a.danmakuPrefs.Get().Enabled {
				return locales.ShowDanmaku(cx)
			}
			return locales.HideDanmaku(cx)
		},
	}
	cards := []core.Node{}
	for _, link := range links {
		if summary, ok := summaries[link.page]; ok {
			cards = append(cards, base.Button{Label: link.label, TestID: "desktop-settings-quick-" + string(link.page),
				Class: "min-w-0 flex-1 gap-3 rounded-2xl p-5 shadow-sm shadow-black/5 " + desktopLibrarySurface,
				Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { pick(link.page) },
				Children: []core.Node{
					core.View{Class: "items-start", Children: []core.Node{desktopSettingsBadge(link, "size-9 rounded-lg", "size-5")}},
					core.Text{Content: link.label, Class: "font-semibold " + setupTitle},
					core.Text{ContentFunc: summary, Class: "text-xs " + setupMuted},
				}})
		}
	}
	return core.View{Class: "gap-6", Children: []core.Node{
		core.View{Class: "flex-row items-center gap-4 rounded-2xl p-5 shadow-sm shadow-black/5 " + desktopLibrarySurface,
			Children: []core.Node{
				core.View{Class: "size-14 shrink-0 items-center justify-center rounded-full bg-linear-to-br from-violet-500 to-fuchsia-500",
					Children: []core.Node{core.Text{Class: "text-xl font-semibold text-white", ContentFunc: func() string {
						char, _ := utf8.DecodeRuneInString(account())
						return string(unicode.ToUpper(char))
					}}}},
				core.View{Class: "min-w-0 flex-1 gap-1", Children: []core.Node{
					core.Text{ContentFunc: account, Class: "text-lg font-semibold " + setupTitle, MaxLines: 1},
					core.Text{ContentFunc: sourceSummary, Class: "text-sm " + setupMuted},
				}},
				core.Text{Content: "Waygu " + core.App().Version, Class: "rounded-full bg-zinc-900/5 px-3 py-1 text-xs dark:bg-zinc-50/5 " + setupMuted},
			}},
		core.Text{Content: locales.DesktopSettingsCommon(cx), Class: "text-sm font-medium " + setupMuted},
		core.View{Class: "flex-row gap-4", Children: cards},
	}}
}
