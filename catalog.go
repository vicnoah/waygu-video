package main

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type catalogItem struct {
	source     source
	item       media.Item
	match      *catalogMatch
	pending    bool
	downloadID string
}

type catalogRequest struct {
	sources      []source
	filter       string
	library      string
	search       string
	limit        int
	home         bool
	personal     screen
	category     libraryCategory
	libraryFacts bool
	scan         bool
	merge        bool
	cachedResume map[string][]media.Item
}

type catalogResult struct {
	items         []catalogItem
	resume        []catalogItem
	hasMore       bool
	failure       error
	resumeCache   []resumeCacheUpdate
	resumeCatalog []catalogItem
	mergeResume   bool
	total         int
	connected     bool
	sources       map[string]homeSourceStatus
}

// Phone is NSGo's zero idiom, so an unset override needs its own value.
const inheritedShellIdiom = core.Idiom(255)

var shellIdiom = core.NewKey("waygu.shellIdiom", inheritedShellIdiom)

func appLayoutIdiom(cx *core.Scope) core.Idiom {
	if override := shellIdiom.Use(cx); override != inheritedShellIdiom {
		return override
	}
	idiom := cx.Env().Idiom()
	if idiom == core.IdiomDesktop {
		if width := cx.Env().Window().Width; width > 0 && width < 560 {
			return core.IdiomPhone
		}
	}
	return idiom
}

func (a *application) shell(cx *core.Scope) core.Node {
	layout := core.NewMemo(cx, func() core.Idiom { return appLayoutIdiom(cx) })
	return core.Dynamic(func(cx *core.Scope) core.Node { return a.shellLayout(cx, layout.Get()) })
}

func (a *application) shellLayout(cx *core.Scope, idiom core.Idiom) core.Node {
	if idiom == core.IdiomTablet {
		return a.tabletShell(cx)
	}
	if idiom == core.IdiomTV {
		return a.tvShell(cx)
	}
	if idiom == core.IdiomDesktop {
		return a.desktopShell(cx)
	}
	titles := []string{locales.Home(cx), locales.Library(cx), locales.Search(cx), locales.Settings(cx)}
	icons := []func() *core.SVG{lucide.House, lucide.LibraryBig, lucide.Search, lucide.Settings}
	pages := []screen{screenHome, screenLibrary, screenSearch, screenSettings}
	page := func(cx *core.Scope, index int) core.Node {
		if pages[index] == screenSettings {
			return a.settings(cx)
		}
		if pages[index] == screenSearch && idiom == core.IdiomPhone {
			return a.compactSearch(cx)
		}
		if pages[index] == screenLibrary {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				view := a.libraryView.Get()
				if idiom == core.IdiomPhone && view == screenLibrary {
					return a.compactLibrary(cx)
				}
				return a.browse(cx, view, idiom, nil)
			})
		}
		return a.browse(cx, pages[index], idiom, nil)
	}
	selectTab := func(index int) {
		if index == 1 {
			a.libraryView.Set(screenLibrary)
		}
		a.tab.Set(index)
	}

	systemIcons := []string{"house", "film", "magnifyingglass", "gearshape"}
	var tabs []ui.ShellTab
	for i, title := range titles {
		tabs = append(tabs, ui.ShellTab{Text: title, Icon: icons[i], SystemIcon: systemIcons[i],
			TestID: "tab-" + string(pages[i]), Content: func(cx *core.Scope) core.Node { return page(cx, i) }})
	}
	if idiom == core.IdiomPhone {
		tabs[1].Icon = lucide.Clapperboard
		return a.phoneShell(cx, tabs)
	}
	return ui.TabShell{Tabs: tabs, Selected: a.tab.Get, OnChange: selectTab}
}

