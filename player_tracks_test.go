package main

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPlaybackTrackControlsKeepPlaybackState(t *testing.T) {
	reports := make(chan media.PlaybackState, 20)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return trackFixtureServer{fixtureServer{session: session}, reports}, nil
		}, nil)
	}, nsgotest.Size(1000, 680),
		nsgotest.Storage(map[string]string{sourcesKey: `[{"ID":"example","Provider":"jellyfin","Address":"https://example.test",
			"Name":"Example","Libraries":[{"ID":"movies"}]}]`}),
		nsgotest.Secrets(map[string]string{"example": "fixture-token"}))

	app.WaitFor("id:title-test-film", time.Second)
	app.Press("id:title-test-film")
	app.WaitFor("text:Fixture synopsis", time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	const at = 12*time.Second + 250*time.Millisecond
	loaded := func() {
		app.Report(nsgotest.Event{Kind: nsgotest.EventLoad, ID: app.FindMounted("id:video-player").ID,
			Duration: time.Minute, RateChangeable: true})
	}
	loaded()
	app.Progress("id:video-player", at, time.Minute)
	app.PauseVideo("id:video-player", core.VideoPauseControls)

	app.Press("id:player-tracks")
	app.PickFiles = func(nsgotest.FilePick) []core.File {
		return []core.File{{Name: "fixture.srt", Opener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("1\n00:00:12,000 --> 00:00:16,000\nFixture caption\n")), nil
		}}}
	}
	app.Press("id:open-subtitle-file")
	app.WaitFor("id:subtitle-later", time.Second)
	app.Press("id:subtitle-later")
	app.WaitForGone("id:subtitle-caption", time.Second)
	app.Press("id:subtitle-earlier")
	app.WaitFor("id:subtitle-caption", time.Second)

	app.Press("id:track-tab-speed")
	app.Press("id:playback-speed-1.5")
	if rate := app.FindMounted("id:video-player").Video.Rate; rate != 1.5 {
		t.Fatalf("native speed = %g", rate)
	}
	app.Press("id:track-tab-audio")
	app.Press("id:audio-track-3")
	app.WaitFor("text:Could not switch audio tracks.", time.Second)
	if previous := app.FindMounted("id:video-player").Video; previous.Source != firstTrackSource || !previous.Paused {
		t.Fatal("a failed audio switch interrupted the previous video")
	}
	app.Press("id:audio-track-2")
	app.WaitUntil(time.Second, func() bool {
		return app.FindMounted("id:video-player").Video.Source == secondTrackSource
	})
	app.Press("id:close-player-tracks")
	app.WaitForGone("id:player-tracks-dialog", time.Second)
	loaded()
	resumed := app.FindMounted("id:video-player")
	if !slices.Contains(resumed.Seeks, at) || resumed.Video.Rate != 1.5 || !resumed.Video.Paused {
		t.Fatalf("track change lost playback state: seeks=%v paused=%v rate=%g", resumed.Seeks, resumed.Video.Paused, resumed.Video.Rate)
	}
	app.WaitFor("text:Fixture caption", time.Second)
	app.Press("id:player-tracks")
	app.Press("id:track-tab-subtitles")
	app.Press("id:subtitles-off")
	app.WaitForGone("id:subtitle-caption", time.Second)

	var lifecycle []media.PlaybackState
	deadline := time.After(time.Second)
	for len(lifecycle) < 3 {
		select {
		case state := <-reports:
			if state.Event != media.Progress {
				lifecycle = append(lifecycle, state)
			}
		case <-deadline:
			t.Fatal("missing playback lifecycle reports")
		}
	}
	if lifecycle[0].Event != media.Started || lifecycle[0].Stream.SessionID != "first" ||
		lifecycle[1].Event != media.Stopped || lifecycle[1].Stream.SessionID != "first" ||
		lifecycle[2].Event != media.Started || lifecycle[2].Stream.SessionID != "second" ||
		lifecycle[1].Position != at || lifecycle[2].Position != at {
		t.Fatal("the server did not receive the audio switch in playback order")
	}
}

const (
	firstTrackSource  = "https://example.test/english.mp4"
	secondTrackSource = "https://example.test/japanese.mp4"
)

type trackFixtureServer struct {
	fixtureServer
	reports chan media.PlaybackState
}

func (f trackFixtureServer) Playback(_ context.Context, item media.Item, options media.PlaybackOptions) (media.Stream, error) {
	stream := media.Stream{ItemID: item.ID, SourceID: "film", SessionID: "first", URL: firstTrackSource,
		Method: media.DirectPlay, Duration: time.Minute,
		Tracks: []media.Track{{ID: "1", Type: "audio", Title: "English", Selected: true},
			{ID: "2", Type: "audio", Title: "Japanese"}, {ID: "3", Type: "audio", Title: "Commentary"}}}
	if options.AudioIndex != nil {
		if options.SourceID != stream.SourceID || *options.AudioIndex != 2 {
			return media.Stream{}, media.ErrNoStream
		}
		stream.SessionID, stream.URL, stream.Method = "second", secondTrackSource, media.Transcode
		stream.Tracks[0].Selected = false
		stream.Tracks[1].Selected = true
	}
	return stream, nil
}

func (f trackFixtureServer) Report(_ context.Context, state media.PlaybackState) error {
	f.reports <- state
	return nil
}
