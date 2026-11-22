package main

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

type nextEpisodeControls struct {
	card        core.Node
	play        func()
	unavailable func() bool
}

func (a *application) nextEpisodeCard(cx *core.Scope, current route, client media.Client,
	position, duration *core.Signal[time.Duration], finished *core.Signal[bool]) nextEpisodeControls {
	episode := current.item
	if episode.Kind != media.Episode || episode.SeriesID == "" || episode.Episode <= 0 {
		return nextEpisodeControls{unavailable: func() bool { return true }}
	}
	upcoming := core.NewResource(cx, nil, func(ctx context.Context, _ struct{}, _ core.ResourceInfo) (media.Item, error) {
		return findNextEpisode(ctx, client, episode)
	})
	dismissed := core.NewSignal(cx, false)
	countdown := core.NewSignal(cx, 0)
	play := func(next media.Item) { a.playEpisodeFrom(current, next) }
	cx.Effect(func() {
		next, ready := upcoming.Get()
		if !ready || next.ID == "" || !finished.Get() || dismissed.Get() || !a.autoNext.Get() {
			countdown.Set(0)
			return
		}
		deadline := cx.Now().Add(10 * time.Second)
		var stop func() bool
		var tick func()
		tick = func() {
			remaining := deadline.Sub(cx.Now())
			if remaining <= 0 {
				play(next)
				return
			}
			countdown.Set(int((remaining + time.Second - 1) / time.Second))
			stop = cx.AfterFunc(min(remaining, time.Second), tick)
		}
		tick()
		cx.Cleanup(func() {
			if stop != nil {
				stop()
			}
		})
	})
	visible := core.NewMemo(cx, func() bool {
		if dismissed.Get() {
			return false
		}
		if finished.Get() {
			return true
		}
		length, at := duration.Get(), position.Get()
		return length > 0 && at > 0 && length-at <= min(30*time.Second, length/3)
	})
	card := core.Dynamic(func(cx *core.Scope) core.Node {
		next, ready := upcoming.Get()
		if !ready || next.ID == "" || !visible.Get() {
			return nil
		}
		return core.View{Class: "self-end w-full max-w-[360px] gap-2 rounded-xl border border-border bg-card p-4", Children: []core.Node{
			core.Text{Class: "text-sm text-muted-foreground", ContentFunc: func() string {
				if seconds := countdown.Get(); seconds > 0 {
					return locales.NextEpisodeCountdown(cx, strconv.Itoa(seconds))
				}
				return locales.NextEpisode(cx)
			}},
			heading(next.Title),
			core.View{Class: "flex-row flex-wrap gap-2", Children: []core.Node{
				ui.Button{Text: locales.Play(cx), Icon: lucide.SkipForward, TestID: "play-next-episode", OnPress: func() { play(next) }},
				ui.Button{Text: locales.Dismiss(cx), Variant: ui.ButtonGhost, TestID: "dismiss-next-episode", OnPress: func() { dismissed.Set(true) }},
			}},
		}}
	})
	return nextEpisodeControls{
		card: card,
		play: func() {
			cx.Untrack(func() {
				if next, ready := upcoming.Get(); ready && next.ID != "" {
					play(next)
				}
			})
		},
		unavailable: func() bool {
			next, ready := upcoming.Get()
			return !ready || next.ID == ""
		},
	}
}

func (a *application) playEpisodeFrom(current route, next media.Item) {
	stack := a.routes.GetUntracked()
	last := stack[len(stack)-1]
	if last.screen == screenPlayer && last.source.ID == current.source.ID && last.item.ID == current.item.ID && next.ID != current.item.ID {
		if next.LibraryID == "" {
			next.LibraryID = current.item.LibraryID
		}
		stack = slices.Clone(stack)
		stack[len(stack)-1] = route{screen: screenPlayer, source: current.source, item: next, resume: true}
		a.routes.Set(stack)
	}
}

func findNextEpisode(ctx context.Context, client media.Client, current media.Item) (media.Item, error) {
	seasons, err := client.Children(ctx, media.Item{ID: current.SeriesID, Kind: media.Series})
	if err != nil {
		return media.Item{}, err
	}
	slices.SortStableFunc(seasons, func(a, b media.Item) int { return cmp.Compare(a.Season, b.Season) })
	for _, season := range seasons {
		if season.Kind != media.Season || season.Season < current.Season {
			continue
		}
		threshold := 0
		if season.Season == current.Season {
			threshold = current.Episode
		}
		episodes, err := client.Children(ctx, season)
		if err != nil {
			return media.Item{}, err
		}
		var next media.Item
		for _, candidate := range episodes {
			if candidate.Kind == media.Episode && candidate.ID != "" && candidate.ID != current.ID && candidate.Episode > threshold &&
				(next.ID == "" || candidate.Episode < next.Episode) {
				next = candidate
			}
		}
		if next.ID != "" {
			return next, nil
		}
	}
	return media.Item{}, nil
}