func (a *application) browse(cx *core.Scope, page screen, idiom core.Idiom, category *core.Signal[libraryCategory]) core.Node {
	query := core.NewSignal(cx, "")
	draft := core.NewSignal(cx, "")
	library := core.NewSignal(cx, "")
	if category == nil {
		category = core.NewSignal(cx, libraryCategory(""))
	}
	limit := core.NewSignal(cx, 36)
	if page == screenLibrary {
		cx.Effect(func() {
			category.Get()
			library.Set("")
			limit.Set(36)
		})
	}
	personal := screen("")
	if page == screenFavorites || page == screenHistory {
		personal = page
	}
	results := core.NewResource(cx, func() (catalogRequest, bool) {
		sources := a.sources.Get()
		term := query.Get()
		var selectedCategory libraryCategory
		if page != screenHome {
			selectedCategory = category.Get()
		}
		request := catalogRequest{sources: sources, filter: a.sourceFilter.Get(), library: library.Get(), search: term,
			limit: limit.Get(), home: page == screenHome, personal: personal, category: selectedCategory, merge: a.mergeTitles.Get(),
			libraryFacts: page == screenHome && (idiom == core.IdiomTV || idiom == core.IdiomDesktop)}
		if request.home {
			request.cachedResume = core.Untracked(cx, func() map[string][]media.Item { return a.readResumeCache(sources) })
		}
		return request, len(sources) > 0 && (page != screenSearch || term != "")
	}, func(ctx context.Context, request catalogRequest, _ core.ResourceInfo) (catalogResult, error) {
		return a.loadCatalog(ctx, request), nil
	})
	lastEdit := a.libraryChanges.GetUntracked()
	cx.Effect(func() {
		if changes := a.libraryChanges.Get(); changes != lastEdit {
			lastEdit = changes
			results.Refetch()
		}
	})
	if page == screenHome || page == screenHistory {
		lastSync := a.progress.synced.GetUntracked()
		cx.Effect(func() {
			if count := a.progress.synced.Get(); count != lastSync {
				lastSync = count
				results.Refetch()
			}
		})
	}
	if page == screenHome {
		cx.Effect(func() {
			if result, ready := results.Get(); ready && !results.Loading() {
				if idiom == core.IdiomTablet || idiom == core.IdiomDesktop {
					a.sourceStatus.Update(func(current map[string]homeSourceStatus) map[string]homeSourceStatus {
						status := make(map[string]homeSourceStatus, len(current)+len(result.sources))
						if a.sourceFilter.GetUntracked() != "" {
							maps.Copy(status, current)
						}
						maps.Copy(status, result.sources)
						return status
					})
				}
				for _, update := range result.resumeCache {
					a.saveResumeCache(update)
				}
			}
		})
		if idiom == core.IdiomPhone || idiom == core.IdiomTablet {
			return a.home(cx, results, limit, idiom == core.IdiomTablet)
		}
		if idiom == core.IdiomTV {
			return a.tvHome(cx, results)
		}
		if idiom == core.IdiomDesktop {
			return a.desktopHome(cx, results, limit, category)
		}
	}
	title, empty := locales.Home(cx), locales.EmptyLibrary(cx)
	switch page {
	case screenLibrary:
		title = locales.Library(cx)
	case screenSearch:
		title = locales.Search(cx)
	case screenFavorites:
		title, empty = locales.Favorites(cx), locales.EmptyFavorites(cx)
	case screenHistory:
		title, empty = locales.WatchHistory(cx), locales.EmptyWatchHistory(cx)
	}
	controls := []core.Node{
		core.View{Class: "flex-row flex-wrap items-center justify-between gap-3", Children: []core.Node{
			heading(title),
			core.Dynamic(func(cx *core.Scope) core.Node {
				sources := a.sources.Get()
				if len(sources) == 0 {
					return nil
				}
				choices := []ui.Option[string]{{Label: locales.AllSources(cx)}}
				for _, source := range sources {
					if page == screenFavorites && source.Provider == media.Plex {
						continue
					}
					choices = append(choices, ui.Option[string]{Label: source.Name, Value: source.ID})
				}
				return ui.Select[string]{Label: locales.MediaSources(cx), Options: choices, Value: a.sourceFilter.Get,
					OnChange: func(id string) { a.sourceFilter.Set(id); library.Set(""); limit.Set(36) }}
			}),
		}},
	}
	if (idiom == core.IdiomPhone || idiom == core.IdiomTV) && (page == screenLibrary || personal != "") {
		controls = append(controls, a.personalLibrarySwitch(cx, page))
	}
	if page == screenLibrary {
		options := libraryCategories(cx, locales.AllCategories(cx))
		controls = append(controls, core.View{Class: "flex-row flex-wrap items-center gap-3", Children: []core.Node{
			ui.Select[libraryCategory]{Label: locales.LibraryCategory(cx), TestID: "catalog-category", Options: options,
				Value: category.Get, OnChange: category.Set},
			core.Dynamic(func(cx *core.Scope) core.Node {
				choices := []ui.Option[string]{{Label: locales.AllLibraries(cx)}}
				filter := a.sourceFilter.Get()
				request := catalogRequest{category: category.Get()}
				for _, source := range a.sources.Get() {
					if filter != "" && source.ID != filter {
						continue
					}
					for _, library := range source.Libraries {
						if request.includesLibrary(source.ID, library) {
							choices = append(choices, ui.Option[string]{Value: source.ID + "/" + library.ID, Label: library.Name + " · " + source.Name})
						}
					}
				}
				if len(choices) <= 1 {
					return nil
				}
				return ui.Select[string]{Label: locales.Library(cx), TestID: "catalog-library", Options: choices, Value: library.Get,
					OnChange: func(id string) { library.Set(id); limit.Set(36) }}
			}),
		}})
	}
	if page == screenSearch {
		submit := func() { query.Set(strings.TrimSpace(draft.GetUntracked())); limit.Set(36) }
		controls = append(controls, core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
			ui.Input{Label: locales.SearchHint(cx), Placeholder: locales.SearchHint(cx), TestID: "search-query",
				AutoFocus: idiom == core.IdiomTablet, Value: draft.Get, OnChange: func(value string) {
					draft.Set(value)
					if strings.TrimSpace(value) == "" {
						query.Set("")
					}
				}, ReturnKeyType: core.ReturnKeySearch, OnSubmit: func(string) { submit() },
				Style: core.Style{FlexGrow: 1, FlexShrink: 1, Height: core.Pt(44)}},
			ui.Button{Label: locales.SearchAction(cx), TestID: "search-submit", Icon: lucide.Search, Size: ui.SizeLarge, OnPress: submit},
		}})
	}
	controls = append(controls, core.Dynamic(func(cx *core.Scope) core.Node {
		if len(a.sources.Get()) == 0 {
			return core.View{Class: "items-center gap-4 rounded-3xl border border-border bg-card p-8", Children: []core.Node{
				core.Icon{SVG: lucide.LibraryBig(), Class: "size-10 text-primary"}, hint(locales.NoSources(cx)),
				ui.Button{Text: locales.AddSource(cx), Icon: lucide.Plus, TestID: "empty-add-source",
					OnPress: func() { a.push(route{screen: screenSources}) }},
			}}
		}
		if page == screenSearch && query.Get() == "" {
			return nil
		}
		var local []catalogItem
		if page == screenLibrary {
			local = a.libraryDownloads(catalogRequest{filter: a.sourceFilter.Get(), library: library.Get(), category: category.Get()})
		}
		loading := results.Loading()
		if loading && page != screenHome && len(local) == 0 {
			return hint(locales.Loading(cx))
		}
		result, ok := results.Get()
		if !ok && !loading && len(local) == 0 {
			return nil
		}
		if loading {
			result = catalogResult{}
		}
		result.items = addLibraryDownloads(result.items, local)
		var children []core.Node
		if result.failure != nil {
			children = append(children, notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx),
				Variant: ui.ButtonOutline, OnPress: results.Refetch})
		}
		if page == screenHome {
			result.resume = a.homeResume(result, loading)
			if len(result.items) > 0 {
				children = append(children, a.hero(cx, result.items[0]))
			}
			if len(result.resume) > 0 {
				children = append(children, heading(locales.ContinueWatching(cx)), a.resumeShelfView(cx, result.resume))
			}
			if len(result.items) > 0 {
				children = append(children, heading(locales.Recent(cx)))
			}
		}
		if len(result.items) > 0 {
			cards := make([]core.Node, 0, len(result.items))
			for _, entry := range result.items {
				cards = append(cards, a.posterCard(cx, entry))
			}
			children = append(children, core.View{Class: "grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5 xl:grid-cols-6", Children: cards})
		} else if loading {
			children = append(children, hint(locales.Loading(cx)))
		} else if result.failure == nil && len(result.resume) == 0 {
			children = append(children, notice(empty))
		}
		if result.hasMore && limit.Get() < 200 {
			children = append(children, ui.Button{Text: locales.NextPage(cx), Variant: ui.ButtonOutline,
				OnPress: func() { limit.Update(func(old int) int { return min(old+36, 200) }) }})
		}
		return core.View{Class: "gap-5", Children: children}
	}))
	return core.Scroll{Class: "flex-1", Children: []core.Node{core.View{
		Class: "gap-5 p-5 md:p-8", StyleFunc: func() core.Style {
			return core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Bottom: core.EnvKeyboard},
				Padding: core.Edges{Bottom: 24 + tabBarInset(cx)}}
		}, Children: controls,
	}}}
}

