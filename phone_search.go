package main

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) compactSearch(cx *core.Scope) core.Node {
	state := a.phoneSearch
	input := &core.Ref{}
	visible := core.NewSignal(cx, 24)
	refresh := core.NewSignal(cx, uint64(0))
	submit := func(raw string) {
		term := strings.TrimSpace(raw)
		cx.Batch(func() {
			state.draft.Set(term)
			state.term.Set(term)
			a.savePhoneSearch(term)
		})
		input.Blur()
	}
	cx.Effect(func() {
		draft := strings.TrimSpace(state.draft.Get())
		if draft != state.term.GetUntracked() {
			stop := cx.AfterFunc(300*time.Millisecond, func() { state.term.Set(draft) })
			cx.Cleanup(func() { stop() })
		}
	})
	filter := core.NewMemo(cx, func() string {
		id := state.source.Get()
		if slices.ContainsFunc(a.sources.Get(), func(s source) bool { return s.ID == id }) {
			return id
		}
		return ""
	})
	request := core.NewMemoFunc(cx, func() *catalogRequest {
		refresh.Get()
		a.libraryChanges.Get()
		a.progress.synced.Get()
		return &catalogRequest{sources: a.sources.Get(), filter: filter.Get(), search: state.term.Get(), limit: 48,
			libraryFacts: true, scan: true, merge: a.mergeTitles.Get()}
	}, nil)
	cx.Effect(func() {
		request.Get()
		visible.Set(24)
	})
	currentRequest := func() (*catalogRequest, bool) {
		q := request.Get()
		return q, q.search != "" && len(q.sources) > 0
	}
	titles := core.NewResource(cx, currentRequest, func(ctx context.Context, q *catalogRequest, _ core.ResourceInfo) (phoneSearchPage, error) {
		catalog := *q
		catalog.merge = false
		ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
		defer cancel()
		return phoneSearchPage{q, a.loadCatalog(ctx, catalog)}, nil
	})
	credits := core.NewResource(cx, currentRequest, func(ctx context.Context, q *catalogRequest, _ core.ResourceInfo) (phoneSearchPage, error) {
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		return phoneSearchPage{q, a.loadSearchCredits(ctx, *q)}, nil
	})
	header := core.View{Class: "gap-3", Children: []core.Node{
		core.View{Class: "gap-3 px-5", Children: []core.Node{
			core.Text{Content: locales.Search(cx), TestID: "phone-search-title", Role: core.RoleHeader,
				Class: "text-[28px] font-bold tracking-tight " + setupTitle},
			core.View{Class: "h-11 flex-row items-center gap-2 rounded-xl bg-white px-3 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder,
				TestID: "phone-search-field", Children: []core.Node{
					searchIconButton(cx, "search-submit", locales.SearchAction(cx), lucide.Search(), func() { submit(state.draft.GetUntracked()) }),
					base.Input{Ref: input, Label: locales.SearchPhoneHint(cx), Placeholder: locales.SearchPhoneHint(cx), TestID: "search-query",
						Class: "h-9 min-w-0 flex-1 bg-transparent text-[15px] " + setupTitle, Value: state.draft.Get, OnChange: state.draft.Set,
						ReturnKeyType: core.ReturnKeySearch, OnSubmit: submit},
					core.Show{When: func() bool { return state.draft.Get() != "" }, Then: func(cx *core.Scope) core.Node {
						return searchIconButton(cx, "search-clear-query", locales.SearchClearQuery(cx), lucide.CircleX(), func() {
							cx.Batch(func() { state.draft.Set(""); state.term.Set("") })
							input.Focus()
						})
					}},
				}},
		}},
		core.Dynamic(func(cx *core.Scope) core.Node {
			return phoneSearchSources(cx, a.sources.Get(), filter.Get, func(id string) {
				cx.Batch(func() {
					state.source.Set(id)
					state.term.Set(strings.TrimSpace(state.draft.GetUntracked()))
				})
				input.Blur()
			})
		}),
	}}
	history := core.Dynamic(func(cx *core.Scope) core.Node {
		return a.phoneSearchHistory(cx, state.history.Get(), submit)
	})
	results := core.Dynamic(func(cx *core.Scope) core.Node {
		q := request.Get()
		if len(q.sources) == 0 {
			return compactLibraryEmpty(cx, lucide.LibraryBig(), locales.NoSources(cx), locales.AddSource(cx),
				func() { a.push(route{screen: screenSources}) })
		}
		if strings.TrimSpace(state.draft.Get()) == "" {
			return nil
		}
		if strings.TrimSpace(state.draft.Get()) != q.search {
			return hintSearch(locales.Loading(cx))
		}
		matched, matchedReady := titles.Get()
		people, peopleReady := credits.Get()
		loading := !matchedReady || matched.request != q || titles.Loading() || !peopleReady || people.request != q || credits.Loading()
		if matched.request != q {
			matched = phoneSearchPage{}
		}
		if people.request != q {
			people = phoneSearchPage{}
		}
		needle := strings.ToLower(q.search)
		local := slices.DeleteFunc(a.libraryDownloads(*q), func(entry catalogItem) bool {
			return !strings.Contains(strings.ToLower(entry.item.Title), needle) &&
				!strings.Contains(strings.ToLower(entry.item.OriginalTitle), needle)
		})
		entries := combinePhoneSearch(q, matched.result.items, people.result.items, local)
		if len(entries) == 0 {
			if loading {
				return hintSearch(locales.Loading(cx))
			}
			if err := errors.Join(matched.result.failure, people.result.failure); err != nil {
				return compactLibraryEmpty(cx, lucide.CircleAlert(), errorText(cx, err), locales.Retry(cx),
					func() { refresh.Update(func(n uint64) uint64 { return n + 1 }) })
			}
			return core.View{Class: "items-center gap-2 px-5 py-12", TestID: "search-empty", Children: []core.Node{
				core.Icon{SVG: lucide.Search(), Class: "size-8 opacity-50 " + setupMuted},
				core.Text{Content: locales.SearchNoResults(cx), Class: "text-center text-sm " + setupMuted},
			}}
		}
		return a.phoneSearchResults(cx, entries, q.search, visible.Get(), loading, matched.result.failure, func() {
			visible.Update(func(n int) int { return n + 24 })
		})
	})
	return core.View{Class: "relative flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", Children: []core.Node{
		homeAmbient(false, true), homeAmbient(true, true),
		core.Scroll{Class: "flex-1", TestID: "phone-search-scroll", Children: []core.Node{
			core.View{Class: "gap-6 pt-2", StyleFunc: func() core.Style {
				return core.Style{
					PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea, Left: core.EnvSafeArea, Right: core.EnvSafeArea, Bottom: core.EnvKeyboard},
					Padding:    core.Edges{Bottom: tabBarInset(cx) + 12},
				}
			}, Children: []core.Node{header, history, results}},
		}},
	}}
}

