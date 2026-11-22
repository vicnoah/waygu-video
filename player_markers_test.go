package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestMarkerSkipHonorsManualSeekAndRejectsLateProgress(t *testing.T) {
	now := time.Now()
	opening := media.PlaybackMarker{Kind: media.MarkerIntro, Start: 2 * time.Second, End: 5 * time.Second}
	markers := []media.PlaybackMarker{opening}
	skip := markerSkip{manual: -1}
	skip.manualSeek(3 * time.Second)
	if at, _ := skip.next(markers, 3*time.Second, time.Minute, now); at != 0 {
		t.Fatal("automatic skip superseded a manual seek into the opening")
	}
	skip.manualSeek(0)
	if at, final := skip.next(markers, 2*time.Second, time.Minute, now); at != 5*time.Second || final {
		t.Fatal("normal playback did not skip the opening")
	}
	if !skip.pending(3*time.Second, now.Add(50*time.Millisecond)) || skip.pending(5*time.Second, now.Add(100*time.Millisecond)) {
		t.Fatal("decoder progress did not settle at the seek target")
	}
	if at, _ := skip.next(markers, 2*time.Second, time.Minute, now); at != 0 {
		t.Fatal("rewinding consumed the same opening a second time")
	}
}

func TestDockerPhonePlaybackMarkers(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("native playback review runs in the local GTK harness")
	}
	config, secrets := homeReviewSources(t)
	entry, client, series, seasons := reviewSeriesFixture(t, config, secrets, "emby")
	_, episodes := seriesFixtureEpisodes(t, t.Context(), client, seasons, 1, 5)
	episode := episodes[2]
	watched := client.(media.WatchedClient)
	if err := watched.SetWatched(t.Context(), episode, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = watched.SetWatched(ctx, episode, false)
	})
	catalog, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(catalog)
	if target == "linux" {
		config[autoNextKey] = "false"
	}
	height := core.Points(1450)
	if target == "linux" {
		height += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:tab-search")
	app.Type("id:search-query", series.Title)
	app.Press("id:search-submit")
	seriesCard := "id:search-series >> id:title-" + series.ID
	app.WaitFor(seriesCard, 8*time.Second)
	app.Press(seriesCard)
	row := "id:child-" + episode.ID
	app.WaitFor(row, 5*time.Second)
	app.Press(row)
	app.Resize(844, 390)

	const video = "id:video-player"
	const length = 18 * time.Second
	app.WaitFor(video, 8*time.Second)
	if target == "linux" {
		app.WaitUntil(5*time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:01" })
		app.WaitUntil(3*time.Second, func() bool { return app.Find("id:player-position").Text >= "00:00:05" })
		app.WaitUntil(12*time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:18" })
		app.WaitFor("id:play-next-episode", 3*time.Second)
		app.Screenshot("player-after-final-credits")
		app.Press("id:close-player")
		return
	}

	app.Play(video, core.VideoInfo{Duration: length})
	app.WaitUntil(6*time.Second, func() bool {
		app.Progress(video, 2250*time.Millisecond, length)
		return app.Find("id:player-position").Text == "00:00:05"
	})
	app.Progress(video, 2500*time.Millisecond, length)
	if app.Find("id:player-position").Text != "00:00:05" {
		t.Fatal("a stale decoder position moved the timeline back into the intro")
	}
	app.Progress(video, 6*time.Second, length)
	app.Progress(video, 14250*time.Millisecond, length)
	seeks := app.FindMounted(video).Seeks
	if len(seeks) != 2 || seeks[0] != 5*time.Second || seeks[1] != 15500*time.Millisecond {
		t.Fatalf("skipping did not preserve the post-credits scene: %v", seeks)
	}
	app.Progress(video, 16*time.Second, length)
	if app.FindMounted(video).Video.Paused {
		t.Fatal("mid-credits skip ended playback")
	}
	app.Progress(video, 16750*time.Millisecond, length)
	app.WaitFor("id:play-next-episode", 3*time.Second)
	if !app.FindMounted(video).Video.Paused || app.Find("id:player-position").Text != "00:00:18" {
		t.Fatal("final credits did not end playback at the actual duration")
	}
	app.WaitUntil(4*time.Second, func() bool {
		item, err := client.Detail(t.Context(), episode)
		return err == nil && item.Watched
	})
	app.FastForward(10 * time.Second)
	app.WaitFor("id:player-title", 3*time.Second)
	if title := app.Find("id:player-title").Text; title != episodes[3].Title {
		t.Fatalf("next episode after final credits = %q, want %q", title, episodes[3].Title)
	}
	app.Press("id:close-player")
}
