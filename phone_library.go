package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

const phoneLibraryPageSize = 36

func (a *application) compactLibrary(cx *core.Scope) core.Node {
	current := a.phoneLibrary
	change := func(edit func(*phoneLibraryState)) {
		value := current.GetUntracked()
		edit(&value)
		current.Set(value)
	}
	category := core.NewMemo(cx, func() libraryCategory {
		if selected := current.Get().category; selected != "" {
			return selected
		}
		return defaultPhoneLibrary(a.sources.Get(), a.sourceFilter.Get())
	})
	library := core.NewMemo(cx, func() string {
		selected := current.Get().library
		if selected != "" && slices.ContainsFunc(libraryChoices(cx, a.sources.Get(), a.sourceFilter.Get(), category.Get()), func(option ui.Option[string]) bool {
			return option.Value == selected
		}) {
			return selected
		}
		return ""
	})
	refresh := core.NewSignal(cx, uint64(0))
	request := core.NewMemoFunc(cx, func() *catalogRequest {
		refresh.Get()
		a.libraryChanges.Get()
		a.progress.synced.Get()
		return &catalogRequest{sources: a.sources.Get(), filter: a.sourceFilter.Get(), library: library.Get(),
			category: category.Get(), libraryFacts: true, limit: phoneLibraryPageSize, merge: a.mergeTitles.Get()}
	}, nil)
	preview, full := a.phoneLibraryPages(cx, request.Get)
	page := core.NewMemoFunc(cx, func() catalogResult {
		q := request.Get()
		initial, _ := preview.Get()
		all, ready := full.Get()
		if ready && all.query == q {
			result := all.result
			result.resume = initial.result.resume
			if !result.connected && result.failure != nil {
				result.items = initial.result.items
			}
			return result
		}
		if initial.query == q {
			return initial.result
		}
		return catalogResult{}
	}, nil)
	rows := core.NewMemoFunc(cx, func() []catalogItem {
		q := request.Get()
		return addLibraryDownloads(page.Get().items, a.libraryDownloads(*q))
	}, nil)
	facets := core.NewMemoFunc(cx, func() libraryFacets {
		return phoneLibraryFacets(rows.Get())
	}, nil)
	filterOpen := core.NewSignal(cx, false)
	limit := core.NewSignal(cx, phoneLibraryPageSize)
	cx.Effect(func() {
		request.Get()
		current.Get()
		limit.Set(phoneLibraryPageSize)
	})
	pickCategory := func(next libraryCategory) {
		change(func(state *phoneLibraryState) {
			state.category, state.library = next, ""
			state.genre, state.year, state.resolution = "", 0, 0
		})
	}
	actions := core.View{Class: "flex-row items-center gap-4", Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node {
			list := current.Get().list
			icon, label := lucide.LayoutGrid(), locales.LibraryListView(cx)
			if list {
				icon, label = lucide.List(), locales.LibraryPosterView(cx)
			}
			return compactLibraryIcon(cx, "library-layout", label, icon, setupAccent, func() {
				change(func(state *phoneLibraryState) { state.list = !state.list })
			})
		}),
		compactLibraryIcon(cx, "library-filter", locales.LibraryFilters(cx), lucide.SlidersHorizontal(), setupTitle,
			func() { filterOpen.Set(true) }),
	}}
	content := core.Dynamic(func(cx *core.Scope) core.Node {
		settings, result := current.Get(), page.Get()
		q := request.Get()
		initial, ready := preview.Get()
		loading := len(q.sources) > 0 && (!ready || initial.query != q || preview.Loading())
		indexing := result.hasMore && full.Loading()
		matches := filterPhoneLibrary(rows.Get(), settings)
		tryAgain := func() { refresh.Update(func(n uint64) uint64 { return n + 1 }) }
		if len(q.sources) == 0 {
			return compactLibraryEmpty(cx, lucide.LibraryBig(), locales.NoSources(cx),
				locales.AddSource(cx), func() { a.push(route{screen: screenSources}) })
		}
		if len(matches) == 0 {
			if loading || indexing {
				return compactLibraryEmpty(cx, lucide.LoaderCircle(), locales.Loading(cx), "", nil)
			}
			if result.failure != nil {
				return compactLibraryEmpty(cx, lucide.CircleAlert(), errorText(cx, result.failure), locales.Retry(cx), tryAgain)
			}
			icon := lucide.Clapperboard()
			switch q.category {
			case librarySeries:
				icon = lucide.Tv()
			case libraryAnime:
				icon = lucide.Sparkles()
			case libraryDocumentaries:
				icon = lucide.Globe()
			}
			return compactLibraryEmpty(cx, icon, locales.EmptyLibrary(cx), "", nil)
		}
		children := []core.Node{core.Text{Content: compactLibrarySummary(cx, q.category, matches, loading || indexing || result.hasMore || result.failure != nil),
			TestID: "library-summary", Class: "text-xs " + setupMuted, MaxLines: 2}}
		bookmarks := libraryBookmarkMap(a.progressResumeShelf(result.resume, q.sources, *q))
		visible := min(len(matches), limit.Get())
		if settings.list {
			list := make([]core.Node, 0, visible)
			for _, item := range matches[:visible] {
				list = append(list, a.compactLibraryCard(cx, item, bookmarks, settings.resolution, true))
			}
			children = append(children, core.View{TestID: "library-list",
				Class: "gap-px overflow-hidden rounded-2xl bg-zinc-200 shadow-sm dark:bg-white/8 " + setupBorder, Children: list})
		} else {
			grid := make([]core.Node, 0, (visible+2)/3)
			for chunk := range slices.Chunk(matches[:visible], 3) {
				cards := make([]core.Node, 0, 3)
				for _, item := range chunk {
					cards = append(cards, a.compactLibraryCard(cx, item, bookmarks, settings.resolution, false))
				}
				for len(cards) < 3 {
					cards = append(cards, core.View{Class: compactPosterWidth})
				}
				grid = append(grid, core.View{Class: "flex-row justify-between", Children: cards})
			}
			children = append(children, core.View{TestID: "library-grid", Class: "gap-5", Children: grid})
		}
		if visible < len(matches) {
			children = append(children, ui.Button{Text: locales.NextPage(cx), TestID: "library-next-page", Variant: ui.ButtonOutline,
				OnPress: func() { limit.Update(func(n int) int { return n + phoneLibraryPageSize }) }})
		}
		if result.failure != nil {
			children = append(children, notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: tryAgain})
		}
		return core.View{Class: "gap-5 px-5", Children: children}
	})
	return core.View{Class: "relative flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", Children: []core.Node{
		homeAmbient(false, true), homeAmbient(true, true),
		core.Scroll{Class: "flex-1", TestID: "phone-library-scroll", Children: []core.Node{
			core.View{StyleFunc: func() core.Style {
				return core.Style{
					PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Left: core.EnvSafeArea, Right: core.EnvSafeArea, Bottom: core.EnvKeyboard},
					Padding:    core.Edges{Bottom: tabBarInset(cx) + 12},
					MinHeight:  core.Pt(cx.Env().Window().Height),
				}
			}, Children: []core.Node{
				core.View{Class: "flex-row items-center justify-between px-5 pt-2", Children: []core.Node{
					core.Text{Content: locales.Library(cx), TestID: "phone-library-title", Role: core.RoleHeader,
						Class: "text-[28px] font-bold tracking-tight " + setupTitle}, actions,
				}},
				compactLibraryCategories(cx, category.Get, pickCategory),
				core.Dynamic(func(cx *core.Scope) core.Node {
					return compactLibraryFilterBar(cx, current.Get(), facets.Get(), change)
				}),
				content,
			}},
		}},
		ui.Dialog{Title: locales.LibraryFilters(cx), TestID: "library-filter-dialog", Open: filterOpen.Get,
			OnOpenChange: func(open bool, _ base.Reason) { filterOpen.Set(open) },
			Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
				sourceOptions := []ui.Option[string]{{Label: locales.AllSources(cx)}}
				for _, source := range a.sources.Get() {
					sourceOptions = append(sourceOptions, ui.Option[string]{Label: source.Name, Value: source.ID})
				}
				return core.View{Class: "gap-3", Children: []core.Node{
					ui.Select[string]{Label: locales.MediaSources(cx), TestID: "library-source", Options: sourceOptions, Value: a.sourceFilter.Get,
						OnChange: func(id string) {
							cx.Batch(func() {
								a.sourceFilter.Set(id)
								change(func(state *phoneLibraryState) { state.library = "" })
							})
						}},
					ui.Select[string]{Label: locales.Library(cx), Options: libraryChoices(cx, a.sources.Get(), a.sourceFilter.Get(), category.Get()),
						TestID: "catalog-library", Value: library.Get, OnChange: func(id string) {
							change(func(state *phoneLibraryState) { state.library = id })
						}},
					ui.Button{Text: locales.LibraryClearFilters(cx), TestID: "library-clear-filters", Variant: ui.ButtonGhost,
						OnPress: func() {
							cx.Batch(func() {
								a.sourceFilter.Set("")
								change(func(state *phoneLibraryState) {
									state.genre, state.year, state.resolution, state.library, state.unwatched = "", 0, 0, "", false
									state.sort = libraryRecent
								})
							})
						}},
					ui.Button{Text: locales.Favorites(cx), TestID: "library-view-favorites", Icon: lucide.Heart, Variant: ui.ButtonOutline,
						OnPress: func() { filterOpen.Set(false); a.openPersonalLibrary(screenFavorites) }},
					ui.Button{Text: locales.WatchHistory(cx), TestID: "library-view-history", Icon: lucide.Clock, Variant: ui.ButtonOutline,
						OnPress: func() { filterOpen.Set(false); a.openPersonalLibrary(screenHistory) }},
				}}
			})}, Footer: []core.Node{
				ui.Button{Text: locales.LibraryDone(cx), TestID: "library-filter-close", OnPress: func() { filterOpen.Set(false) }},
			}},
	}}
}

