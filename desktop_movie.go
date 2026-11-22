package main

import (
	"encoding/json"
	"slices"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) desktopMovieDetail(cx *core.Scope, current route, copies func() []ui.Option[copyKey], selectCopy func(copyKey)) core.Node {
	state := a.movieDetailState(cx, current, true)
	extras := a.movieFeatures(cx, current, false)
	related := a.movieFeatures(cx, current, true)
	matchOpen := core.NewSignal(cx, false)
	refreshMatch := core.NewSignal(cx, uint64(0))
	closeMatch := func() {
		cx.Batch(func() {
			matchOpen.Set(false)
			refreshMatch.Update(func(revision uint64) uint64 { return revision + 1 })
		})
	}
	paneWidth := func() core.Points {
		width := cx.Env().Window().Width
		if !a.desktopCollapsed.Get() {
			width -= 240
		}
		return width
	}
	mask := core.NewMemo(cx, func() bool { return paneWidth() < 720 })
	stacked := core.NewMemo(cx, func() bool { return paneWidth() < 900 })
	reload := func() {
		state.loaded.Refetch()
		state.available.Refetch()
		extras.Refetch()
		related.Refetch()
	}
	hero := core.Dynamic(func(cx *core.Scope) core.Node {
		clips, _ := extras.Get()
		var trailer func()
		if index := slices.IndexFunc(clips, func(item media.Item) bool { return item.Trailer }); index >= 0 {
			trailer = func() { a.push(route{screen: screenPlayer, source: current.source, item: clips[index]}) }
		}
		return a.desktopMovieHero(cx, current, state, mask.Get(), reload, trailer)
	})
	main := core.Dynamic(func(cx *core.Scope) core.Node {
		item := state.item()
		left := []core.Node{desktopMoviePlaybackOptions(cx, state), state.personal.error}
		if result, ready := state.loaded.Get(); ready && result.failure != nil {
			left = append(left, core.View{Class: "gap-2", Children: []core.Node{
				notice(errorText(cx, result.failure)), ui.Button{Text: locales.Retry(cx), Variant: ui.ButtonOutline, OnPress: reload},
			}})
		}
		if state.available.Err() != nil {
			left = append(left, ui.Button{Text: locales.ReloadPlaybackVersions(cx), Variant: ui.ButtonOutline, OnPress: state.available.Refetch})
		}
		left = append(left, desktopMovieSynopsis(cx, item, state.loaded.Loading()))
		if cast := desktopMovieCast(cx, item.People); cast != nil {
			left = append(left, cast)
		}
		if items, ready := related.Get(); ready {
			if recommendations := a.desktopMovieRecommendations(cx, current.source, item.ID, items); recommendations != nil {
				left = append(left, recommendations)
			}
		}
		columns, side := "flex-row gap-10", "w-80 shrink-0 gap-4"
		if stacked.Get() {
			columns, side = "gap-8", "gap-4"
		}
		return core.View{Class: "px-10 pt-8 pb-10 " + columns, TestID: "desktop-movie-body", Children: []core.Node{
			core.View{Class: "min-w-0 flex-1 gap-8", Children: left},
			core.View{Class: side, Children: []core.Node{
				desktopMovieFile(cx, current, item, state.active(), copies, selectCopy),
				core.Dynamic(func(cx *core.Scope) core.Node {
					refreshMatch.Get()
					return a.desktopMovieDanmaku(cx, current, func() { matchOpen.Set(true) })
				}),
			}},
		}}
	})
	return core.View{Class: "relative min-h-0 flex-1 bg-[#f4f4f7] dark:bg-[#0f1017]", TestID: "desktop-movie", Children: []core.Node{
		homeAmbient(false, true), homeAmbient(true, true),
		core.Scroll{Class: "flex-1", TestID: "desktop-movie-scroll", Children: []core.Node{hero, main}},
		ui.Dialog{Title: locales.Danmaku(cx), TestID: "detail-danmaku-dialog", Open: matchOpen.Get,
			OnOpenChange: func(open bool, _ base.Reason) {
				if !open {
					closeMatch()
				}
			},
			Children: []core.Node{core.Show{When: matchOpen.Get, Then: func(cx *core.Scope) core.Node {
				return core.Scroll{Class: "max-h-[350px]", Children: []core.Node{
					a.onlineDanmaku(cx, current, media.Stream{}, nil, closeMatch),
				}}
			}}}, Footer: []core.Node{ui.Button{Text: locales.Back(cx), TestID: "detail-close-danmaku", OnPress: closeMatch}}},
	}}
}

func (a *application) desktopMovieRecommendations(cx *core.Scope, source source, current string, items []media.Item) core.Node {
	seen := map[string]bool{current: true}
	var cards []core.Node
	for _, item := range items {
		if item.Kind != media.Movie || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		entry := catalogItem{source: source, item: item}
		cards = append(cards, a.compactMediaCard(cx, entry, nil, 0, "w-36 shrink-0 gap-1.5 rounded-xl",
			"h-52 rounded-xl shadow-lg shadow-black/15", true, func() { a.openCatalogEntry(entry) }))
	}
	if len(cards) == 0 {
		return nil
	}
	return core.View{Class: "gap-4", Children: []core.Node{
		desktopMovieHeading(locales.RelatedMovies(cx)),
		homeRail("desktop-movie-recommendations", "gap-4", "pb-2", cards),
	}}
}

func (a *application) desktopMovieDanmaku(cx *core.Scope, current route, open func()) core.Node {
	status, action := locales.MovieDanmakuUnmatched(cx), locales.DesktopDanmakuMatch(cx)
	var bound danmaku.DandanEpisode
	if raw, found := a.storage.GetItem(danmakuBindingKey(current)); found && json.Unmarshal([]byte(raw), &bound) == nil && bound.ID > 0 {
		status, action = locales.DesktopDanmakuMatched(cx), locales.DesktopDanmakuChange(cx)
	}
	row := []core.Node{
		core.Icon{SVG: lucide.MessageSquareText(), Class: "size-4 shrink-0 text-fuchsia-500"},
		core.Text{Content: status, Class: "min-w-0 flex-1 text-xs text-zinc-900/80 dark:text-zinc-50/80", TestID: "desktop-movie-danmaku-status"},
	}
	if current.downloadID == "" {
		row = append(row, base.Button{Label: action, TestID: "detail-danmaku",
			Class: "shrink-0 rounded-sm", Style: core.Style{FocusVisible: ui.FocusRing(cx, 1)},
			OnPress: func(core.PressEvent) { open() }, Children: []core.Node{
				core.Text{Content: action, Class: "text-xs " + setupAccent},
			}})
	}
	return core.View{Class: desktopMoviePanel, TestID: "desktop-movie-danmaku", Children: []core.Node{
		core.Text{Content: locales.Danmaku(cx), Class: "font-semibold " + setupTitle},
		core.View{Class: "flex-row items-center gap-2", Children: row},
	}}
}
