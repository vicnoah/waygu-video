package main

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPlaybackQualityChangesAtCurrentPosition(t *testing.T) {
	reports := make(chan media.PlaybackState, 20)
	requests := make(chan media.PlaybackOptions, 8)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return qualityFixture{fixtureServer{session: session}, reports, requests}, nil
		}, nil)
	}, nsgotest.Size(1000, 720), nsgotest.Offscreen(),
		nsgotest.Storage(map[string]string{sourcesKey: `[{"ID":"example","Provider":"jellyfin","Address":"https://example.test",
			"Name":"Example","Libraries":[{"ID":"movies"}]}]`}),
		nsgotest.Secrets(map[string]string{"example": "fixture-token"}))
	app.WaitFor("id:title-test-film", time.Second)
	app.Press("id:title-test-film")
	app.WaitFor("text:Fixture synopsis", time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	app.Play("id:video-player", core.VideoInfo{Duration: time.Minute, RateChangeable: true})
	const at = 24 * time.Second
	app.Progress("id:video-player", at, time.Minute)
	app.PauseVideo("id:video-player", core.VideoPauseControls)
	app.Press("id:player-tracks")
	app.Press("id:track-tab-quality")
	app.WaitFor("id:player-version-2", time.Second)
	app.Press("id:player-version-2")
	app.WaitFor("text:Could not switch playback quality.", time.Second)
	if video := app.FindMounted("id:video-player").Video; video.Source != qualityFixtureURL+"original.mp4" || !video.Paused {
		t.Fatal("failed quality switch replaced the original stream")
	}

	app.Press("id:player-version-1")
	app.WaitUntil(time.Second, func() bool {
		return app.FindMounted("id:video-player").Video.Source == qualityFixtureURL+"small.mp4"
	})
	app.Play("id:video-player", core.VideoInfo{Duration: time.Minute, RateChangeable: true})
	loaded := app.FindMounted("id:video-player")
	if !slices.Contains(loaded.Seeks, at) || !loaded.Video.Paused {
		t.Fatal("version switch lost playback position or pause state")
	}
	key := versionHistoryKey(route{source: source{ID: "example"}, item: media.Item{ID: "test-film"}})
	if stored, _ := app.Storage().GetItem(key); stored != "small" {
		t.Fatalf("remembered %q after switching versions", stored)
	}

	app.Press("id:player-bitrate-4000000")
	app.WaitUntil(time.Second, func() bool {
		return app.FindMounted("id:video-player").Video.Source == qualityFixtureURL+"small.m3u8"
	})
	app.Play("id:video-player", core.VideoInfo{Duration: time.Minute, RateChangeable: true})
	resumed := app.FindMounted("id:video-player")
	if !slices.Contains(resumed.Seeks, at) || !resumed.Video.Paused {
		t.Fatal("bitrate switch lost playback position or pause state")
	}
	for range 3 {
		<-requests // initial playback, rejected version and successful version switch
	}
	options := <-requests
	if options.SourceID != "small" || options.AudioIndex == nil || *options.AudioIndex != 2 || options.MaxBitrate != 4_000_000 {
		t.Fatalf("bitrate override lost the version or audio selection: %+v", options)
	}

	var events []media.PlaybackState
	timeout := time.After(time.Second)
	for len(events) < 5 {
		select {
		case update := <-reports:
			if update.Event != media.Progress {
				events = append(events, update)
			}
		case <-timeout:
			t.Fatal("missing reports after quality switches")
		}
	}
	for i, want := range []struct {
		event media.PlaybackEvent
		id    string
	}{{media.Started, "original"}, {media.Stopped, "original"}, {media.Started, "small"},
		{media.Stopped, "small"}, {media.Started, "small"}} {
		if event := events[i]; event.Event != want.event || event.Stream.VersionID() != want.id || (i > 0 && event.Position != at) {
			t.Fatalf("incorrect report %d: %s %s at %s", i, event.Event, event.Stream.VersionID(), event.Position)
		}
	}
}

const qualityFixtureURL = "https://example.test/"

type qualityFixture struct {
	fixtureServer
	reports  chan media.PlaybackState
	requests chan media.PlaybackOptions
}

func (qualityFixture) Versions(context.Context, media.Item) ([]media.Version, error) {
	return []media.Version{{ID: "original", Height: 1080}, {ID: "small", Height: 720}, {ID: "removed", Height: 480}}, nil
}

func (f qualityFixture) Playback(_ context.Context, item media.Item, options media.PlaybackOptions) (media.Stream, error) {
	f.requests <- options
	id := options.SourceID
	if id == "" {
		id = "original"
	}
	if id == "removed" {
		return media.Stream{}, media.ErrNoStream
	}
	stream := media.Stream{ItemID: item.ID, SourceID: id, URL: qualityFixtureURL + id + ".mp4", Method: media.DirectPlay,
		Duration: time.Minute, Tracks: []media.Track{{ID: "2", Type: "audio", Selected: true}}}
	if options.MaxBitrate > 0 {
		stream.Method, stream.URL = media.Transcode, qualityFixtureURL+id+".m3u8"
	}
	return stream, nil
}

func (f qualityFixture) Report(_ context.Context, event media.PlaybackState) error {
	f.reports <- event
	return nil
}