func hintSearch(text string) core.Node {
	return core.Text{Content: text, Class: "px-5 text-sm " + setupMuted, Role: core.RoleStatus}
}

func searchIconButton(cx *core.Scope, id, label string, icon *core.SVG, press func()) core.Node {
	return base.Button{Label: label, TestID: id, Class: "h-9 shrink-0 items-center justify-center rounded",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { press() },
		Children: []core.Node{core.Icon{SVG: icon, Class: "size-4 " + setupMuted}}}
}

func phoneSearchSources(cx *core.Scope, sources []source, selected func() string, choose func(string)) core.Node {
	if len(sources) == 0 {
		return nil
	}
	tabs := make([]core.Node, 0, len(sources)+1)
	add := func(id, label, testID string) {
		tabs = append(tabs, base.Tab{Label: label, TestID: testID, Selected: func() bool { return selected() == id },
			Class: "group shrink-0 rounded-full border border-zinc-900/8 bg-white px-3 py-1 selected:border-transparent selected:bg-zinc-900 dark:border-white/8 dark:bg-[#1a1b25] dark:selected:bg-zinc-50",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { choose(id) },
			Children: []core.Node{
				core.Text{Content: label, MaxLines: 1,
					Class: "text-xs text-zinc-900/80 group-selected:font-medium group-selected:text-[#f4f4f7] dark:text-zinc-50/80 dark:group-selected:text-[#0f1017]"},
			}})
	}
	add("", locales.SearchAllSources(cx), "search-source-all")
	for _, source := range sources {
		add(source.ID, source.Name, "search-source-"+source.ID)
	}
	return core.Scroll{Horizontal: true, TestID: "search-source-scroll", Children: []core.Node{
		base.Tabs{Label: locales.MediaSources(cx), Class: "flex-row gap-2 px-5", Children: tabs},
	}}
}

