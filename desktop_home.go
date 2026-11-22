package main

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) desktopHome(cx *core.Scope, results *core.Resource[catalogResult], limit *core.Signal[int], category *core.Signal[libraryCategory]) core.Node {
	notices := core.NewSignal(cx, false)
	openSearch := func() { a.tab.Set(2) }
	openLibrary := func(filter string, next libraryCategory) {
		category.Set(next)
		a.sourceFilter.Set(filter)
		a.libraryView.Set(screenLibrary)
		a.tab.Set(1)
	}
	toolbar := core.View{Class: "h-16 shrink-0 flex-row items-center gap-3 px-8 pt-2", TestID: "desktop-home-toolbar", Children: []core.Node{
		a.homeSourceSelect(cx, results, func(id string) { limit.Set(36); a.sourceFilter.Set(id) }, true),
		core.View{Class: "flex-1"},
		base.Button{Label: locales.Search(cx), TestID: "desktop-home-search",
			Class:   "h-9 w-72 shrink flex-row items-center gap-2 rounded-full bg-white px-4 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder,
			OnPress: func(core.PressEvent) { openSearch() }, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, Children: []core.Node{
				core.Icon{SVG: lucide.Search(), Class: "size-4 " + setupMuted},
				core.Text{Content: locales.SearchHint(cx), Class: "flex-1 text-sm " + setupMuted, MaxLines: 1},
				core.Text{Content: cx.KeyLabel("mod+k"), Class: "rounded border px-1.5 text-[10px] " + setupBorder + " " + setupMuted},
			}},
		desktopToolbarAction(cx, "desktop-home-refresh", locales.RefreshLibraries(cx), lucide.RefreshCw(), func() {
			for _, entry := range a.sources.GetUntracked() {
				a.checkSource(entry, true)
			}
			a.scheduleProgressReplay(0)
			results.Refetch()
		}),
		desktopToolbarAction(cx, "desktop-home-notices", locales.DesktopNotices(cx), lucide.Bell(), func() { notices.Set(true) }),
		a.desktopProfile(cx),
	}}
	body := core.Dynamic(func(cx *core.Scope) core.Node {
		if len(a.sources.Get()) == 0 {
			return core.View{Class: "mx-8 items-center gap-4 rounded-3xl bg-white p-8 dark:bg-[#1a1b25] " + setupBorder, Children: []core.Node{
				notice(locales.NoSources(cx)),
				ui.Button{Text: locales.AddSource(cx), Icon: lucide.Plus, TestID: "empty-add-source",
					OnPress: func() { a.push(route{screen: screenSources}) }},
			}}
		}
		result, ready := results.Get()
		if !ready && results.Loading() {
			return core.View{Class: "px-8", Children: []core.Node{hint(locales.Loading(cx))}}
		}
		nodes := make([]core.Node, 0, 7)
		if result.failure != nil {
			nodes = append(nodes, core.View{Class: "mx-8 gap-2", Children: []core.Node{
				notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: results.Refetch},
			}})
		}
		recent := slices.Clone(result.items)
		slices.SortStableFunc(recent, func(a, b catalogItem) int { return b.item.AddedAt.Compare(a.item.AddedAt) })
		if featured := recommendHomeTitles(recent, 4); len(featured) > 0 {
			nodes = append(nodes, core.Func(func(cx *core.Scope) core.Node { return a.desktopFeatured(cx, featured) }))
		}
		resume := a.homeResume(result, false)
		build := func(items []catalogItem, card func(*core.Scope, catalogItem) core.Node) []core.Node {
			cards := make([]core.Node, 0, len(items))
			for _, entry := range items {
				cards = append(cards, card(cx, entry))
			}
			return cards
		}
		var films, shows []catalogItem
		for _, entry := range recent {
			if entry.item.Kind == media.Series {
				shows = append(shows, entry)
			} else {
				films = append(films, entry)
			}
		}
		if len(resume) > 0 {
			nodes = append(nodes, desktopHomeShelf(cx, "desktop-resume", locales.ContinueWatching(cx), nil,
				build(resume, a.homeResumeCard), func() { a.openPersonalLibrary(screenHistory) }))
		}
		if len(films) > 0 {
			nodes = append(nodes, desktopHomeShelf(cx, "desktop-recent", locales.Recent(cx), nil,
				build(films, a.homePosterCard), func() { openLibrary(a.sourceFilter.GetUntracked(), "") }))
		}
		if len(shows) > 0 {
			nodes = append(nodes, desktopHomeShelf(cx, "desktop-series", locales.HomeSeriesUpdates(cx), nil,
				build(shows, a.desktopSeriesCard), func() { openLibrary(a.sourceFilter.GetUntracked(), librarySeries) }))
		}
		unmerged := recent
		if len(result.resumeCatalog) > 0 {
			unmerged = result.resumeCatalog
		}
		for _, source := range a.sources.Get() {
			var entries []catalogItem
			for _, entry := range unmerged {
				if source.ID == entry.source.ID {
					entries = append(entries, entry)
				}
			}
			if len(entries) > 0 {
				slices.SortStableFunc(entries, func(a, b catalogItem) int { return b.item.AddedAt.Compare(a.item.AddedAt) })
				name := providerName(cx, source.Provider)
				if source.Provider == media.Flymoo {
					name = "fnOS"
				}
				_, style := tvProviderStyle(source.Provider)
				badge := core.Text{Content: name, Class: "rounded px-1.5 py-0.5 text-[11px] font-medium " + style}
				nodes = append(nodes, desktopHomeShelf(cx, "desktop-source-"+source.ID, locales.DesktopFromSource(cx, source.Name), badge,
					build(entries, a.homePosterCard), func() { openLibrary(source.ID, "") }))
			}
		}
		if len(nodes) == 0 {
			nodes = append(nodes, core.View{Class: "px-8", Children: []core.Node{notice(locales.EmptyLibrary(cx))}})
		}
		return core.View{Class: "gap-9 pt-9 pb-10", Children: nodes}
	})
	return core.View{Class: "flex-1", TestID: "desktop-home", Children: []core.Node{
		toolbar,
		core.Scroll{Class: "flex-1", TestID: "desktop-home-scroll", Children: []core.Node{body}},
		ui.Dialog{Title: locales.DesktopNotices(cx), Open: notices.Get, OnOpenChange: func(open bool, _ base.Reason) { notices.Set(open) },
			Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
				result, _ := results.Get()
				var rows []core.Node
				for _, entry := range a.sources.Get() {
					if result.sources[entry.ID].failed {
						rows = append(rows, hint(entry.Name+" · "+locales.HomeUnavailable(cx)))
					}
				}
				if len(rows) == 0 {
					return hint(locales.DesktopNoNotices(cx))
				}
				return core.View{Class: "gap-2", Children: rows}
			})}},
	}}
}

