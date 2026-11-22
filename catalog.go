package main

import (
	"context"
	"errors"
	"slices"
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
	source source
	item   media.Item
}

type catalogRequest struct {
	sources []source
	filter  string
	library string
	search  string
	limit   int
	resume  bool
}

type catalogResult struct {
	items   []catalogItem
	resume  []catalogItem
	hasMore bool
	failure error
}

func (a *application) shell(cx *core.Scope) core.Node {
	titles := []string{locales.Home(cx), locales.Library(cx), locales.Search(cx), locales.Settings(cx)}
	icons := []func() *core.SVG{lucide.House, lucide.LibraryBig, lucide.Search, lucide.Settings}
	pages := []screen{screenHome, screenLibrary, screenSearch, screenSettings}
	page := func(cx *core.Scope, index int) core.Node {
		if pages[index] == screenSettings {
			return a.settings(cx)
		}
		return a.browse(cx, pages[index])
	}
	idiom := cx.Env().Idiom()
	if idiom == core.IdiomDesktop || idiom == core.IdiomTablet || idiom == core.IdiomTV {
		content := core.Dynamic(func(cx *core.Scope) core.Node { return page(cx, a.tab.Get()) })
		if idiom == core.IdiomTV {
			items := []core.Node{brand(locales.Brand(cx))}
			for i := range titles {
				items = append(items, ui.Button{Text: titles[i], Icon: icons[i], Variant: ui.ButtonGhost,
					Size: ui.SizeLarge, OnPress: func() { a.tab.Set(i) }})
			}
			return pageFrame(core.View{Class: "flex-row items-center gap-4 px-8 py-4", Children: items}, content)
		}
		var items []ui.SidebarItem
		for i, title := range titles {
			items = append(items, ui.SidebarItem{ID: string(pages[i]), Text: title, Icon: icons[i], TestID: "tab-" + string(pages[i])})
		}
		sidebar := ui.Sidebar{Sections: []ui.SidebarSection{{Items: items}},
			Selected: func() string { return string(pages[a.tab.Get()]) }, OnSelect: func(id string) {
				if index := slices.Index(pages, screen(id)); index >= 0 {
					a.tab.Set(index)
				}
			}}
		return ui.SidebarShell{DefaultWidth: 210, Fixed: true,
			SidebarActions: []core.Node{brand(locales.Brand(cx))}, Sidebar: []core.Node{sidebar},
			Content: []core.Node{content}}
	}
	systemIcons := []string{"house", "film", "magnifyingglass", "gearshape"}
	var tabs []ui.ShellTab
	for i, title := range titles {
		tabs = append(tabs, ui.ShellTab{Text: title, Icon: icons[i], SystemIcon: systemIcons[i],
			TestID: "tab-" + string(pages[i]), Content: func(cx *core.Scope) core.Node { return page(cx, i) }})
	}
	return ui.TabShell{Tabs: tabs, Selected: a.tab.Get, OnChange: a.tab.Set}
}

func (a *application) browse(cx *core.Scope, page screen) core.Node {
	query := core.NewSignal(cx, "")
	draft := core.NewSignal(cx, "")
	library := core.NewSignal(cx, "")
	limit := core.NewSignal(cx, 36)
	results := core.NewResource(cx, func() (catalogRequest, bool) {
		sources := a.sources.Get()
		term := query.Get()
		return catalogRequest{sources: sources, filter: a.sourceFilter.Get(), library: library.Get(), search: term,
			limit: limit.Get(), resume: page == screenHome}, len(sources) > 0 && (page != screenSearch || term != "")
	}, func(ctx context.Context, request catalogRequest, _ core.ResourceInfo) (catalogResult, error) {
		return a.loadCatalog(ctx, request), nil
	})
	title := locales.Home(cx)
	if page == screenLibrary {
		title = locales.Library(cx)
	} else if page == screenSearch {
		title = locales.Search(cx)
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
					choices = append(choices, ui.Option[string]{Label: source.Name, Value: source.ID})
				}
				return ui.Select[string]{Label: locales.MediaSources(cx), Options: choices, Value: a.sourceFilter.Get,
					OnChange: func(id string) { a.sourceFilter.Set(id); library.Set(""); limit.Set(36) }}
			}),
		}},
	}
	if page == screenLibrary {
		controls = append(controls, core.Dynamic(func(cx *core.Scope) core.Node {
			choices := []ui.Option[string]{{Label: locales.AllLibraries(cx)}}
			filter := a.sourceFilter.Get()
			for _, source := range a.sources.Get() {
				if filter != "" && source.ID != filter {
					continue
				}
				for _, library := range source.Libraries {
					choices = append(choices, ui.Option[string]{Value: source.ID + "/" + library.ID, Label: library.Name + " · " + source.Name})
				}
			}
			if len(choices) <= 1 {
				return nil
			}
			return ui.Select[string]{Label: locales.Library(cx), Options: choices, Value: library.Get,
				OnChange: func(id string) { library.Set(id); limit.Set(36) }}
		}))
	}
	if page == screenSearch {
		submit := func() { query.Set(strings.TrimSpace(draft.GetUntracked())); limit.Set(36) }
		controls = append(controls, core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
			ui.Input{Label: locales.SearchHint(cx), Placeholder: locales.SearchHint(cx), TestID: "search-query",
				Value: draft.Get, OnChange: func(value string) {
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
		if results.Loading() {
			return hint(locales.Loading(cx))
		}
		result, ok := results.Get()
		if !ok {
			return nil
		}
		var children []core.Node
		if result.failure != nil {
			children = append(children, notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx),
				Variant: ui.ButtonOutline, OnPress: results.Refetch})
		}
		if page == screenHome && len(result.items) > 0 {
			children = append(children, a.hero(cx, result.items[0]))
			if len(result.resume) > 0 {
				children = append(children, heading(locales.ContinueWatching(cx)), a.posterShelf(cx, result.resume))
			}
			children = append(children, heading(locales.Recent(cx)))
		}
		if len(result.items) > 0 {
			cards := make([]core.Node, 0, len(result.items))
			for _, entry := range result.items {
				cards = append(cards, a.posterCard(cx, entry, false))
			}
			children = append(children, core.View{Class: "grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5 xl:grid-cols-6", Children: cards})
		} else if result.failure == nil {
			children = append(children, notice(locales.EmptyLibrary(cx)))
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
				Padding: core.Edges{Bottom: 24 + ui.TabBarInset(cx)}}
		}, Children: controls,
	}}}
}