func (a *application) phoneSearchHistory(cx *core.Scope, terms []string, search func(string)) core.Node {
	if len(terms) == 0 {
		return nil
	}
	chips := make([]core.Node, 0, len(terms))
	for index, term := range terms {
		chips = append(chips, base.Button{Label: term, TestID: "search-history-" + strconv.Itoa(index),
			Class: "max-w-[208px] shrink-0 rounded-full bg-zinc-900/5 px-3 py-1.5 dark:bg-zinc-50/5",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { search(term) },
			Children: []core.Node{
				core.Text{Content: term, Class: "text-xs text-zinc-900/80 dark:text-zinc-50/80", MaxLines: 1},
			}})
	}
	return core.View{Class: "gap-2", TestID: "search-history", Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between px-5", Children: []core.Node{
			core.Text{Content: locales.SearchRecentQueries(cx), Class: "text-sm font-semibold " + setupTitle, Role: core.RoleHeader},
			base.Button{Label: locales.SearchClearHistory(cx), TestID: "search-clear-history",
				Class: "rounded", Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) {
					a.storage.RemoveItem(phoneSearchHistoryKey)
					a.phoneSearch.history.Set(nil)
				}, Children: []core.Node{core.Text{Content: locales.SearchClear(cx), Class: "text-xs " + setupMuted}}},
		}},
		homeRail("search-history-scroll", "gap-2", "px-5", chips),
	}}
}

func (a *application) phoneSearchResults(cx *core.Scope, entries []catalogItem, term string, limit int, loading bool, failure error, more func()) core.Node {
	var movies, shows []catalogItem
	for _, entry := range entries {
		if entry.item.Kind == media.Series {
			shows = append(shows, entry)
		} else if playable(entry.item) {
			movies = append(movies, entry)
		}
	}
	heading := func(label, id string) core.Node {
		return core.Text{Content: label, Role: core.RoleHeader, TestID: id, Class: "px-5 text-sm font-semibold " + setupTitle}
	}
	var sections []core.Node
	if len(movies) > 0 {
		cards := make([]core.Node, 0, min(len(movies), limit))
		for _, entry := range movies[:min(len(movies), limit)] {
			cards = append(cards, a.compactMediaCard(cx, entry, nil, 0, "w-28 shrink-0 gap-1.5 rounded-xl",
				"w-full rounded-xl shadow-lg shadow-black/15", func() {
					a.savePhoneSearch(term)
					a.openCatalogEntry(entry)
				}))
		}
		sections = append(sections, core.View{Class: "gap-3", Children: []core.Node{
			heading(locales.SearchGroupCount(cx, locales.LibraryMovies(cx), strconv.Itoa(len(movies))), "search-movie-count"),
			homeRail("search-movies", "gap-3", "px-5", cards),
		}})
	}
	if len(shows) > 0 {
		rows := []core.Node{heading(locales.SearchGroupCount(cx, locales.LibrarySeries(cx), strconv.Itoa(len(shows))), "search-series-count")}
		for _, entry := range shows[:min(len(shows), limit)] {
			rows = append(rows, a.phoneSearchShow(cx, entry, func() {
				a.savePhoneSearch(term)
				a.openCatalogEntry(entry)
			}))
		}
		sections = append(sections, core.View{Class: "gap-3", TestID: "search-series", Children: rows})
	}
	people := peopleInSearch(entries, term)
	if len(people) > 0 {
		portraits := make([]core.Node, 0, min(len(people), limit))
		for _, person := range people[:min(len(people), limit)] {
			portraits = append(portraits, searchCreditPortrait(cx, person))
		}
		sections = append(sections, core.View{Class: "gap-3", Children: []core.Node{
			heading(locales.CastAndCrew(cx), "search-people-heading"),
			homeRail("search-people", "gap-4", "px-5", portraits),
		}})
	}
	if loading {
		sections = append(sections, hintSearch(locales.Loading(cx)))
	}
	if failure != nil {
		sections = append(sections, core.View{Class: "px-5", Children: []core.Node{notice(errorText(cx, failure))}})
	}
	if len(movies) > limit || len(shows) > limit || len(people) > limit {
		sections = append(sections, core.View{Class: "px-5", Children: []core.Node{
			ui.Button{Text: locales.NextPage(cx), TestID: "search-more", Variant: ui.ButtonOutline, OnPress: more},
		}})
	}
	return core.View{Class: "gap-6", TestID: "search-results", Children: sections}
}

