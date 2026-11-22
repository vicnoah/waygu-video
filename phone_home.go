package main

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type homeSourceStatus struct {
	total             int
	connected, failed bool
}

func (a *application) phoneHome(cx *core.Scope, results *core.Resource[catalogResult], limit *core.Signal[int]) core.Node {
	expansion := core.NewSignal(cx, "")
	selectSource := func(id string) {
		a.sourceFilter.Set(id)
		limit.Set(36)
		expansion.Set("")
	}
	return core.View{Class: "relative flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", Children: []core.Node{
		core.SVGView{Class: "absolute inset-x-0 top-0 h-[1100px] w-full dark:hidden", SVG: welcomeAmbientLight},
		core.SVGView{Class: "absolute inset-x-0 top-0 hidden h-[1100px] w-full dark:flex", SVG: welcomeAmbientDark},
		core.Scroll{Class: "flex-1", TestID: "home-scroll", Children: []core.Node{
			core.View{Class: "gap-8", StyleFunc: func() core.Style {
				return core.Style{PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea},
					Padding: core.Edges{Top: 12, Bottom: 12 + max(tabBarInset(cx), core.Insets(cx).Bottom)}}
			}, Children: []core.Node{
				a.phoneHomeHeader(cx, results, selectSource),
				core.Dynamic(func(cx *core.Scope) core.Node {
					sources := a.sources.Get()
					if len(sources) == 0 {
						return core.View{Class: "mx-5 items-center gap-4 rounded-3xl bg-white p-8 dark:bg-[#1a1b25] " + setupBorder, Children: []core.Node{
							core.Icon{SVG: lucide.Clapperboard(), Class: "size-10 " + setupAccent}, hint(locales.NoSources(cx)),
							ui.Button{Text: locales.AddSource(cx), Icon: lucide.Plus, TestID: "empty-add-source",
								OnPress: func() { a.push(route{screen: screenSources}) }},
						}}
					}
					loading := results.Loading()
					result, _ := results.Get()
					if loading {
						result = catalogResult{}
					}
					resume := a.homeResume(result, loading)
					var sections []core.Node
					if result.failure != nil {
						sections = append(sections, core.View{Class: "mx-5 gap-2", Children: []core.Node{
							notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: results.Refetch},
						}})
					}
					recent := slices.Clone(result.items)
					slices.SortStableFunc(recent, func(a, b catalogItem) int { return b.item.AddedAt.Compare(a.item.AddedAt) })
					if len(recent) > 0 {
						sections = append(sections, a.homeHeroCarousel(cx, recommendHomeTitles(recent)))
					}
					if len(resume) > 0 {
						cards := make([]core.Node, 0, len(resume))
						for _, entry := range resume {
							cards = append(cards, a.homeResumeCard(cx, entry))
						}
						sections = append(sections, homeShelf("resume", locales.ContinueWatching(cx), cards, expansion, nil))
					}
					if len(recent) > 0 {
						var movies, series []core.Node
						for _, entry := range recent {
							card := a.homePosterCard(cx, entry)
							if entry.item.Kind == media.Series {
								series = append(series, card)
							} else {
								movies = append(movies, card)
							}
						}
						var more func()
						if result.hasMore && limit.Get() < 200 {
							more = func() { limit.Update(func(n int) int { return min(n+36, 200) }) }
						}
						if len(movies) > 0 {
							sections = append(sections, homeShelf("recent", locales.Recent(cx), movies, expansion, more))
						}
						if len(series) > 0 {
							sections = append(sections, homeShelf("series", locales.HomeSeriesUpdates(cx), series, expansion, more))
						}
					} else if loading {
						sections = append(sections, core.Text{Content: locales.Loading(cx), Class: "px-5 text-sm " + setupMuted})
					} else if len(resume) == 0 && result.failure == nil {
						sections = append(sections, core.View{Class: "mx-5", Children: []core.Node{notice(locales.EmptyLibrary(cx))}})
					}
					sections = append(sections, a.homeSourceCards(cx, sources, result.sources, selectSource))
					return core.View{Class: "gap-8", Children: sections}
				}),
			}},
		}},
	}}
}