func (a *application) homeResume(result catalogResult, loading bool) []catalogItem {
	request := catalogRequest{filter: a.sourceFilter.Get(), home: true}
	sources := a.sources.Get()
	merge := result.mergeResume || loading && len(sources) > 1 && request.filter == "" && a.mergeTitles.Get()
	resume := a.progressResumeShelf(result.resume, sources, request)
	return continueWatching(resume, result.resumeCatalog, merge)
}

func (a *application) loadCatalog(ctx context.Context, q catalogRequest) catalogResult {
	groups := make([]catalogResult, len(q.sources))
	var workers sync.WaitGroup
	for i, source := range q.sources {
		if q.personal == screenFavorites && source.Provider == media.Plex {
			continue
		}
		if q.filter == "" || q.filter == source.ID {
			workers.Go(func() { groups[i] = a.loadServerCatalog(ctx, source, q) })
		}
	}
	workers.Wait()
	var all catalogResult
	if q.home {
		all.sources = make(map[string]homeSourceStatus, len(q.sources))
	}
	for index, group := range groups {
		all.items = append(all.items, group.items...)
		all.resume = append(all.resume, group.resume...)
		all.resumeCache = append(all.resumeCache, group.resumeCache...)
		all.hasMore = all.hasMore || group.hasMore
		all.failure = errors.Join(all.failure, group.failure)
		all.connected = all.connected || group.connected
		if q.home && (q.filter == "" || q.filter == q.sources[index].ID) {
			all.sources[q.sources[index].ID] = homeSourceStatus{total: group.total, connected: group.connected, failed: group.failure != nil}
		}
	}
	if q.personal == screenHistory {
		sortRecent(all.items)
		return all
	}
	merge := q.merge && len(q.sources) > 1 && q.filter == "" && q.library == ""
	if q.home && merge {
		all.resumeCatalog, all.mergeResume = all.items, true
	}
	if merge {
		all.items = mergeCatalogItems(all.items, q.home, q.category)
	}
	return all
}

