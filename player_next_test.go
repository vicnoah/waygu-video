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
	app := openFixtureEpisode(t, false)
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

func TestPhoneSleepTimerFollowsAutoNext(t *testing.T) {
	app := openFixtureEpisode(t, true)
	const length = time.Minute
	app.Play("id:video-player", core.VideoInfo{Duration: length})
	choosePhonePlayerAction(app, "sleep")
	app.Press("id:phone-player-sleep-15")
	app.FastForward(time.Minute)
	app.Progress("id:video-player", length, length)
	app.WaitFor("text:Next episode in 10 s", time.Second)
	app.FastForward(10 * time.Second)
	app.WaitFor("id:phone-player >> text:Second episode", time.Second)
	app.Play("id:video-player", core.VideoInfo{Duration: length})
	choosePhonePlayerAction(app, "sleep")
	if !app.Find("id:phone-player-sleep-15").Props.Checked {
		t.Fatal("autoplay lost the active sleep timer")
	}
	app.Press("id:phone-player-info-close")
	app.FastForward(13*time.Minute + 49*time.Second)
	if app.FindMounted("id:video-player").Video.Paused {
		t.Fatal("sleep timer paused before its original deadline")
	}
	app.FastForward(time.Second)
	if !app.FindMounted("id:video-player").Video.Paused {
		t.Fatal("sleep timer did not pause after the original deadline")
	}
	choosePhonePlayerAction(app, "sleep")
	if !app.Find("id:phone-player-sleep-0").Props.Checked {
		t.Fatal("expired sleep timer remained selected")
	}
	app.Press("id:phone-player-info-close")
	app.Progress("id:video-player", length, length)
	app.FastForward(11 * time.Second)
	if title := app.Find("id:player-title").Text; title != "Second episode" {
		t.Fatalf("autoplay crossed an expired sleep timer: %q", title)
	}
	app.Press("id:toggle-play")
	app.Progress("id:video-player", length, length)
	app.WaitFor("text:Next episode in 10 s", time.Second)
}

func openFixtureEpisode(t *testing.T, phone bool) *nsgotest.App {
	t.Helper()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		if phone {
			shellIdiom.Provide(cx, core.IdiomPhone)
		}
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return episodeServer{fixtureServer{session: session}}, nil
		}, nil)
	}, nsgotest.Storage(map[string]string{sourcesKey: `[{
		"ID":"example","Provider":"jellyfin","Address":"http://example.test",
		"Name":"Example","Libraries":[{"ID":"shows"}]
	}]`}), nsgotest.Secrets(map[string]string{"example": "fixture-token"}))
	app.WaitFor("id:title-first", time.Second)
	app.Press("id:title-first")
	app.WaitFor("text:Fixture synopsis", time.Second)
	app.Press("id:detail-play")
	if phone {
		app.Resize(844, 390)
	}
	app.WaitFor("id:video-player", time.Second)
	return app
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
