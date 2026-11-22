package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

const audioFixture = "Waygu Audio Marker Series"

func TestDockerInferredPlaybackMarkers(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			if provider == "plex" && desktopAudioDecoder() == "" {
				t.Skip("Plex fallback requires desktop FFmpeg")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 28*time.Second)
			defer cancel()
			entry, client, _, seasons := reviewNamedSeriesFixture(t, config, secrets, provider, audioFixture)
			_, episodes := seriesFixtureEpisodes(t, ctx, client, seasons, 1, 3)
			episode := episodes[0]
			stream, err := client.Playback(ctx, episode, media.PlaybackOptions{})
			if err != nil {
				t.Fatal(err)
			}
			known, err := client.(media.MarkerClient).PlaybackMarkers(ctx, episode, stream)
			if err != nil || len(known) != 0 {
				t.Fatalf("the fingerprint-only fixture returned server markers: %v, %v", known, err)
			}
			var app application
			got := app.inferredMarkers(ctx, route{source: entry, item: episode}, client,
				audioMarkerRequest{key: sourceForMarkers(stream), stream: stream, kinds: []media.MarkerKind{media.MarkerIntro, media.MarkerCredits}})
			want := []media.PlaybackMarker{
				{Kind: media.MarkerIntro, Start: 4 * time.Second, End: 13 * time.Second},
				{Kind: media.MarkerCredits, Start: 39 * time.Second, End: 48 * time.Second},
			}
			if len(got) != len(want) {
				t.Fatalf("inferred markers: %+v, want %+v", got, want)
			}
			for i, actual := range got {
				expected := want[i]
				if actual.Kind != expected.Kind || durationDifference(actual.Start, expected.Start) > 375*time.Millisecond ||
					durationDifference(actual.End, expected.End) > 375*time.Millisecond {
					t.Fatalf("marker %d: %+v, want %+v", i, actual, expected)
				}
			}
		})
	}
}

func durationDifference(a, b time.Duration) time.Duration { return max(a-b, b-a) }

func TestDockerPhoneUsesInferredPlaybackMarkers(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("the native capture runs on Linux GTK")
	}
	config, secrets := homeReviewSources(t)
	entry, client, series, seasons := reviewNamedSeriesFixture(t, config, secrets, "jellyfin", audioFixture)
	_, episodes := seriesFixtureEpisodes(t, t.Context(), client, seasons, 1, 3)
	watched := client.(media.WatchedClient)
	if err := watched.SetWatched(t.Context(), episodes[0], false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = watched.SetWatched(ctx, episodes[0], false)
	})
	catalog, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(catalog)
	config[autoNextKey] = "false"
	config[playbackPreferencesKey] = `{"NextSeconds":15}`
	height := core.Points(1450)
	if target == "linux" {
		height += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}), nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:tab-search")
	app.Type("id:search-query", series.Title)
	app.Press("id:search-submit")
	show := "id:search-series >> id:title-" + series.ID
	app.WaitFor(show, 8*time.Second)
	app.Press(show)
	row := "id:child-" + episodes[0].ID
	app.WaitFor(row, 5*time.Second)
	app.Press(row)
	app.Resize(844, 390)
	const video = "id:video-player"
	const length = 48 * time.Second
	app.WaitFor(video, 8*time.Second)
	if target == "linux" {
		app.WaitUntil(6*time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:02" })
		app.WaitUntil(5*time.Second, func() bool { return app.Find("id:player-position").Text >= "00:00:12" })
		app.Press("id:player-seek-forward")
		app.WaitUntil(16*time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:48" })
		app.WaitFor("id:play-next-episode", 3*time.Second)
		app.Screenshot("player-fingerprinted-credits")
		app.Press("id:close-player")
		return
	}
	app.Play(video, core.VideoInfo{Duration: length})
	app.WaitUntil(12*time.Second, func() bool {
		app.Progress(video, 5*time.Second, length)
		return slices.ContainsFunc(app.FindMounted(video).Seeks, func(at time.Duration) bool {
			return durationDifference(at, 13*time.Second) <= 375*time.Millisecond
		})
	})
	app.Progress(video, 14*time.Second, length)
	app.Progress(video, 40*time.Second, length)
	app.WaitFor("id:play-next-episode", 3*time.Second)
	if position := app.Find("id:player-position").Text; position != "00:00:48" {
		t.Fatalf("fingerprinted credits left the player at %s", position)
	}
	app.Press("id:close-player")
}