func (a *application) phoneSearchShow(cx *core.Scope, entry catalogItem, open func()) core.Node {
	item := entry.item
	genre := locales.LibrarySeries(cx)
	if len(item.Genres) > 0 {
		genre = item.Genres[0]
	}
	parts := []string{genre}
	if item.SeasonCount > 0 {
		parts = append(parts, locales.SeriesSeasonCount(cx, strconv.Itoa(item.SeasonCount)))
	}
	if entry.source.Name != "" {
		parts = append(parts, entry.source.Name)
	}
	return base.Button{Label: item.Title, Hint: entry.source.Name, TestID: a.catalogEntryID("title-", entry),
		Class: "mx-5 flex-row items-center gap-3 rounded-2xl bg-white p-2.5 shadow-sm shadow-black/5 dark:bg-[#1a1b25] " + setupBorder,
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { open() },
		Children: []core.Node{
			core.View{Class: "relative h-14 w-24 shrink-0 overflow-hidden rounded-lg", Children: []core.Node{
				homeArtwork(cmp.Or(item.Backdrop, item.Poster), "absolute inset-0"),
			}},
			core.View{Class: "min-w-0 flex-1 gap-0.5", Children: []core.Node{
				core.Text{Content: item.Title, MaxLines: 1, Class: "text-sm font-medium " + setupTitle},
				core.Text{Content: strings.Join(parts, " · "), MaxLines: 1, Class: "text-xs " + setupMuted},
			}},
			core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 shrink-0 opacity-50 " + setupMuted},
		}}
}

func searchCreditPortrait(cx *core.Scope, credit searchPerson) core.Node {
	person := credit.person
	kind := locales.SearchActor(cx)
	switch searchPersonKind(person) {
	case "director":
		kind = locales.Director(cx)
	case "composer":
		kind = locales.SearchComposer(cx)
	}
	return core.View{Class: "w-16 shrink-0 items-center gap-1.5", Children: []core.Node{
		core.View{Class: "relative size-16 overflow-hidden rounded-full border-2 border-white shadow-md shadow-black/10 dark:border-[#1a1b25]",
			Children: []core.Node{homeArtwork(person.Portrait, "absolute inset-0")}},
		core.Text{Content: person.Name, Class: "w-full text-center text-[11px] " + setupTitle, MaxLines: 1},
		core.Text{Content: locales.SearchCreditCount(cx, kind, strconv.Itoa(credit.titles)),
			Class: "w-full text-center text-[10px] " + setupMuted, MaxLines: 1},
	}}
}