func compactLibraryIcon(cx *core.Scope, id, label string, icon *core.SVG, tint string, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Class: "min-h-9 items-center justify-center rounded-lg",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { press() },
		Children: []core.Node{core.Icon{SVG: icon, Class: "size-5 " + tint}}}
}

func compactLibraryCategories(cx *core.Scope, selected func() libraryCategory, selectCategory func(libraryCategory)) core.Node {
	tabs := make([]core.Node, 0, len(phoneLibraryCategories))
	for _, option := range libraryCategories(cx, locales.AllCategories(cx))[1:] {
		active := func() bool { return selected() == option.Value }
		tabs = append(tabs, base.Tab{TestID: "library-category-" + string(option.Value), Label: option.Label, Selected: active,
			Class: "group shrink-0 rounded-full border border-zinc-900/8 bg-white px-4 py-2 shadow-sm shadow-black/5 selected:border-transparent selected:bg-zinc-900 dark:border-white/8 dark:bg-[#1a1b25] dark:selected:bg-zinc-50",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { selectCategory(option.Value) },
			Children: []core.Node{
				core.Text{Content: option.Label, Class: "text-sm font-medium text-zinc-900/80 group-selected:text-[#f4f4f7] dark:text-zinc-50/80 dark:group-selected:text-[#0f1017]", MaxLines: 1},
			}})
	}
	return core.Scroll{Horizontal: true, TestID: "catalog-category", Children: []core.Node{
		base.Tabs{Label: locales.LibraryCategory(cx), Class: "flex-row gap-2 px-5 pt-4", Children: tabs},
	}}
}