func (a *application) loadCatalog(ctx context.Context, q catalogRequest) catalogResult {
	groups := make([]catalogResult, len(q.sources))
	var workers sync.WaitGroup
	for i, source := range q.sources {
		if q.filter == "" || q.filter == source.ID {
			workers.Go(func() { groups[i] = a.loadServerCatalog(ctx, source, q) })
		}
	}
	workers.Wait()
	var all catalogResult
	for _, group := range groups {
		all.items = append(all.items, group.items...)
		all.resume = append(all.resume, group.resume...)
		all.hasMore = all.hasMore || group.hasMore
		all.failure = errors.Join(all.failure, group.failure)
	}
	return all
}

func (a *application) loadServerCatalog(ctx context.Context, source source, q catalogRequest) catalogResult {
	client, err := a.client(source)
	if err != nil {
		return catalogResult{failure: err}
	}
	var result catalogResult
	seen := make(map[string]bool)
	for _, library := range source.Libraries {
		if q.library != "" && q.library != source.ID+"/"+library.ID {
			continue
		}
		page, err := client.Catalog(ctx, media.Query{LibraryID: library.ID, Limit: q.limit, Search: q.search})
		if err != nil && source.Provider == media.Flymoo && q.search != "" && q.library == "" {
			// Older FnOS servers omit the library ID on global search results.
			page, err = client.Catalog(ctx, media.Query{Limit: q.limit, Search: q.search})
		}
		if err != nil {
			result.failure = errors.Join(result.failure, err)
			continue
		}
		for _, item := range page.Items {
			if !seen[item.ID] {
				result.items = append(result.items, catalogItem{source, item})
				seen[item.ID] = true
			}
		}
		result.hasMore = result.hasMore || page.Total > len(page.Items)
	}
	if q.resume {
		previous, err := client.Resume(ctx)
		if err == nil {
			for _, item := range previous {
				if item.Position > 0 {
					result.resume = append(result.resume, catalogItem{source, item})
				}
			}
		}
	}
	return result
}

func (a *application) hero(cx *core.Scope, entry catalogItem) core.Node {
	item := entry.item
	open := func() { a.push(route{screen: screenDetail, source: entry.source, item: item}) }
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
				ui.Button{Text: locales.Play(cx), Icon: lucide.Play, Size: ui.SizeLarge, OnPress: func() {
					if playable(item) {
						a.push(route{screen: screenPlayer, source: entry.source, item: item, resume: true})
					} else {
						open()
					}
				}},
				ui.Button{Text: locales.Details(cx), Icon: lucide.Info, Variant: ui.ButtonSecondary, Size: ui.SizeLarge, OnPress: open},
			}},
		}},
	}}
}

func (a *application) posterShelf(cx *core.Scope, items []catalogItem) core.Node {
	children := make([]core.Node, 0, len(items))
	for _, entry := range items {
		children = append(children, core.View{Class: "w-36", Children: []core.Node{a.posterCard(cx, entry, true)}})
	}
	return core.Scroll{Horizontal: true, Class: "flex-shrink-0", Children: []core.Node{
		core.View{Class: "flex-row gap-4 pb-2", Children: children},
	}}
}

func (a *application) posterCard(cx *core.Scope, entry catalogItem, resume bool) core.Node {
	item := entry.item
	style := core.Style{FlexShrink: 1, CornerRadius: 16, FocusVisible: ui.FocusRing(cx, 3)}
	art := core.Node(core.Icon{SVG: lucide.Film(), Class: "size-9 text-primary"})
	if item.Poster != "" {
		art = core.Image{Class: "absolute inset-0", Source: core.Src(item.Poster), ResizeMode: core.Cover, ElementsHidden: true}
	}
	return base.Button{Label: item.Title, Style: style, TestID: "title-" + item.ID,
		OnPress: func(core.PressEvent) {
			if resume && playable(item) {
				a.push(route{screen: screenPlayer, source: entry.source, item: item, resume: true})
			} else {
				a.push(route{screen: screenDetail, source: entry.source, item: item})
			}
		}, Children: []core.Node{core.View{Class: "w-full gap-2", Children: []core.Node{
			core.View{Class: "relative h-52 items-center justify-center overflow-hidden rounded-2xl border border-border bg-card", Children: []core.Node{art}},
			core.Text{Content: item.Title, Class: "line-clamp-1 text-sm font-semibold"},
			hint(itemMetadata(cx, item)),
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
