package main

import (
	"cmp"
	cryptorand "crypto/rand"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) openDesktopLibrary(category libraryCategory, source string) {
	a.scope.Batch(func() {
		a.desktopCategory.Set(category)
		state := a.desktopLibraryState.GetUntracked()
		state.filters, state.collection = desktopLibraryFilters{Source: source}, ""
		a.desktopLibraryState.Set(state)
		a.sourceFilter.Set(source)
		a.libraryView.Set(screenLibrary)
		a.tab.Set(1)
	})
}

func (a *application) desktopCollectionSidebar(cx *core.Scope, afterSelect func()) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		nodes := []core.Node{tabletSidebarHeading(locales.DesktopSmartCollections(cx))}
		for _, collection := range a.desktopCollections.Get() {
			nodes = append(nodes, tabletSidebarLink(cx, "desktop-collection-"+collection.ID, collection.Name, lucide.FolderHeart,
				func() bool {
					return a.tab.Get() == 1 && a.libraryView.Get() == screenLibrary &&
						a.desktopLibraryState.Get().collection == collection.ID
				}, func() {
					cx.Batch(func() {
						a.desktopCategory.Set(collection.Category)
						state := a.desktopLibraryState.GetUntracked()
						state.filters, state.sort, state.collection = collection.Filters, collection.Sort, collection.ID
						a.desktopLibraryState.Set(state)
						a.sourceFilter.Set(collection.Filters.Source)
						a.libraryView.Set(screenLibrary)
						a.tab.Set(1)
						afterSelect()
					})
				}))
		}
		return core.View{Class: "gap-0.5", Children: nodes}
	})
}

