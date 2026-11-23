package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"waygu-video/locales"
)

type tvSettingsSection int

const (
	tvSources tvSettingsSection = iota
	tvPlayback
	tvSubtitles
	tvDanmaku
	tvAppearance
	tvLibraries
	tvAccounts
	tvStorage
	tvParental
	tvAbout
)

const (
	tvSettingsTitle     = "text-zinc-900 dark:text-zinc-50"
	tvSettingsMuted     = "text-zinc-500 dark:text-zinc-400"
	tvSettingsCard      = "border border-zinc-900/8 bg-white shadow-sm shadow-black/5 dark:border-white/8 dark:bg-[#1a1b25]"
	tvSettingsFocusRow  = "group selected:bg-[#18181b] dark:selected:bg-zinc-50"
	tvSettingsFocusText = "group-selected:text-[#f4f4f7] dark:group-selected:text-[#0f1017]"
)

func (a *application) tvSettingsPage(cx *core.Scope, initial tvSettingsSection, current route, back func()) core.Node {
	l := tvLayoutFor(cx)
	category := core.NewSignal(cx, initial)
	inOptions := core.NewSignal(cx, false)
	homeLayout := core.NewSignal(cx, false)
	var focus func(string)
	focusOption := func(id string) {
		cx.Post(func() {
			if focus != nil {
				focus(id)
			}
		})
	}
	openHomeLayout := func() {
		homeLayout.Set(true)
		focusOption("")
	}
	categories := []struct {
		label string
		id    string
		icon  *core.SVG
		value func() string
	}{
		{locales.MediaSources(cx), "sources", lucide.Server(), func() string { return strconv.Itoa(len(a.sources.Get())) }},
		{locales.PlaybackSettings(cx), "playback", lucide.CirclePlay(), nil},
		{locales.Subtitles(cx), "subtitles", lucide.Captions(), nil},
		{locales.Danmaku(cx), "danmaku", lucide.MessageSquare(), nil},
		{locales.SettingsAppearance(cx), "appearance", lucide.Palette(), nil},
		{locales.Library(cx), "libraries", lucide.Library(), nil},
		{locales.TvSettingsAccounts(cx), "accounts", lucide.RefreshCw(), nil},
		{locales.TvSettingsStorage(cx), "storage", lucide.HardDrive(), nil},
		{locales.TvSettingsParental(cx), "parental", lucide.Shield(), nil},
		{locales.TvSettingsAbout(cx), "about", lucide.Info(), func() string { return "v" + core.App().Version }},
	}
	refs := make([]core.Ref, len(categories))
	cx.Post(refs[initial].Focus)
	focusOptions := func() { focusOption("") }
	focusCategory := func() { cx.Post(refs[category.GetUntracked()].Focus) }
	exit := func() {
		if homeLayout.GetUntracked() {
			homeLayout.Set(false)
			focusOption("tv-open-home-layout")
			return
		}
		back()
	}
	cx.OnBackPress(func() bool { exit(); return true })
	cx.OnKey("escape", exit)
	sidebar := make([]core.Node, 0, len(categories))
	for index, section := range categories {
		item := tvSettingsSection(index)
		selected := func() bool { return category.Get() == item }
		entry := core.Func(func(cx *core.Scope) core.Node {
			var focused = core.NewSignal(cx, false)
			move := func(delta int) {
				if next := index + delta; next >= 0 && next < len(refs) {
					refs[next].Focus()
				}
			}
			cx.Effect(func() {
				if !focused.Get() {
					return
				}
				keys := core.KeyOptions{When: core.KeyNotEditing, Repeat: true}
				cx.OnKeyWith("up", keys, func() { move(-1) })
				cx.OnKeyWith("down", keys, func() { move(1) })
				cx.OnKeyWith("right", keys, focusOptions)
			})
			style := core.Style{Height: l.length(56), Gap: l.points(20), Padding: l.padding(24, 0),
				CornerRadius: l.points(16), FocusVisible: l.focusRing()}
			label := l.text(section.label, 24, "flex-1 min-w-0 text-zinc-900/80 dark:text-zinc-50/80 group-selected:font-semibold group-selected:text-violet-500")
			value := l.text("", 20, "text-zinc-500/70 dark:text-zinc-400/70")
			value.ContentFunc = section.value
			return core.View{Class: "group relative flex-row items-center selected:bg-violet-500/12", Style: style,
				Role: core.RoleButton, Selected: selected, Ref: &refs[index], Label: section.label, TestID: "tv-settings-section-" + section.id,
				OnFocus: func() {
					focused.Set(true)
					category.Set(item)
					homeLayout.Set(false)
					inOptions.Set(false)
				},
				OnBlur:  func() { focused.Set(false) },
				OnPress: func(core.PressEvent) { category.Set(item); focusOptions() },
				Children: []core.Node{
					core.Show{When: selected, Then: func(*core.Scope) core.Node {
						return core.View{Class: "rounded-full bg-violet-500", Style: core.Style{Width: l.length(6), Height: l.length(28)}}
					}},
					l.icon(section.icon, 28, tvSettingsMuted+" group-selected:text-violet-500"), label,
					core.Show{When: func() bool { return selected() && inOptions.Get() }, Then: func(*core.Scope) core.Node {
						return l.icon(lucide.ChevronRight(), 24, "text-violet-500")
					}},
					core.Show{When: func() bool { return section.value != nil && !selected() }, Then: func(*core.Scope) core.Node { return value }},
				},
			}
		})
		sidebar = append(sidebar, entry)
	}
	backButton := core.View{Class: "flex-row items-center self-start", Style: core.Style{Gap: l.points(16), FocusVisible: l.focusRing()},
		Role: core.RoleButton, Label: locales.Back(cx), TestID: "back", OnPress: func(core.PressEvent) { exit() },
		Children: []core.Node{core.View{Class: tvSettingsCard + " rounded-full items-center justify-center",
			Style:    core.Style{Width: l.length(48), Height: l.length(48)},
			Children: []core.Node{l.icon(lucide.ChevronLeft(), 28, tvSettingsTitle)}},
			l.text(locales.Settings(cx), 48, "text-5xl font-bold tracking-tight "+tvSettingsTitle),
		},
	}
	both := l.points(64)
	left := core.View{Class: "shrink-0 border-r border-zinc-900/8 bg-[#e9e9f0]/40 dark:border-white/8 dark:bg-[#1a1b25]/40",
		TestID: "tv-settings-sidebar", Style: core.Style{Width: l.length(640), Gap: l.points(32),
			Padding: core.Edges{Top: both, Left: both, Right: both, Bottom: l.points(48)}},
		Children: []core.Node{backButton, core.View{Style: core.Style{Gap: l.points(6)}, Children: sidebar},
			core.View{Class: "flex-1"}, a.tvPairCard(cx, l)},
	}
	right := core.Dynamic(func(cx *core.Scope) core.Node {
		section := category.Get()
		layout := section == tvAppearance && homeLayout.Get()
		var sources []source
		if section == tvSources || section == tvLibraries {
			sources = a.sources.Get()
		}
		return core.Func(func(cx *core.Scope) core.Node {
			var dialogs []core.Node
			title := categories[section].label
			var rows func() []tvOption
			var description string
			if layout {
				title = locales.TvHomeLayout(cx)
				description = locales.TvHomeLayoutHint(cx)
				failure := core.NewSignal(cx, false)
				rows = a.tvHomeLayoutOptions(cx, func(err error) { failure.Set(err != nil) })
				dialogs = append(dialogs, core.Show{When: failure.Get, Then: func(cx *core.Scope) core.Node {
					return core.View{Class: "absolute inset-x-0 bottom-0 items-center",
						PointerEvents: core.PointerEventsNone,
						Children:      []core.Node{notice(locales.TvHomeLayoutSaveError(cx))}}
				}})
			} else {
				options := a.tvSettingsOptions(cx, l, section, current, sources, &dialogs, openHomeLayout)
				rows = func() []tvOption { return options }
			}
			if section == tvDanmaku {
				description = locales.TvDanmakuHint(cx)
			}
			header := []core.Node{l.text(title, 48, "text-5xl font-bold tracking-tight "+tvSettingsTitle)}
			if description != "" {
				header = append(header, l.text(description, 20, tvSettingsMuted))
			}
			clock := tvClock(cx, l)
			clock.Class = "text-3xl font-medium " + tvSettingsTitle
			content := core.Scroll{Class: "flex-1", TestID: "tv-settings-options", Children: []core.Node{
				core.View{Style: core.Style{Gap: l.points(24), Padding: core.Edges{
					Top: l.points(64), Left: l.points(80), Right: l.points(80), Bottom: l.points(48)}},
					Children: []core.Node{core.View{Class: "flex-row items-end justify-between", Children: []core.Node{
						core.View{Style: core.Style{Gap: l.points(8)}, Children: header}, clock,
					}}, tvSettingsRows(l, rows, &focus, func() { inOptions.Set(true) }, focusCategory)},
				},
			}}
			return core.View{Class: "flex-1", Children: append([]core.Node{content}, dialogs...)}
		})
	})
	return core.View{Class: "relative flex-1 flex-row bg-waygu-page", TestID: "tv-settings",
		Children: []core.Node{homeAmbient(false, true), homeAmbient(true, true), left, right}}
}