func compactLibraryFilterBar(cx *core.Scope, state phoneLibraryState, facets libraryFacets, change func(func(*phoneLibraryState))) core.Node {
	sorts := []ui.Option[librarySort]{
		{Label: locales.Recent(cx), Value: libraryRecent}, {Label: locales.LibrarySortTitle(cx), Value: libraryTitle},
		{Label: locales.LibrarySortYear(cx), Value: libraryYear}, {Label: locales.LibrarySortRating(cx), Value: libraryRating},
	}
	genres := []ui.Option[string]{{Label: locales.LibraryAnyGenre(cx)}}
	for _, genre := range facets.genres {
		genres = append(genres, ui.Option[string]{Label: genre, Value: strings.ToLower(genre)})
	}
	years := []ui.Option[int]{{Label: locales.LibraryAnyYear(cx)}}
	for _, year := range facets.years {
		years = append(years, ui.Option[int]{Label: strconv.Itoa(year), Value: year})
	}
	resolutions := []ui.Option[int]{{Label: locales.LibraryAnyResolution(cx)}}
	for _, height := range facets.resolutions {
		resolutions = append(resolutions, ui.Option[int]{Label: libraryResolutionName(height), Value: height})
	}
	unwatched := base.Button{Label: locales.LibraryUnwatched(cx), TestID: "library-unwatched", Selected: func() bool { return state.unwatched },
		Class: libraryFilterClass(state.unwatched), Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress:  func(core.PressEvent) { change(func(next *phoneLibraryState) { next.unwatched = !next.unwatched }) },
		Children: []core.Node{core.Text{Content: locales.LibraryUnwatched(cx), Class: "text-xs " + libraryFilterTint(state.unwatched)}}}
	return core.Scroll{Horizontal: true, TestID: "library-filters", Children: []core.Node{
		core.View{Class: "flex-row items-center gap-2 px-5 pt-3 pb-4", Children: []core.Node{
			libraryFilterMenu(cx, "library-sort", locales.LibrarySort(cx), sorts[state.sort].Label, sorts, state.sort, true, lucide.ArrowDownWideNarrow(),
				func(value librarySort) { change(func(next *phoneLibraryState) { next.sort = value }) }),
			libraryFilterMenu(cx, "library-genre", locales.LibraryGenre(cx), libraryFilterValue(locales.LibraryGenre(cx), genres, state.genre), genres,
				state.genre, state.genre != "", nil, func(value string) { change(func(next *phoneLibraryState) { next.genre = value }) }),
			libraryFilterMenu(cx, "library-year", locales.LibraryYear(cx), libraryFilterValue(locales.LibraryYear(cx), years, state.year), years,
				state.year, state.year != 0, nil, func(value int) { change(func(next *phoneLibraryState) { next.year = value }) }),
			libraryFilterMenu(cx, "library-resolution", locales.LibraryResolution(cx), libraryFilterValue(locales.LibraryResolution(cx), resolutions, state.resolution), resolutions,
				state.resolution, state.resolution != 0, nil, func(value int) { change(func(next *phoneLibraryState) { next.resolution = value }) }),
			unwatched,
		}},
	}}
}