func (a *application) desktopLibrary(cx *core.Scope, category func() libraryCategory, collapsed func() bool) core.Node {
	current := a.desktopLibraryState
	change := func(edit func(*desktopLibraryState)) {
		state := current.GetUntracked()
		edit(&state)
		current.Set(state)
	}
	updateFilters := func(edit func(*desktopLibraryFilters)) {
		change(func(state *desktopLibraryState) {
			edit(&state.filters)
			state.collection = ""
		})
	}
	pickSource := func(source string) {
		cx.Batch(func() {
			updateFilters(func(f *desktopLibraryFilters) { f.Source, f.Library = source, "" })
			a.sourceFilter.Set(source)
		})
	}
	clear := func() {
		cx.Batch(func() {
			updateFilters(func(f *desktopLibraryFilters) { *f = desktopLibraryFilters{} })
			a.sourceFilter.Set("")
		})
	}
	availableLibraries := func() []ui.Option[string] {
		return libraryChoices(cx, a.browseSources(), current.Get().filters.Source, category())
	}
	selectedSource := core.NewMemo(cx, func() string { return current.Get().filters.Source })
	selectedLibrary := core.NewMemo(cx, func() string { return current.Get().filters.Library })
	refresh := core.NewSignal(cx, uint64(0))
	request := core.NewMemoFunc(cx, func() *catalogRequest {
		refresh.Get()
		a.libraryChanges.Get()
		a.progress.synced.Get()
		return &catalogRequest{sources: a.sources.Get(), parental: a.parentalRules(), filter: selectedSource.Get(), library: selectedLibrary.Get(),
			category: category(), limit: 48, libraryFacts: true, merge: a.mergeTitles.Get()}
	}, nil)
	preview, full := a.libraryPages(cx, request.Get)
	page := currentLibraryPage(cx, request.Get, preview, full)
	items := core.NewMemoFunc(cx, func() []catalogItem {
		return addLibraryDownloads(page.Get().items, a.libraryDownloads(*request.Get()))
	}, nil)
	entries := core.NewMemoFunc(cx, func() []desktopLibraryRow { return desktopLibraryEntries(items.Get()) }, nil)
	filters := core.NewMemo(cx, func() desktopLibraryFilters { return current.Get().filters })
	order := core.NewMemo(cx, func() librarySort { return current.Get().sort })
	rows := core.NewMemoFunc(cx, func() []desktopLibraryRow { return desktopLibraryRows(entries.Get(), filters.Get(), order.Get()) }, nil)
	facets := core.NewMemoFunc(cx, func() libraryFacets { return phoneLibraryFacets(items.Get()) }, nil)
	bookmarks := core.NewMemoFunc(cx, func() map[copyKey]media.Item {
		q := request.Get()
		return libraryBookmarkMap(a.progressResumeShelf(page.Get().resume, q.sources, *q))
	}, nil)
	waiting := func() bool {
		q := request.Get()
		result, ready := preview.Get()
		return len(q.sources) > 0 && (!ready || result.query != q || preview.Loading() || full.Loading())
	}
	filterDialog := core.NewSignal(cx, false)
	title := func() string {
		if id := current.Get().collection; id != "" {
			for _, item := range a.desktopCollections.Get() {
				if item.ID == id {
					return item.Name
				}
			}
		}
		return libraryFilterValue(locales.Library(cx), libraryCategories(cx, ""), category())
	}
	shuffle := func() {
		var candidates []catalogItem
		for _, row := range rows.GetUntracked() {
			if playable(row.entry.item) {
				candidates = append(candidates, row.entry)
			}
		}
		if len(candidates) > 0 {
			a.playCatalogEntry(candidates[rand.IntN(len(candidates))])
		}
	}
	sort := func(order librarySort) { change(func(state *desktopLibraryState) { state.sort = order }) }
	mode := func(mode desktopLibraryMode) { change(func(state *desktopLibraryState) { state.mode = mode }) }
	return core.View{Class: "relative flex-1 min-h-0 gap-5 px-8 pt-6 bg-[#f4f4f7] dark:bg-[#0f1017]", TestID: "desktop-library",
		Children: []core.Node{
			homeAmbient(false, true), homeAmbient(true, true),
			core.View{Class: "min-h-[62px] flex-row flex-wrap items-center gap-4", TestID: "desktop-library-header", Children: []core.Node{
				core.View{Class: "min-w-0 flex-1 gap-1", Children: []core.Node{
					core.Text{ContentFunc: title, Class: "text-3xl font-bold " + setupTitle, Role: core.RoleHeader, TestID: "desktop-library-title", MaxLines: 1},
					core.Text{ContentFunc: func() string {
						return desktopLibrarySummary(cx, category(), rows.Get(), waiting() || page.Get().failure != nil)
					},
						Class: "text-sm " + setupMuted, MaxLines: 2, TestID: "desktop-library-summary"},
				}},
				core.Dynamic(func(cx *core.Scope) core.Node { return desktopLibrarySortMenu(cx, order.Get(), sort) }),
				desktopLibraryModePicker(cx, func() desktopLibraryMode { return current.Get().mode }, mode),
				desktopLibraryAction(cx, "desktop-library-shuffle", locales.DesktopShuffle(cx), lucide.Shuffle(), false,
					"h-9 rounded-lg bg-violet-500 px-3.5 shadow-lg shadow-violet-500/30", "font-medium text-white", false, func() bool {
						return !slices.ContainsFunc(rows.Get(), func(row desktopLibraryRow) bool { return playable(row.entry.item) })
					}, shuffle),
			}},
			desktopLibraryFilterBar(cx, filters.Get, facets.Get, a.browseSources, updateFilters, pickSource, clear, func() { filterDialog.Set(true) }),
			a.desktopLibraryResults(cx, rows, bookmarks, current, collapsed, waiting, func() error { return page.Get().failure },
				func() { refresh.Update(func(value uint64) uint64 { return value + 1 }) }, sort),
			a.desktopLibraryDialog(cx, filterDialog, category, availableLibraries, current, updateFilters, clear),
		}}
}