func desktopToolbarAction(cx *core.Scope, id, label string, icon *core.SVG, action func()) core.Node {
	return base.Button{Label: label, TestID: id, Class: "size-9 shrink-0 items-center justify-center rounded-full bg-white shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { action() },
		Children: []core.Node{core.Icon{SVG: icon, Class: "size-4 " + setupTitle}}}
}

func (a *application) desktopProfile(cx *core.Scope) core.Node {
	return base.Button{Label: locales.Settings(cx), TestID: "desktop-home-profile",
		Class: "size-9 shrink-0 items-center justify-center rounded-full bg-linear-to-br from-violet-500 to-fuchsia-500",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { a.tab.Set(3) },
		Children: []core.Node{core.Dynamic(func(*core.Scope) core.Node {
			for _, src := range a.sources.Get() {
				if letter, _ := utf8.DecodeRuneInString(strings.TrimSpace(src.Username)); letter != utf8.RuneError {
					return core.Text{Content: string(unicode.ToUpper(letter)), Class: "text-sm font-semibold text-white"}
				}
			}
			return core.Icon{SVG: lucide.UserRound(), Class: "size-4 text-white"}
		})}}
}

func desktopHomeShelf(cx *core.Scope, id, title string, badge core.Node, cards []core.Node, more func()) core.Node {
	header := homeSectionTitle(cx, title, locales.HomeSeeAll(cx), more, id+"-all", "px-8")
	if badge != nil {
		header.Children[0] = core.View{Class: "flex-row items-center gap-2", Children: []core.Node{header.Children[0], badge}}
	}
	return core.View{Class: "gap-4", TestID: id, Children: []core.Node{
		header,
		homeRail(id+"-rail", "gap-4", "px-8 pb-1", cards),
	}}
}

func (a *application) desktopFeatured(cx *core.Scope, entries []catalogItem) core.Node {
	index := core.NewSignal(cx, 0)
	cx.Effect(func() {
		cursor := index.Get()
		if len(entries) > 1 && !cx.Env().ReduceMotion() {
			cx.AfterFunc(8*time.Second, func() { index.Set((cursor + 1) % len(entries)) })
		}
	})
	dots := make([]core.Node, 0, len(entries))
	for i, entry := range entries {
		dots = append(dots, base.Button{Label: entry.item.Title, TestID: "home-hero-dot-" + strconv.Itoa(i),
			Class:   "size-1.5 rounded-full bg-white/40 selected:w-6 selected:bg-white",
			HitSlop: core.All(6), Selected: func() bool { return index.Get() == i }, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			OnPress: func(core.PressEvent) { index.Set(i) }})
	}
	return core.View{Class: "relative mx-8 h-[380px] overflow-hidden rounded-3xl shadow-2xl shadow-black/25", TestID: "home-hero",
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node { return a.desktopFeaturedTitle(cx, entries[index.Get()]) }),
			core.View{Class: "absolute right-8 bottom-8 flex-row gap-1.5", Children: dots},
		}}
}