func (a *application) phoneHomeHeader(cx *core.Scope, results *core.Resource[catalogResult], selectSource func(string)) core.Node {
	avatar := base.Button{Label: locales.Settings(cx), TestID: "home-profile", Class: "size-8 items-center justify-center rounded-full bg-linear-to-br from-violet-500 to-fuchsia-500 shadow-md shadow-violet-500/30",
		Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { a.tab.Set(3) },
		Children: []core.Node{core.Dynamic(func(*core.Scope) core.Node {
			for _, source := range a.sources.Get() {
				if filter := a.sourceFilter.Get(); filter != "" && filter != source.ID {
					continue
				}
				if initial, _ := utf8.DecodeRuneInString(strings.TrimSpace(source.Username)); initial != utf8.RuneError {
					return core.Text{Content: string(unicode.ToUpper(initial)), Class: "text-xs font-semibold text-white"}
				}
			}
			return core.Icon{SVG: lucide.UserRound(), Class: "size-4 text-white"}
		})}}
	return core.View{Class: "gap-3 px-5", Children: []core.Node{
		core.View{Class: "flex-row items-center justify-between gap-2", Children: []core.Node{
			core.View{Class: "flex-row items-center gap-2", Children: []core.Node{
				core.Image{Class: "size-7", Source: core.ImageSource{Data: welcomeMark}, ResizeMode: core.Contain, ElementsHidden: true},
				core.Text{Content: locales.AppName(cx), Class: "text-lg font-bold tracking-tight " + setupTitle, MaxLines: 1},
			}},
			core.View{Class: "flex-row items-center gap-2", Children: []core.Node{a.homeSourceSelect(cx, results, selectSource), avatar}},
		}},
		core.View{Class: "pt-1", Children: []core.Node{
			core.Text{Content: locales.HomeGreeting(cx), Class: "text-3xl font-bold tracking-tight " + setupTitle},
			core.Text{TestID: "home-summary", Class: "text-sm " + setupMuted, ContentFunc: func() string {
				if len(a.sources.Get()) == 0 {
					return ""
				}
				if results.Loading() {
					return locales.Loading(cx)
				}
				result, _ := results.Get()
				online := 0
				for _, status := range result.sources {
					if status.connected {
						online++
					}
				}
				return locales.HomeSummary(cx, strconv.Itoa(online), strconv.Itoa(len(result.items)))
			}},
		}},
	}}
}

func (a *application) homeSourceSelect(cx *core.Scope, results *core.Resource[catalogResult], selectSource func(string)) core.Node {
	return core.Show{When: func() bool { return len(a.sources.Get()) > 0 }, Then: func(cx *core.Scope) core.Node {
		label := locales.AllSources(cx)
		return base.Select{Label: locales.MediaSources(cx), TestID: "home-source-filter",
			Class:      "h-8 max-w-[154px] rounded-full bg-white/80 shadow-sm shadow-black/5 dark:bg-[#1a1b25]/80 " + setupBorder,
			Style:      core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			PopupClass: "min-w-[170px] overflow-hidden rounded-2xl bg-white shadow-lg dark:bg-[#1a1b25] " + setupBorder,
			ItemClass:  "min-h-11 items-center px-3 selected:bg-violet-500/12",
			Items: func() []base.SelectItem {
				items := []base.SelectItem{{Value: "all", Title: label}}
				for _, source := range a.sources.Get() {
					items = append(items, base.SelectItem{Value: "source:" + source.ID, Title: source.Name})
				}
				return items
			},
			Value: func() *string {
				value := "all"
				if id := a.sourceFilter.Get(); id != "" {
					value = "source:" + id
				}
				return &value
			},
			OnChange: func(value *string) {
				if value == nil || *value == "all" {
					selectSource("")
				} else {
					selectSource(strings.TrimPrefix(*value, "source:"))
				}
			},
			Trigger: core.View{Class: "h-full flex-row items-center justify-center gap-1.5 px-3", Children: []core.Node{
				core.View{Class: "size-1.5 shrink-0 rounded-full", ClassFunc: func() string {
					result, _ := results.Get()
					for _, status := range result.sources {
						if status.connected {
							return "bg-emerald-500"
						}
					}
					return "bg-zinc-400"
				}},
				core.Text{Class: "min-w-0 shrink text-xs font-medium " + setupTitle, MaxLines: 1, ContentFunc: func() string {
					for _, source := range a.sources.Get() {
						if source.ID == a.sourceFilter.Get() {
							return source.Name
						}
					}
					return label
				}},
				core.Icon{SVG: lucide.ChevronDown(), Class: "size-3.5 shrink-0 " + setupMuted},
			}},
			RenderItem: func(row base.SelectEntry) core.Node {
				return core.Text{Content: row.Title, Class: "text-sm " + setupTitle, MaxLines: 1}
			},
		}
	}}
}

