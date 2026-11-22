package main

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPlayNextEpisodeAcrossSeasons(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return episodeServer{fixtureServer{session: session}}, nil
		})
	}, nsgotest.Storage(map[string]string{sourcesKey: `[{
		"ID":"example","Provider":"jellyfin","Address":"http://example.test",
		"Name":"Example","Libraries":[{"ID":"shows"}]
	}]`}), nsgotest.Secrets(map[string]string{"example": "fixture-token"}))
	app.WaitFor("id:title-first", time.Second)
	app.Press("id:title-first")
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	const length = time.Minute
	app.Play("id:video-player", core.VideoInfo{Duration: length})
	app.Progress("id:video-player", 40*time.Second, length)
	app.WaitFor("id:play-next-episode", time.Second)
	app.Progress("id:video-player", length, length)
	app.Find("text:Next episode in 10 s")
	app.FastForward(10 * time.Second)
	app.WaitFor("id:video-player", time.Second)
	if title := app.Find("id:player-title").Text; title != "Second episode" {
		t.Fatalf("next video title: %q", title)
	}
	app.Play("id:video-player", core.VideoInfo{Duration: length})
	app.Progress("id:video-player", length, length)
	app.WaitFor("id:dismiss-next-episode", time.Second)
	app.Press("id:dismiss-next-episode")
	app.FastForward(11 * time.Second)
	if title := app.Find("id:player-title").Text; title != "Second episode" {
		t.Fatalf("next episode dismissal changed the video: %q", title)
	}
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", time.Second)
}

type episodeServer struct{ fixtureServer }

func (episodeServer) Catalog(context.Context, media.Query) (media.Page, error) {
	return media.Page{Items: []media.Item{{ID: "first", Title: "First episode", Kind: media.Episode,
		SeriesID: "series", Season: 1, Episode: 1}}, Total: 1}, nil
}

func (episodeServer) Children(_ context.Context, item media.Item) ([]media.Item, error) {
	switch item.ID {
	case "series":
		return []media.Item{
			{ID: "second-season", Kind: media.Season, SeriesID: "series", Season: 2},
			{ID: "first-season", Kind: media.Season, SeriesID: "series", Season: 1},
		}, nil
	case "first-season":
		return []media.Item{{ID: "first", Kind: media.Episode, SeriesID: "series", Season: 1, Episode: 1}}, nil
	case "second-season":
		return []media.Item{
			{ID: "third", Title: "Third episode", Kind: media.Episode, SeriesID: "series", Season: 2, Episode: 2},
			{ID: "second", Title: "Second episode", Kind: media.Episode, SeriesID: "series", Season: 2, Episode: 1},
		}, nil
	default:
		return nil, nil
	}
}

func (episodeServer) Playback(_ context.Context, item media.Item, _ media.PlaybackOptions) (media.Stream, error) {
	return media.Stream{ItemID: item.ID, URL: "http://example.test/fixture.mp4", Method: media.DirectPlay, Duration: time.Minute}, nil
}
