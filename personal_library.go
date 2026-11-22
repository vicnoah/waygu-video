package main

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) openPersonalLibrary(view screen) {
	a.scope.Batch(func() {
		a.sourceFilter.Set("")
		a.libraryView.Set(view)
		a.tab.Set(1)
	})
}

func (a *application) personalLibrarySwitch(cx *core.Scope, page screen) core.Node {
	views := []struct {
		page  screen
		label string
		icon  func() *core.SVG
	}{
		{screenLibrary, locales.AllTitles(cx), nil},
		{screenFavorites, locales.Favorites(cx), lucide.Heart},
		{screenHistory, locales.WatchHistory(cx), lucide.Clock},
	}
	var buttons []core.Node
	for _, view := range views {
		var parts []core.Node
		if view.icon != nil {
			parts = append(parts, core.Icon{SVG: view.icon(), Class: "size-4 " + setupMuted})
		}
		parts = append(parts, core.Text{Content: view.label, Class: "text-sm font-medium " + setupTitle})
		buttons = append(buttons, base.Button{Label: view.label, TestID: "library-view-" + string(view.page),
			Class:    "h-9 flex-row items-center justify-center gap-1.5 rounded-full bg-white px-3 dark:bg-[#1a1b25] selected:bg-violet-500/15 " + setupBorder,
			Selected: func() bool { return view.page == page }, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
			OnPress: func(core.PressEvent) { a.openPersonalLibrary(view.page) }, Children: parts})
	}
	return core.View{Class: "flex-row flex-wrap gap-2", Children: buttons}
}

type detailPersonalActions struct {
	buttons func(*core.Scope, detailResult, bool) []core.Node
	error   core.Node
}

func (a *application) detailPersonal(cx *core.Scope, current route, loaded *core.Resource[detailResult]) detailPersonalActions {
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal[error](cx, nil)
	edit := func(watch bool) {
		result, ready := loaded.Get()
		if !ready || busy.GetUntracked() || loaded.Loading() {
			return
		}
		busy.Set(true)
		failure.Set(nil)
		p := a.progress
		pause := a.pauseProgressReplay()
		previous := pendingProgress{Account: mediaAccountKey(current.source), Update: media.OfflineProgress{ItemID: result.item.ID}}
		var olderSessions []*progressWatch
		if watch {
			for session := range p.live {
				if session.account == previous.Account && session.item.ID == result.item.ID {
					olderSessions = append(olderSessions, session)
				}
			}
		}
		previous = p.entries[previous.key()]
		lifetime := a.scope.Context()
		go func() {
			p.transport.Lock()
			ctx, cancel := context.WithTimeout(lifetime, 15*time.Second)
			client, err := a.client(current.source)
			if err == nil && watch {
				if watcher, ok := client.(media.WatchedClient); ok {
					err = watcher.SetWatched(ctx, result.item, !result.item.Watched)
				} else {
					err = media.ErrProvider
				}
			} else if err == nil {
				if favorites, ok := client.(media.FavoriteClient); ok {
					err = favorites.SetFavorite(ctx, result.item, !result.item.Favorite)
				} else {
					err = media.ErrProvider
				}
			}
			if err == nil {
				// Queued events from an earlier playing session must not
				// restore the bookmark after the explicit watched change.
				for _, session := range olderSessions {
					session.superseded = true
				}
			}
			cancel()
			p.transport.Unlock()
			a.scope.Post(func() {
				defer pause()
				if err == nil {
					if watch {
						a.dropPendingProgress(previous)
					}
					a.libraryChanges.Update(func(count uint64) uint64 { return count + 1 })
				}
				cx.Post(func() {
					busy.Set(false)
					failure.Set(err)
					if err != nil {
						loaded.Refetch()
						return
					}
					if watch {
						result.item.Watched = !result.item.Watched
						result.item.Position = 0
					} else {
						result.item.Favorite = !result.item.Favorite
					}
					loaded.Mutate(result)
				})
			})
		}()
	}
	return detailPersonalActions{
		buttons: func(cx *core.Scope, result detailResult, tablet bool) []core.Node {
			button := func(id string, label string, icon func() *core.SVG, selected, watch bool) core.Node {
				disabled := func() bool { return busy.Get() || loaded.Loading() }
				if tablet {
					return detailPosterAction(cx, id, label, icon(), selected, disabled, func() { edit(watch) })
				}
				return ui.Button{Text: label, Icon: icon, Variant: ui.ButtonOutline, TestID: id, Disabled: disabled,
					OnPress: func() { edit(watch) }}
			}
			var buttons []core.Node
			if result.canFavorite && (playable(result.item) || result.item.Kind == media.Series) {
				label := locales.Favorite(cx)
				if result.item.Favorite {
					label = locales.RemoveFavorite(cx)
				}
				buttons = append(buttons, button("detail-favorite", label, lucide.Heart, result.item.Favorite, false))
			}
			if result.canWatch && playable(result.item) {
				label := locales.MarkWatched(cx)
				if result.item.Watched {
					label = locales.MarkUnwatched(cx)
				}
				buttons = append(buttons, button("detail-watched", label, lucide.CircleCheck, result.item.Watched, true))
			}
			return buttons
		},
		error: core.Dynamic(func(cx *core.Scope) core.Node {
			if err := failure.Get(); err != nil {
				return notice(errorText(cx, err))
			}
			return nil
		}),
	}
}

func detailPosterAction(cx *core.Scope, id, label string, icon *core.SVG, selected bool, disabled func() bool, press func()) core.Node {
	tint := "text-white"
	if selected {
		tint = "text-violet-300"
	}
	return base.Button{Label: label, TestID: id, Selected: func() bool { return selected },
		Disabled: disabled, Style: core.Style{FocusVisible: ui.FocusRing(cx, 2)},
		Class:   "h-14 w-[82px] items-center justify-center gap-1 rounded-xl bg-white/15 selected:bg-violet-500/25 disabled:opacity-50",
		OnPress: func(core.PressEvent) { press() }, Children: []core.Node{
			core.Icon{SVG: icon, Class: "size-4 " + tint},
			core.Text{Content: label, Class: "text-center text-[11px] text-white/80", MaxLines: 2},
		}}
}
