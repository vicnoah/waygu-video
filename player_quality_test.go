package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
	"waygu-video/internal/playback"
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
	if options.SourceID != "small" || options.AudioIndex == nil || *options.AudioIndex != 2 ||
		options.MaxBitrate != 4_000_000 || options.MaxHeight != 480 {
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

func TestDockerPlaybackHeightLimit(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media servers")
	}
	config, secrets := homeReviewSources(t)
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			client, movie := reviewFixtureItem(t, config, secrets, provider, "Waygu Test Movie")
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
			defer cancel()
			const height = 180
			original, err := client.Playback(ctx, movie, media.PlaybackOptions{})
			if err != nil || original.Rendition.Height <= height {
				t.Fatalf("fixture should exceed %dp in original quality, got %dp, %v", height, original.Rendition.Height, err)
			}
			limited, err := client.Playback(ctx, movie, media.PlaybackOptions{MaxHeight: height, MaxBitrate: 4_000_000})
			if err != nil || limited.Method != media.Transcode || limited.VersionID() != original.VersionID() {
				t.Fatalf("expected a transcode of the original version, got %s, %v", limited.Method, err)
			}
			address, stop, err := playback.Open(limited.Resource())
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			defer func() {
				if err := client.Report(ctx, media.PlaybackState{Event: media.Stopped, Stream: limited, Position: original.Position}); err != nil {
					t.Errorf("stop limited playback: %v", err)
				}
			}()
			raw, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
				"-show_entries", "stream=height", "-of", "json", address).Output()
			if err != nil {
				t.Fatalf("decode the limited stream: %v", err)
			}
			var measured struct {
				Streams []struct{ Height int }
			}
			if err := json.Unmarshal(raw, &measured); err != nil || len(measured.Streams) != 1 ||
				measured.Streams[0].Height <= 0 || measured.Streams[0].Height > height {
				t.Fatalf("expected a decodable stream of at most %dp, got %+v, %v", height, measured, err)
			}
			t.Logf("decoded %dp from the %dp original", measured.Streams[0].Height, original.Rendition.Height)
		})
	}
}

func TestDockerPreferredPlaybackMethod(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || nsgotest.Target() != "" {
		t.Skip("requires the Docker media servers and the simulated playback surface")
	}
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			for _, mode := range []struct {
				name, preferences, container string
			}{
				{"direct", "", "mp4"},
				{"transcode", `{"DisableDirectPlay":true}`, "hls"},
			} {
				t.Run(mode.name, func(t *testing.T) {
					config, secrets := dockerReviewSources(t, false)
					_, item := reviewFixtureItem(t, config, secrets, provider, "Waygu Test Movie")
					config[playbackPreferencesKey] = mode.preferences
					app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
						shellIdiom.Provide(cx, core.IdiomPhone)
						return Root(cx)
					}, nsgotest.Size(390, 800), nsgotest.Offscreen(),
						nsgotest.Storage(config), nsgotest.Secrets(secrets))
					app.Press("id:tab-search")
					app.Type("id:search-query", item.Title)
					app.Press("id:search-source-" + provider)
					result := "id:title-" + provider + "-" + item.ID
					app.WaitFor(result, 10*time.Second)
					app.Press(result)
					app.WaitFor("id:detail-play", 10*time.Second)
					app.Press("id:detail-play")
					app.WaitFor("id:video-player", 15*time.Second)
					video := app.FindMounted("id:video-player").Video
					ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
					defer cancel()
					raw, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
						"-read_intervals", "%+1", "-show_frames",
						"-show_entries", "format=format_name:stream=codec_name:frame=key_frame", "-of", "json", video.Source).Output()
					if err != nil {
						t.Fatalf("probe the player's stream: %v", err)
					}
					var measured struct {
						Format struct {
							Name string `json:"format_name"`
						}
						Streams []struct {
							Codec string `json:"codec_name"`
						}
						Frames []struct{}
					}
					if err := json.Unmarshal(raw, &measured); err != nil || len(measured.Streams) != 1 || len(measured.Frames) == 0 ||
						measured.Streams[0].Codec != "h264" || !slices.Contains(strings.Split(measured.Format.Name, ","), mode.container) {
						t.Fatalf("wanted H.264 in %s, got %s (%v)", mode.container, raw, err)
					}
					app.Play("id:video-player", core.VideoInfo{Duration: item.Duration})
					app.Press("id:close-player")
					t.Logf("decoded H.264 in %s", mode.container)
				})
			}
		})
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
