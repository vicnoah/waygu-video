package main

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type tvLayout core.Points

func tvLayoutFor(cx *core.Scope) tvLayout {
	width := cx.Env().Window().Width
	if width <= 0 {
		width = 1920
	}
	return tvLayout(min(width/1920, 1))
}

func (l tvLayout) points(at1920 core.Points) core.Points { return core.Points(l) * at1920 }
func (l tvLayout) length(at1920 core.Points) core.Length { return core.Pt(l.points(at1920)) }

func (l tvLayout) padding(x, y core.Points) core.Edges {
	return core.Edges{Left: l.points(x), Right: l.points(x), Top: l.points(y), Bottom: l.points(y)}
}

func (l tvLayout) icon(svg *core.SVG, size core.Points, class string) core.Icon {
	return core.Icon{SVG: svg, Class: class, Style: core.Style{Width: l.length(size), Height: l.length(size)}}
}

func (l tvLayout) text(value string, size core.Points, class string) core.Text {
	switch size {
	case 14:
		class = "text-sm " + class
	case 16:
		class = "text-base " + class
	case 18:
		class = "text-lg " + class
	case 20:
		class = "text-xl " + class
	case 24:
		class = "text-2xl " + class
	case 30:
		class = "text-3xl " + class
	case 96:
		class = "text-8xl " + class
	}
	return core.Text{Content: value, Class: class, MaxLines: 1, Style: core.Style{FontSize: l.points(size)}}
}

func (l tvLayout) focusRing() *core.Style {
	return &core.Style{Shadows: []core.BoxShadow{core.Ring(max(2, l.points(4)), core.RGB(0x8b5cf6))}}
}

func tvClock(cx *core.Scope, l tvLayout) core.Text {
	clock := core.NewSignal(cx, cx.Now().Format("15:04"))
	cx.Effect(func() {
		clock.Get()
		next := cx.Now().Truncate(time.Minute).Add(time.Minute)
		cx.AfterFunc(next.Sub(cx.Now()), func() { clock.Set(cx.Now().Format("15:04")) })
	})
	text := l.text("", 30, "font-medium text-white")
	text.ContentFunc = clock.Get
	return text
}

type tvShellHomeOptions struct {
	header     core.Node
	homeTab    *core.Ref
	openRecent func()
}

var tvHomeOptions = core.NewKey("waygu.tvHomeOptions", tvShellHomeOptions{})

func (a *application) tvShell(cx *core.Scope) core.Node {
	category := core.NewSignal(cx, libraryCategory(""))
	focusTab := ""
	section := func() screen {
		switch a.tab.Get() {
		case 1:
			return a.libraryView.Get()
		case 2:
			return screenSearch
		case 3:
			return screenSettings
		default:
			return screenHome
		}
	}
	open := func(page screen, kind libraryCategory) {
		a.scope.Batch(func() {
			a.sourceFilter.Set("")
			category.Set(kind)
			switch page {
			case screenHome:
				a.tab.Set(0)
			case screenSearch:
				a.tab.Set(2)
			case screenSettings:
				a.push(route{screen: screenTVSettings})
			default:
				a.libraryView.Set(page)
				a.tab.Set(1)
			}
		})
	}
	l := tvLayoutFor(cx)
	return core.Dynamic(func(cx *core.Scope) core.Node {
		page := section()
		homeTab := &core.Ref{}
		header := a.tvHeader(cx, l, section, category.Get, open, &focusTab, homeTab)
		if page == screenHome {
			tvHomeOptions.Provide(cx, tvShellHomeOptions{
				header:     header,
				homeTab:    homeTab,
				openRecent: func() { open(screenLibrary, "") },
			})
			return a.browse(cx, page, core.IdiomTV, nil)
		}
		content := a.browse(cx, page, core.IdiomTV, category)
		return pageFrame(core.View{Class: "shrink-0 bg-[#171720]", Children: []core.Node{header}}, content)
	})
}

