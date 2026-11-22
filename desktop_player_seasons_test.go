package main

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

type desktopSeasonFixture struct {
	media.Client
	releaseFirstSeason <-chan struct{}
}

func (c desktopSeasonFixture) Children(ctx context.Context, item media.Item) ([]media.Item, error) {
	switch item.ID {
	case "series":
		return []media.Item{
			{ID: "first", Kind: media.Season, Season: 1},
			{ID: "second", Kind: media.Season, Season: 2},
		}, nil
	case "first":
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.releaseFirstSeason:
		}
		return []media.Item{
			{ID: "episode-1", Kind: media.Episode, Season: 1, Episode: 1},
			{ID: "episode-1b", Kind: media.Episode, Season: 1, Episode: 2},
		}, nil
	case "second":
		return []media.Item{{ID: "episode-2", Kind: media.Episode, Season: 2, Episode: 1}}, nil
	default:
		return nil, nil
	}
}

func TestDesktopSeasonMenuSurvivesEpisodeLoad(t *testing.T) {
	release := make(chan struct{})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		a := &application{}
		panel := a.desktopPlayerEpisodePanel(cx, desktopPlayerControls{
			current: route{item: media.Item{ID: "playing", SeriesID: "series", Season: 1, Kind: media.Episode}},
			client:  desktopSeasonFixture{releaseFirstSeason: release},
		})
		return core.View{Class: "w-80 flex-1 bg-zinc-900", Children: []core.Node{panel}}
	}, nsgotest.Size(640, 480), nsgotest.Idiom(core.IdiomDesktop),
		nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))

	const trigger = "id:desktop-player-season"
	const secondSeason = "role:menuitem 第 2 季"
	app.WaitFor(trigger, 5*time.Second)
	app.Press(trigger)
	app.WaitFor(secondSeason, 5*time.Second)
	close(release)
	app.WaitFor("id:desktop-episode-episode-1", 5*time.Second)
	app.WaitFor("text:共 2 集", 5*time.Second)
	app.Press(secondSeason)
	app.WaitFor("id:desktop-episode-episode-2", 5*time.Second)
	app.WaitForGone(secondSeason, 5*time.Second)
	app.WaitFor(trigger+" >> text:第 2 季", 5*time.Second)
	app.WaitFor("text:共 1 集", 5*time.Second)
}