func libraryFilterValue[T comparable](defaultLabel string, options []ui.Option[T], value T) string {
	var zero T
	if value != zero {
		for _, option := range options {
			if option.Value == value {
				return option.Label
			}
		}
	}
	return defaultLabel
}

func libraryFilterMenu[T comparable](cx *core.Scope, id, label, display string, options []ui.Option[T], current T, active bool,
	icon *core.SVG, change func(T)) core.Node {
	tint := libraryFilterTint(active)
	chip := []core.Node{core.Text{Content: display, Class: "text-xs " + tint, MaxLines: 1}}
	if icon != nil {
		chip = append([]core.Node{core.Icon{SVG: icon, Class: "size-3.5 shrink-0 " + tint}}, chip...)
	} else {
		chip = append(chip, core.Icon{SVG: lucide.ChevronDown(), Class: "size-3 shrink-0 " + setupMuted})
	}
	items := make([]core.Node, 0, len(options))
	for i, option := range options {
		items = append(items, ui.MenuItem{Text: option.Label, TestID: id + "-" + strconv.Itoa(i),
			Checked: func() bool { return current == option.Value }, OnSelect: func() { change(option.Value) }})
	}
	return base.Menu{Label: label, TestID: id, Class: libraryFilterClass(active),
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, PopupClass: tabletDetailPopup, Children: items,
		Trigger: core.View{Class: "flex-row items-center gap-1", Children: chip}}
}

func libraryFilterClass(active bool) string {
	const shape = "shrink-0 flex-row items-center gap-1 rounded-lg border px-2.5 py-1.5"
	if active {
		return shape + " border-violet-500/30 bg-violet-500/12"
	}
	return shape + " border-zinc-900/8 bg-white dark:border-white/8 dark:bg-[#1a1b25]"
}

func libraryFilterTint(active bool) string {
	if active {
		return setupAccent
	}
	return "text-zinc-900/80 dark:text-zinc-50/80"
}