func (q catalogRequest) includesLibrary(sourceID string, library sourceLibrary) bool {
	if library.Hidden || (q.home && library.HideFromHome) || (q.library != "" && q.library != sourceID+"/"+library.ID) {
		return false
	}
	category := library.category()
	return q.category == "" || category == "" || category == q.category
}

func (a *application) loadServerCatalog(ctx context.Context, source source, q catalogRequest) catalogResult {
	libraries := make([]sourceLibrary, 0, len(source.Libraries))
	for _, library := range source.Libraries {
		if q.includesLibrary(source.ID, library) {
			libraries = append(libraries, library)
		}
	}
	if len(libraries) == 0 {
		return catalogResult{}
	}
	client, err := a.client(source)
	if err != nil {
		result := catalogResult{failure: err}
		if q.home {
			result.resume = cachedResumeShelf(source, libraries, q.cachedResume[source.ID])
		}
		return result
	}
	list := func(ctx context.Context, _ media.Library, query media.Query) (media.Page, error) {
		return client.Catalog(ctx, query)
	}
	switch q.personal {
	case screenFavorites:
		favorites, ok := client.(media.FavoriteClient)
		if !ok {
			return catalogResult{}
		}
		list = func(ctx context.Context, _ media.Library, query media.Query) (media.Page, error) {
			return favorites.Favorites(ctx, query)
		}
	case screenHistory:
		history, ok := client.(media.HistoryClient)
		if !ok {
			return catalogResult{}
		}
		list = history.History
	}
	var result catalogResult
	seen := make(map[string]bool)
	for _, library := range libraries {
		query := media.Query{LibraryID: library.ID, Limit: q.limit, Search: q.search, CatalogFacts: q.libraryFacts}
		var page media.Page
		var err error
		if q.scan {
			page, err = scanCatalogPages(ctx, client.Catalog, query)
		} else {
			page, err = list(ctx, library.Library, query)
		}
		globalSearch := err != nil && source.Provider == media.Flymoo && q.search != "" && q.library == ""
		if globalSearch {
			// Older FnOS servers omit the library ID on global search results.
			fallback := media.Query{Limit: q.limit, Search: q.search, CatalogFacts: q.libraryFacts}
			if q.scan {
				page, err = scanCatalogPages(ctx, client.Catalog, fallback)
			} else {
				page, err = client.Catalog(ctx, fallback)
			}
		}
		if err != nil {
			result.failure = errors.Join(result.failure, err)
			if len(page.Items) == 0 {
				continue
			}
		}
		result.connected = true
		result.total += max(page.Total, len(page.Items))
		for _, item := range page.Items {
			if q.personal != "" && item.LibraryID != "" && item.LibraryID != library.ID {
				continue
			}
			if globalSearch && (len(libraries) < len(source.Libraries) || q.category != "") && item.LibraryID != library.ID {
				continue
			}
			if !seen[item.ID] && library.matchesCategory(q.category, item) {
				if item.LibraryID == "" && !globalSearch {
					item.LibraryID = library.ID
				}
				result.items = append(result.items, catalogItem{source: source, item: item})
				seen[item.ID] = true
			}
		}
		result.hasMore = result.hasMore || page.Total > len(page.Items)
	}
	if q.home {
		var refreshed bool
		var resumeError error
		result.resume, refreshed, resumeError = resumeShelf(ctx, client, source, libraries, q.cachedResume[source.ID])
		result.failure = errors.Join(result.failure, resumeError)
		if q.merge && len(q.sources) > 1 && q.filter == "" {
			enrichResumeSeries(ctx, client, result.resume, result.items)
		}
		if refreshed {
			result.resumeCache = []resumeCacheUpdate{{source: source, items: result.resume}}
		}
	} else if q.libraryFacts && q.category != libraryMovies && !q.scan {
		result.resume, _, _ = resumeShelf(ctx, client, source, libraries, nil)
	}
	return result
}