func (a *application) tvHeader(cx *core.Scope, l tvLayout, page func() screen, category func() libraryCategory, open func(screen, libraryCategory), focusTab *string, homeTab *core.Ref) core.Node {
	tabs := []struct {
		id       string
		page     screen
		category libraryCategory
		label    string
		icon     *core.SVG
	}{
		{"search", screenSearch, "", locales.Search(cx), lucide.Search()},
		{"home", screenHome, "", locales.Home(cx), nil},
		{"library", screenLibrary, libraryMovies, locales.LibraryMovies(cx), nil},
		{"series", screenLibrary, librarySeries, locales.LibrarySeries(cx), nil},
		{"anime", screenLibrary, libraryAnime, locales.LibraryAnime(cx), nil},
		{"favorites", screenFavorites, "", locales.Favorites(cx), nil},
		{"settings", screenSettings, "", locales.Settings(cx), lucide.Settings()},
	}
	nodes := make([]core.Node, 0, len(tabs))
	for _, tab := range tabs {
		selected := func() bool { return page() == tab.page && (tab.page != screenLibrary || category() == tab.category) }
		button := core.View{
			Class: "group flex-row items-center justify-center rounded-full selected:bg-white",
			Style: core.Style{Height: l.length(56), Gap: l.points(8), FocusVisible: l.focusRing()},
			Role:  core.RoleButton, Label: tab.label, TestID: "tv-tab-" + tab.id,
			Selected: selected, OnPress: func(core.PressEvent) {
				*focusTab = tab.id
				open(tab.page, tab.category)
			},
		}
		if tab.page == screenHome {
			button.Ref = homeTab
		}
		if *focusTab == tab.id {
			if button.Ref == nil {
				button.Ref = &core.Ref{}
			}
			cx.Post(button.Ref.Focus)
			*focusTab = ""
		}
		if tab.icon != nil {
			button.Children = append(button.Children, l.icon(tab.icon, 26, "text-white/80 group-selected:text-black"))
		}
		if tab.page == screenSearch {
			button.Style.Width = l.length(56)
		} else {
			button.Style.Padding = l.padding(32, 0)
			button.Children = append(button.Children, l.text(tab.label, 24, "font-medium text-white/80 group-selected:font-semibold group-selected:text-black"))
		}
		nodes = append(nodes, button)
	}
	return core.View{Class: "flex-row items-center justify-between", TestID: "tv-header",
		Style: core.Style{Padding: core.Edges{Top: l.points(48), Left: l.points(96), Right: l.points(96), Bottom: l.points(16)}},
		Children: []core.Node{
			core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
				core.Image{Style: core.Style{Width: l.length(48), Height: l.length(48)}, Source: core.ImageSource{Data: welcomeMark}, ResizeMode: core.Contain, ElementsHidden: true},
				l.text(locales.Brand(cx), 30, "font-bold tracking-tight text-white"),
			}},
			core.View{Class: "flex-row items-center rounded-full border border-white/15 bg-black/35 shadow-2xl",
				Style: core.Style{Padding: core.All(l.points(8)), Gap: l.points(4)}, Children: nodes},
			core.View{Class: "flex-row items-center", Style: core.Style{Gap: l.points(24)}, Children: []core.Node{
				tvClock(cx, l),
				core.View{Class: "items-center justify-center rounded-full bg-linear-to-br from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/40 ring-2 ring-white/30",
					Style: core.Style{Width: l.length(56), Height: l.length(56)}, Children: []core.Node{
						core.Dynamic(func(cx *core.Scope) core.Node {
							for _, source := range a.browseSources() {
								if initial, _ := utf8.DecodeRuneInString(strings.TrimSpace(source.Username)); initial != utf8.RuneError {
									return l.text(string(unicode.ToUpper(initial)), 24, "font-semibold text-white")
								}
							}
							return l.icon(lucide.UserRound(), 24, "text-white")
						}),
					}},
			}},
		}}
}

const tvBackground = "bg-waygu-page"

