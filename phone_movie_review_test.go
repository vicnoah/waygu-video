package main

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerMovieFeatures(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	for _, id := range []string{"emby", "jellyfin", "plex"} {
		t.Run(id, func(t *testing.T) {
			client, movie := reviewFixtureItem(t, config, secrets, id, "Waygu Track Test")
			features := client.(media.MovieFeaturesClient)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			clips, err := features.Extras(ctx, movie)
			if err != nil {
				t.Fatal("read extras:", err)
			}
			if id != "plex" && len(clips) < 2 {
				t.Fatalf("expected the local trailer and featurette, got %d clips", len(clips))
			}
			for _, clip := range clips {
				stream, err := client.Playback(ctx, clip, media.PlaybackOptions{})
				if err != nil || stream.File.URL == "" || stream.ItemID != clip.ID || clip.ID == movie.ID {
					t.Fatalf("resolve extra %q: %v", clip.Title, err)
				}
			}
			if _, err := features.RelatedMovies(ctx, movie); err != nil {
				t.Fatal("read server recommendations:", err)
			}
		})
	}
}

func TestDockerPhoneMovieReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	client, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	recommendation := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Test Movie")
	resume := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Resume Test")
	features := client.(media.MovieFeaturesClient)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	clips, err := features.Extras(ctx, movie)
	if err != nil || len(clips) < 2 {
		t.Fatalf("read trailer and featurette: %v, %d items", err, len(clips))
	}
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(1960)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Idiom(core.IdiomPhone), nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.WaitFor("id:home-source-filter", 8*time.Second)
			app.Press("id:home-source-filter")
			app.Press("role:option Emby 影视库")
			app.WaitFor("id:title-emby-"+movie.ID, 10*time.Second)
			app.Press("id:title-emby-" + movie.ID)
			app.WaitFor("id:phone-movie-hero", 5*time.Second)
			app.WaitFor("id:detail-download", 5*time.Second)
			app.WaitFor("id:detail-audio", 5*time.Second)
			app.WaitFor("id:detail-subtitle", 5*time.Second)
			app.WaitFor("id:detail-extra-"+clips[0].ID, 8*time.Second)
			app.WaitFor("id:detail-related", 8*time.Second)
			if height := app.Find("id:phone-movie-hero").Frame.Size.Height; height != 460 {
				t.Fatalf("hero height = %v, want 460", height)
			}
			if height := app.Find("id:detail-play").Frame.Size.Height; height != 52 {
				t.Fatalf("play action height = %v, want 52", height)
			}
			if nsgotest.Target() != "" {
				app.Advance(time.Second)
				app.Screenshot("movie-detail-" + variant.name)
				if app.Find("id:phone-movie-scroll").Frame.Size.Height < 1960 {
					app.Scroll("id:phone-movie-scroll", 900)
					app.Advance(500 * time.Millisecond)
					app.Screenshot("movie-detail-" + variant.name + "-bottom")
					app.Scroll("id:phone-movie-scroll", -900)
					app.Advance(500 * time.Millisecond)
				}
			}
			app.Press("id:detail-cast-all")
			app.WaitFor("id:detail-cast-list", time.Second)
			app.Advance(350 * time.Millisecond)
			app.Press("id:detail-close-cast")
			app.Advance(350 * time.Millisecond)
			app.WaitForGone("id:detail-cast-list", 2*time.Second)
			app.Press("id:detail-danmaku")
			app.WaitFor("id:danmaku-search", 2*time.Second)
			app.Advance(350 * time.Millisecond)
			app.Press("id:detail-close-danmaku")
			app.Advance(350 * time.Millisecond)
			app.WaitForGone("id:detail-danmaku-dialog", 2*time.Second)
			selectTrack := func(control, contains string) {
				t.Helper()
				app.Press("id:" + control)
				option := "role:option /" + regexp.QuoteMeta(contains) + "/"
				label := app.WaitFor(option, time.Second).Props.Label
				app.Press(option)
				if value := app.Find("id:" + control + "-value").Text; value != label {
					t.Fatalf("%s = %q, want %q", control, value, label)
				}
			}
			selectTrack("detail-audio", "Japanese")
			selectTrack("detail-subtitle", "外挂")
			app.Press("id:detail-extra-" + clips[0].ID)
			app.WaitFor("id:video-player", 12*time.Second)
			if nsgotest.Target() != "" {
				app.WaitForGone("text:正在加载…", 10*time.Second)
				before := app.Find("id:player-position").Text
				app.WaitUntil(6*time.Second, func() bool { return app.Find("id:player-position").Text != before })
			}
			app.Press("id:close-player")
			app.WaitFor("id:phone-movie-hero", 5*time.Second)
			app.WaitFor("id:detail-related", 8*time.Second)
			app.Press("id:title-emby-" + recommendation)
			app.WaitUntil(3*time.Second, func() bool { return app.Find("id:phone-movie-title").Text == "Waygu Test Movie" })
			app.Press("id:back")
			app.WaitUntil(3*time.Second, func() bool { return app.Find("id:phone-movie-title").Text == movie.Title })
			app.Press("id:back")
			if variant.appearance != core.AppearanceLight {
				return
			}
			app.WaitFor("id:title-emby-"+resume, 5*time.Second)
			app.Press("id:title-emby-" + resume)
			app.WaitFor("role:button /继续播放/", 5*time.Second)
			app.LongPress("id:detail-play")
			app.WaitFor("id:detail-play-menu", 3*time.Second)
			app.Advance(350 * time.Millisecond)
			if nsgotest.Target() != "" {
				app.Screenshot("movie-detail-resume")
			}
			app.Press("id:detail-from-start")
			app.WaitFor("id:video-player", 8*time.Second)
			if nsgotest.Target() == "" {
				app.Play("id:video-player", core.VideoInfo{Duration: 6 * time.Minute})
				if got := app.Find("id:player-position").Text; got != "00:00" {
					t.Fatalf("play from beginning started at %q", got)
				}
			}
		})
	}
}