func (a *application) hero(cx *core.Scope, entry catalogItem) core.Node {
	item := entry.item
	art := item.Backdrop
	if art == "" {
		art = item.Poster
	}
	return core.View{Class: "relative h-[300px] justify-end overflow-hidden rounded-3xl bg-zinc-900 md:h-[380px]", Children: []core.Node{
		core.Image{Class: "absolute inset-0", Source: core.Src(art), ResizeMode: core.Cover, ElementsHidden: true},
		core.View{Class: "absolute inset-0 bg-linear-to-t from-black via-black/55 to-black/5"},
		core.View{Class: "gap-3 p-6 md:p-8", Children: []core.Node{
			core.Text{Content: entry.source.Name, Class: "text-sm font-medium text-violet-200"},
			core.Text{Content: item.Title, Class: "text-3xl font-bold text-white md:text-5xl"},
			core.Text{Content: itemMetadata(cx, item), Class: "text-sm text-zinc-200"},
			core.View{Class: "flex-row gap-2", Children: []core.Node{
				ui.Button{Text: locales.Play(cx), Icon: lucide.Play, Size: ui.SizeLarge, OnPress: func() { a.playCatalogEntry(entry) }},
				ui.Button{Text: locales.Details(cx), Icon: lucide.Info, Variant: ui.ButtonSecondary, Size: ui.SizeLarge,
					OnPress: func() { a.openCatalogEntry(entry) }},
			}},
		}},
	}}
}