func compactLibrarySummary(cx *core.Scope, category libraryCategory, items []catalogItem, partial bool) string {
	amount := strconv.Itoa(len(items))
	if partial {
		return locales.LibraryLoadedCount(cx, amount)
	}
	sources := make(map[string]bool)
	unplayed := 0
	for _, entry := range items {
		sources[entry.source.ID] = true
		if entry.match != nil {
			for _, copy := range entry.match.copies {
				sources[copy.source.ID] = true
			}
		}
		if entry.item.Kind == media.Series && entry.item.UnplayedCount != nil && *entry.item.UnplayedCount > 0 {
			unplayed++
		}
	}
	if category == librarySeries {
		return locales.LibrarySeriesCount(cx, amount) + " · " + locales.LibrarySeriesUnplayed(cx, strconv.Itoa(unplayed))
	}
	if category == libraryMovies {
		return locales.LibraryMovieCount(cx, amount, strconv.Itoa(len(sources)))
	}
	return locales.LibraryTitleCount(cx, amount, strconv.Itoa(len(sources)))
}

func compactLibraryEmpty(cx *core.Scope, icon *core.SVG, label, action string, press func()) core.Node {
	children := []core.Node{
		core.Icon{SVG: icon, Class: "size-8 opacity-50 " + setupMuted},
		core.Text{Content: label, Class: "text-center text-sm " + setupMuted},
	}
	if press != nil {
		children = append(children, ui.Button{Text: action, Variant: ui.ButtonOutline, OnPress: press})
	}
	return core.View{Class: "min-h-[260px] flex-1 items-center justify-center gap-2 px-5", TestID: "library-empty", Children: children}
}

const compactPosterWidth = "w-28 max-w-[32%] min-w-0 shrink-0"

func (a *application) compactLibraryCard(cx *core.Scope, entry catalogItem, bookmarks map[copyKey]media.Item, resolution int, list bool) core.Node {
	item := entry.item
	details, progress := libraryCardDetails(cx, entry, bookmarks)
	watched := catalogWatched(entry)
	shape, coverShape := compactPosterWidth+" gap-1.5 rounded-xl", "w-full rounded-xl shadow-lg shadow-black/15"
	if list {
		shape, coverShape = "flex-row items-center gap-3 bg-white px-3 py-2 dark:bg-[#1a1b25]", "w-12 shrink-0 rounded-lg"
	}
	cover := []core.Node{homeArtwork(item.Poster, "absolute inset-0 justify-center"), a.downloadBadge(entry)}
	var best media.VideoProfile
	for _, profile := range catalogVideoProfiles(entry) {
		if resolution > 0 && libraryResolution(profile) != resolution {
			continue
		}
		if libraryResolution(profile) > libraryResolution(best) {
			best = profile
		}
	}
	quality, tint := "", "bg-black/55"
	if height := libraryResolution(best); height > 0 {
		quality = libraryResolutionName(height)
		if resolution == 0 && best.Range != "" {
			if dynamicRange := detailVideoRange(media.Version{VideoRange: best.Range}); dynamicRange != "" {
				quality = dynamicRange
			}
		}
	} else if item.Kind == media.Series && homeIsFresh(cx, item) && !watched {
		quality, tint = locales.HomeNewShort(cx), "bg-violet-500"
	}
	if quality != "" {
		cover = append(cover, core.Text{Content: quality, TestID: a.catalogEntryID("library-quality-", entry),
			Class: "absolute top-1.5 left-1.5 rounded-md px-1.5 py-0.5 text-[9px] font-bold text-white " + tint})
	}
	if watched {
		cover = append(cover, core.View{Class: "absolute top-1.5 right-1.5 size-5 items-center justify-center rounded-full bg-violet-500 shadow-md",
			Children: []core.Node{core.Icon{SVG: lucide.Check(), Class: "size-3 text-white"}}})
	} else if progress.Duration > 0 && canResume(progress) {
		cover = append(cover, homeProgress(cx, "", "absolute inset-x-2 bottom-2", progress))
	}
	return base.Button{Label: item.Title, Hint: entry.source.Name, TestID: a.catalogEntryID("title-", entry),
		Class: shape, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		OnPress: func(core.PressEvent) { a.openCatalogEntry(entry) },
		Children: []core.Node{
			core.View{Class: "relative overflow-hidden " + coverShape, Style: core.Style{AspectRatio: 0.7},
				TestID: a.catalogEntryID("library-poster-", entry), Children: cover},
			core.View{Class: "min-w-0 gap-1.5", Children: []core.Node{
				core.Text{Content: item.Title, Class: "text-[13px] font-medium " + setupTitle, MaxLines: 1},
				core.Text{Content: details, Class: "text-[11px] " + setupMuted, MaxLines: 1},
			}},
		}}
}