func (a *application) desktopFeaturedTitle(cx *core.Scope, entry catalogItem) core.Node {
	item := entry.item
	labels := []string{itemMetadata(cx, item)}
	if len(item.Genres) > 0 {
		labels = append(labels, strings.Join(item.Genres[:min(2, len(item.Genres))], " / "))
	}
	if minutes := int(item.Duration / time.Minute); minutes > 0 {
		labels = append(labels, locales.HomeDuration(cx, strconv.Itoa(minutes)))
	}
	action := func(id, label, color string, icon *core.SVG, onPress func()) core.Node {
		return base.Button{Label: label, TestID: id, Class: "h-11 flex-row items-center gap-2 rounded-full px-6 " + color,
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { onPress() },
			Children: []core.Node{core.Icon{SVG: icon, Class: "size-4 text-white"}, core.Text{Content: label, Class: "font-semibold text-white"}}}
	}
	buttons := []core.Node{action("home-hero-details", locales.Details(cx), "border border-white/20 bg-white/15", lucide.Info(), func() { a.openCatalogEntry(entry) })}
	if playable(item) {
		buttons = append([]core.Node{
			action("home-hero-play", locales.Play(cx), "bg-violet-500 shadow-lg shadow-violet-500/40", lucide.Play(), func() { a.playCatalogEntry(entry) }),
		}, buttons...)
	}
	buttons = append(buttons, a.tvHeroFavorite(cx, tvLayout(.55), entry))
	meta := []core.Node{core.Text{Content: strings.Join(labels, "    "), Class: "text-sm text-zinc-300", MaxLines: 1}}
	if quality := tvQuality(entry); quality != "" {
		meta = append(meta, core.Text{Content: quality, Class: "rounded border border-white/30 px-1.5 text-[11px] font-semibold text-white"})
	}
	return core.View{Class: "absolute inset-0", Children: []core.Node{
		homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0"),
		core.View{Class: "absolute inset-0 bg-linear-to-r from-black/90 via-black/50 to-transparent"},
		core.View{Class: "absolute bottom-0 left-0 w-[600px] max-w-full gap-4 p-10", Children: []core.Node{
			core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
				core.Text{Content: locales.Recent(cx), Class: "rounded-full bg-violet-500/20 px-2.5 py-1 text-xs font-medium text-violet-300"},
				core.Text{Content: entry.source.Name, TestID: "home-hero-source", Class: "rounded-full bg-emerald-500/15 px-2.5 py-1 text-xs text-emerald-400"},
			}},
			core.Text{Content: item.Title, Class: "text-5xl font-bold text-white", MaxLines: 2, TestID: "home-hero-title"},
			core.View{Class: "flex-row items-center gap-3", Children: meta},
			core.Text{Content: item.Overview, Class: "text-sm leading-relaxed text-zinc-300", MaxLines: 2},
			core.View{Class: "flex-row gap-3 pt-1", Children: buttons},
		}},
	}}
}

func (a *application) desktopSeriesCard(cx *core.Scope, entry catalogItem) core.Node {
	item := entry.item
	art := []core.Node{
		homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0"),
		core.View{Class: "absolute inset-x-0 bottom-0 h-16 bg-linear-to-t from-black/60 to-transparent"},
	}
	if homeIsFresh(cx, item) {
		art = append(art, core.View{Class: "absolute bottom-3 left-2.5 flex-row items-center gap-1 rounded-full bg-black/55 px-2 py-1",
			Children: []core.Node{core.Icon{SVG: lucide.Play(), Class: "size-3 text-white"},
				core.Text{Content: locales.HomeNewRelease(cx), Class: "text-[10px] font-medium text-white"}}})
	}
	detail := itemMetadata(cx, item)
	if item.EpisodeCount > 0 {
		detail = locales.LibraryEpisodeCount(cx, strconv.Itoa(item.EpisodeCount))
	}
	return base.Button{Label: item.Title, Hint: entry.source.Name, TestID: a.catalogEntryID("title-", entry),
		Class: "w-72 shrink-0 gap-2 rounded-2xl", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress: func(core.PressEvent) { a.openCatalogEntry(entry) }, Children: []core.Node{
			core.View{Class: "relative h-40 overflow-hidden rounded-xl shadow-lg shadow-black/15", Children: art},
			core.Text{Content: item.Title, Class: "text-sm font-medium " + setupTitle, MaxLines: 1},
			core.Text{Content: detail, Class: "text-xs " + setupMuted, MaxLines: 1},
		}}
}