func (a *application) openCatalogEntry(entry catalogItem) {
	a.push(route{screen: screenDetail, source: entry.source, item: entry.item, match: entry.match, downloadID: entry.downloadID})
}

func (a *application) playCatalogEntry(entry catalogItem) {
	if !playable(entry.item) {
		a.openCatalogEntry(entry)
		return
	}
	next := route{screen: screenPlayer, source: entry.source, item: entry.item, resume: true, match: entry.match}
	if remembered := a.rememberedCopy(next); remembered != "" {
		for _, copy := range entry.match.copies {
			if encodeCopyKey(copy.key()) == remembered {
				next.source, next.item = copy.source, copy.item
				break
			}
		}
	}
	a.push(next)
}

func (a *application) catalogEntryID(prefix string, entry catalogItem) string {
	if len(a.sources.GetUntracked()) > 1 {
		return prefix + entry.source.ID + "-" + entry.item.ID
	}
	return prefix + entry.item.ID
}

func (a *application) posterCard(cx *core.Scope, entry catalogItem) core.Node {
	item := entry.item
	id := a.catalogEntryID("title-", entry)
	label := item.Title
	description := itemMetadata(cx, item)
	if len(a.sources.GetUntracked()) > 1 {
		label += " · " + entry.source.Name
		if description != "" {
			description += " · "
		}
		description += entry.source.Name
	}
	style := core.Style{FlexShrink: 1, CornerRadius: 16, FocusVisible: ui.FocusRing(cx, 3)}
	art := core.Node(core.Icon{SVG: lucide.Film(), Class: "size-9 text-primary"})
	if item.Poster != "" {
		art = core.Image{Class: "absolute inset-0", Source: core.Src(item.Poster), ResizeMode: core.Cover, ElementsHidden: true}
	}
	artwork := []core.Node{art, a.downloadBadge(entry)}
	return base.Button{Label: label, Style: style, TestID: id,
		OnPress: func(core.PressEvent) {
			a.openCatalogEntry(entry)
		}, Children: []core.Node{core.View{Class: "w-full gap-2", Children: []core.Node{
			core.View{Class: "relative h-52 items-center justify-center overflow-hidden rounded-2xl border border-border bg-card", Children: artwork},
			core.Text{Content: item.Title, Class: "line-clamp-1 text-sm font-semibold"},
			hint(description),
		}}}}
}

func itemMetadata(cx *core.Scope, item media.Item) string {
	var parts []string
	switch item.Kind {
	case media.Movie, media.Video:
		parts = append(parts, locales.MovieType(cx))
	case media.Series:
		parts = append(parts, locales.SeriesType(cx))
	case media.Episode:
		parts = append(parts, locales.EpisodeType(cx))
	}
	if item.Year > 0 {
		parts = append(parts, strconv.Itoa(item.Year))
	}
	if item.Rating > 0 {
		parts = append(parts, strconv.FormatFloat(item.Rating, 'f', 1, 64))
	}
	return strings.Join(parts, " · ")
}

func playable(item media.Item) bool {
	return item.Kind == media.Movie || item.Kind == media.Episode || item.Kind == media.Video
}