func (a *application) tvHome(cx *core.Scope, results *core.Resource[catalogResult]) core.Node {
	l := tvLayoutFor(cx)
	options := tvHomeOptions.Use(cx)
	dismissals, dismiss := a.tvResumeDismissals(cx)
	favorites := a.homeFavorites(cx)
	seed := rand.Uint64()
	return core.Scroll{Class: "flex-1 " + tvBackground, HideIndicator: true, TestID: "tv-home-scroll", Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node {
			sources := a.browseSources()
			result, ready := results.Get()
			loading := !ready && results.Loading()
			rules := a.parentalRules()
			recent := slices.Clone(rules.catalog(result.items))
			slices.SortStableFunc(recent, func(a, b catalogItem) int { return b.item.AddedAt.Compare(a.item.AddedAt) })
			featured := recommendHomeTitles(recent, 5)
			header := core.View{Class: "bg-[#171720]", Children: []core.Node{options.header}}
			var top core.Node = header
			if len(featured) > 0 {
				top = a.tvFeatured(cx, l, featured, options.header, options.homeTab)
			}

			children := []core.Node{homeAmbient(false, true), homeAmbient(true, true)}
			message := func(parts ...core.Node) core.Node {
				return core.View{Class: "gap-3", Style: core.Style{Padding: l.padding(96, 16)}, Children: parts}
			}
			if len(sources) == 0 {
				children = append(children, message(notice(locales.NoSources(cx)),
					ui.Button{Text: locales.AddSource(cx), Icon: lucide.Plus, TestID: "empty-add-source",
						OnPress: func() { a.push(route{screen: screenSources}) }}))
			} else if result.failure != nil {
				children = append(children, message(notice(errorText(cx, result.failure)),
					ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: results.Refetch}))
			}
			shelves := make(map[string]core.Node)
			settings := a.appearancePrefs.Get().forHome(true)
			if resume := a.homeResume(result, loading); len(resume) > 0 {
				shelves[homeContinue] = core.Dynamic(func(cx *core.Scope) core.Node {
					hidden := dismissals.Get()
					cards := make([]core.Node, 0, len(resume))
					for _, entry := range resume {
						if bookmarked, ok := hidden[tvResumeKey(entry)]; ok && bookmarked == tvBookmark(entry.item) {
							continue
						}
						cards = append(cards, a.tvResumeCard(cx, l, entry, func() { dismiss(entry) }))
					}
					if len(cards) == 0 {
						return nil
					}
					return tvShelf(l, "tv-resume", locales.ContinueWatching(cx), "bg-violet-500",
						l.text(locales.HomeItemCount(cx, strconv.Itoa(len(cards))), 20, tvMuted), nil, 32, cards)
				})
			}
			if len(recent) > 0 {
				total := 0
				for _, status := range result.sources {
					total += status.total
				}
				all := l.action("tv-recent-all", locales.TvAllTitles(cx, strconv.Itoa(max(total, len(recent)))), lucide.ChevronRight(), tvShelfAction, options.openRecent)
				movies, series := recent, []catalogItem(nil)
				if settings.HomeOrdered || !settings.enabled(homeRecent) || !settings.enabled(homeSeries) {
					movies = slices.DeleteFunc(slices.Clone(recent), func(item catalogItem) bool { return item.item.Kind == media.Series })
					series = slices.DeleteFunc(slices.Clone(recent), func(item catalogItem) bool { return item.item.Kind != media.Series })
				}
				if len(movies) > 0 {
					shelves[homeRecent] = a.tvPosters(cx, l, "tv-recent", locales.Recent(cx), "bg-fuchsia-500", nil, all, movies)
				}
				if len(series) > 0 {
					shelves[homeSeries] = a.tvPosters(cx, l, "tv-series", locales.HomeSeriesUpdates(cx), "bg-violet-500", nil, nil, series)
				}
				unmerged := recent
				if len(result.resumeCatalog) > 0 {
					unmerged = rules.catalog(result.resumeCatalog)
				}
				var grouped []core.Node
				sourcesByID := make(map[string]source, len(sources))
				for _, entry := range sources {
					sourcesByID[entry.ID] = entry
				}
				for _, shelf := range settings.tvSourceShelves(sources) {
					if !shelf.Enabled {
						continue
					}
					source := sourcesByID[shelf.ID]
					var items []catalogItem
					for _, entry := range unmerged {
						if entry.source.ID == source.ID {
							items = append(items, entry)
						}
					}
					if len(items) > 0 {
						slices.SortStableFunc(items, func(a, b catalogItem) int { return b.item.AddedAt.Compare(a.item.AddedAt) })
						dot, badge := tvProviderStyle(source.Provider)
						tag := l.text(providerName(cx, source.Provider), 18, "rounded-md px-3 py-1 font-semibold "+badge)
						grouped = append(grouped, a.tvPosters(cx, l, "tv-source-"+source.ID, locales.TvFromSource(cx, source.Name), dot, tag, nil, items))
					}
				}
				if len(grouped) > 0 {
					shelves[homeGrouped] = core.Group(grouped)
				}
			}
			shelves[homeFavorite] = core.Dynamic(func(cx *core.Scope) core.Node {
				page, ready := favorites.Get()
				items := a.parentalRules().catalog(page.items)
				if !ready || len(items) == 0 || favorites.Loading() || a.kidsBrowsing() {
					return nil
				}
				return a.tvPosters(cx, l, "tv-favorites", locales.AppearanceFavorites(cx), "bg-rose-500", nil, nil, items)
			})
			if settings.enabled(homeRandom) && len(recent) > 0 {
				shelves[homeRandom] = a.tvPosters(cx, l, "tv-random", locales.AppearanceRandom(cx), "bg-violet-500", nil, nil, homeSuggestions(recent, seed))
			}
			children = append(children, homeModuleOrder(settings, shelves)...)
			if len(recent) == 0 && loading {
				children = append(children, message(hint(locales.Loading(cx))))
			} else if len(recent) == 0 && len(sources) > 0 && len(children) == 2 {
				children = append(children, message(notice(locales.EmptyLibrary(cx))))
			}
			return core.View{Children: []core.Node{top,
				core.View{Class: "relative", Style: core.Style{Gap: l.points(56), Padding: core.Edges{Bottom: l.points(96)}}, Children: children},
			}}
		}),
	}}
}

func tvProviderStyle(provider media.Provider) (dot, badge string) {
	switch provider {
	case media.Emby:
		return "bg-emerald-400", "bg-emerald-500/15 text-emerald-600 dark:text-emerald-300"
	case media.Jellyfin:
		return "bg-sky-400", "bg-sky-500/15 text-sky-600 dark:text-sky-300"
	case media.Plex:
		return "bg-amber-400", "bg-amber-500/15 text-amber-700 dark:text-amber-300"
	default:
		return "bg-blue-500", "bg-blue-500/15 text-blue-500 dark:text-blue-300"
	}
}