func desktopLibrarySummary(cx *core.Scope, category libraryCategory, rows []desktopLibraryRow, waiting bool) string {
	if waiting {
		return locales.LibraryLoadedCount(cx, strconv.Itoa(len(rows)))
	}
	sources := make(map[string]bool)
	var bytes int64
	var known int
	for _, row := range rows {
		sources[row.entry.source.ID] = true
		if row.entry.match != nil {
			for _, copy := range row.entry.match.copies {
				sources[copy.source.ID] = true
			}
		}
		if row.size > 0 {
			known++
			bytes += row.size
		}
	}
	count := message.NewPrinter(language.Make(cx.Env().Locale())).Sprint(len(rows))
	label := locales.LibraryTitleCount(cx, count, strconv.Itoa(len(sources)))
	if category == libraryMovies {
		label = locales.DesktopMovieCount(cx, count, strconv.Itoa(len(sources)))
	}
	if known == 0 {
		return label
	}
	if known < len(rows) {
		return locales.DesktopLibraryKnownSize(cx, label, mediaBytes(bytes))
	}
	return locales.DesktopLibraryTotalSize(cx, label, mediaBytes(bytes))
}

func (a *application) desktopLibraryDialog(cx *core.Scope, open *core.Signal[bool], category func() libraryCategory,
	libraries func() []ui.Option[string], current *core.Signal[desktopLibraryState],
	update func(func(*desktopLibraryFilters)), clear func()) core.Node {
	name := core.NewSignal(cx, "")
	failed := core.NewSignal(cx, false)
	close := func() { open.Set(false); name.Set(""); failed.Set(false) }
	save := func() {
		label := strings.TrimSpace(name.GetUntracked())
		if label == "" {
			return
		}
		state := current.GetUntracked()
		entry := desktopCollection{ID: cryptorand.Text(), Name: label, Category: category(), Filters: state.filters, Sort: state.sort}
		items := append(slices.Clone(a.desktopCollections.GetUntracked()), entry)
		if err := a.writeDesktopCollections(items); err != nil {
			failed.Set(true)
			return
		}
		state.collection = entry.ID
		current.Set(state)
		close()
	}
	remove := func() {
		state := current.GetUntracked()
		items := slices.DeleteFunc(slices.Clone(a.desktopCollections.GetUntracked()), func(entry desktopCollection) bool { return entry.ID == state.collection })
		if err := a.writeDesktopCollections(items); err != nil {
			failed.Set(true)
			return
		}
		state.collection = ""
		current.Set(state)
		close()
	}
	return ui.Dialog{Title: locales.LibraryFilters(cx), TestID: "desktop-filter-dialog", Open: open.Get,
		OnOpenChange: func(visible bool, _ base.Reason) {
			if !visible {
				close()
			}
		},
		Children: []core.Node{core.View{Class: "gap-4", Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				return ui.Select[string]{Label: locales.Library(cx), TestID: "desktop-filter-library", Options: libraries(),
					Value: func() string { return current.Get().filters.Library }, OnChange: func(id string) {
						update(func(f *desktopLibraryFilters) { f.Library = id })
					}}
			}),
			ui.Input{Label: locales.DesktopCollectionName(cx), Placeholder: locales.DesktopCollectionName(cx),
				Value: name.Get, OnChange: name.Set, TestID: "desktop-collection-name"},
			core.Show{When: failed.Get, Then: func(cx *core.Scope) core.Node { return notice(locales.LibrarySaveError(cx)) }},
			core.View{Class: "flex-row flex-wrap gap-2", Children: []core.Node{
				ui.Button{Text: locales.DesktopSaveCollection(cx), TestID: "desktop-save-collection",
					Disabled: func() bool { return strings.TrimSpace(name.Get()) == "" }, OnPress: save},
				ui.Button{Text: locales.LibraryClearFilters(cx), Variant: ui.ButtonGhost, TestID: "desktop-dialog-clear-filters", OnPress: clear},
				core.Show{When: func() bool { return current.Get().collection != "" }, Then: func(cx *core.Scope) core.Node {
					return ui.Button{Text: locales.DesktopDeleteCollection(cx), Variant: ui.ButtonOutline, TestID: "desktop-delete-collection", OnPress: remove}
				}},
			}},
		}}}, Footer: []core.Node{ui.Button{Text: locales.LibraryDone(cx), OnPress: close}},
	}
}

func desktopRowKey(row desktopLibraryRow) string {
	entry := row.entry
	return cmp.Or(entry.source.ID, "offline") + "/" + entry.item.ID
}