func (a *application) homeSourceCards(cx *core.Scope, sources []source, statuses map[string]homeSourceStatus, selectSource func(string)) core.Node {
	cards := make([]core.Node, 0, len(sources))
	for _, source := range sources {
		index := slices.IndexFunc(sourceProviders[:], func(entry sourceProvider) bool { return entry.provider == source.Provider })
		if index < 0 {
			continue
		}
		entry := sourceProviders[index]
		status := statuses[source.ID]
		dot, label := "bg-zinc-400", ""
		if status.connected {
			dot, label = "bg-emerald-500", locales.HomeItemCount(cx, strconv.Itoa(status.total))
		}
		if status.failed {
			dot, label = "bg-amber-500", locales.HomeUnavailable(cx)
		}
		detail := providerName(cx, source.Provider)
		if label != "" {
			detail += " · " + label
		}
		cards = append(cards, base.Button{Label: source.Name, Hint: detail, TestID: "home-source-" + source.ID,
			Class:    "w-36 shrink-0 gap-3 rounded-2xl bg-white p-3.5 shadow-sm shadow-black/5 dark:bg-[#1a1b25] selected:border-violet-500 " + setupBorder,
			Selected: func() bool { return a.sourceFilter.Get() == source.ID },
			Style:    core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) {
				if a.sourceFilter.GetUntracked() == source.ID {
					selectSource("")
				} else {
					selectSource(source.ID)
				}
			}, Children: []core.Node{
				core.View{Class: "flex-row items-center justify-between", Children: []core.Node{
					core.View{Class: "size-9 items-center justify-center rounded-xl " + entry.result,
						Children: []core.Node{core.Icon{SVG: entry.icon(), Class: "size-4 " + entry.tint}}},
					core.View{Class: "size-2 rounded-full " + dot},
				}},
				core.View{Class: "gap-0.5", Children: []core.Node{
					core.Text{Content: source.Name, Class: "text-sm font-semibold " + setupTitle, MaxLines: 1},
					core.Text{Content: detail, Class: "text-[11px] " + setupMuted, MaxLines: 1},
				}},
			}})
	}
	return core.View{Class: "gap-3", Children: []core.Node{
		homeSectionTitle(cx, locales.HomeBySource(cx), "", nil, ""),
		homeRail("home-source-rail", "gap-2.5", cards),
	}}
}

func homeSectionTitle(cx *core.Scope, title, more string, selectMore func(), id string) core.Node {
	nodes := []core.Node{core.Text{Content: title, Class: "text-lg font-bold " + setupTitle}}
	if selectMore != nil {
		nodes = append(nodes, base.Button{Label: more, TestID: id, Class: "flex-row items-center gap-0.5",
			Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)}, OnPress: func(core.PressEvent) { selectMore() },
			Children: []core.Node{
				core.Text{Content: more, Class: "text-sm " + setupMuted},
				core.Icon{SVG: lucide.ChevronRight(), Class: "size-4 " + setupMuted},
			}})
	}
	return core.View{Class: "flex-row items-center justify-between px-5", Children: nodes}
}

func homeRail(id, gap string, cards []core.Node) core.Node {
	return core.Scroll{Horizontal: true, TestID: id, Style: core.Style{FlexShrink: 0}, Children: []core.Node{
		core.View{Class: "flex-row px-5 " + gap, Children: cards},
	}}
}

func homeShelf(id, label string, cards []core.Node, expansion *core.Signal[string], more func()) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		title := locales.HomeSeeAll(cx)
		var row core.Node = homeRail("home-"+id+"-rail", "gap-3", cards[:min(len(cards), 12)])
		if expansion.Get() == id {
			title = locales.HomeCollapse(cx)
			row = core.View{Class: "flex-row flex-wrap gap-3 px-5", Children: cards}
		}
		showAll := func() {
			if expansion.GetUntracked() == id {
				expansion.Set("")
			} else {
				expansion.Set(id)
			}
		}
		children := []core.Node{homeSectionTitle(cx, label, title, showAll, "home-"+id+"-all"), row}
		if more != nil && expansion.Get() == id {
			children = append(children, core.View{Class: "px-5", Children: []core.Node{
				ui.Button{Text: locales.NextPage(cx), Variant: ui.ButtonOutline, OnPress: more},
			}})
		}
		return core.View{Class: "gap-3", Children: children}
	})
}
