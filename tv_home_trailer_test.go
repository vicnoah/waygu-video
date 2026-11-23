package main

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDockerTVSilentTrailer(t *testing.T) {
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux" && target != "android") {
		t.Skip("requires the Docker media fixtures and a headless, GTK or Android TV host")
	}
	config, secrets := dockerReviewSources(t, false)
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	if target != "" {
		testTVSilentTrailer(t, "emby", target, sources, config, secrets)
		return
	}
	for _, provider := range []string{"emby", "jellyfin"} {
		t.Run(provider, func(t *testing.T) { testTVSilentTrailer(t, provider, target, sources, config, secrets) })
	}
}

func testTVSilentTrailer(t *testing.T, provider, target string, sources []source, config, secrets map[string]string) {
	t.Helper()
	index := slices.IndexFunc(sources, func(entry source) bool { return entry.ID == provider })
	if index < 0 {
		t.Fatalf("missing %s trailer fixture", provider)
	}
	config = maps.Clone(config)
	config[sourcesKey] = downloadFixtureJSON(t, sources[index:index+1])
	env := core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}
	height := core.Points(1080)
	if target == "linux" {
		height += 37
	} else if target == "android" {
		env.OS = core.OSAndroid
	}
	appearance := core.AppearanceDark
	if os.Getenv("WAYGU_REVIEW_APPEARANCE") == "light" {
		appearance = core.AppearanceLight
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomTV)
		cx.SetSubtreeAppearance(appearance)
		return Root(cx)
	}, nsgotest.Size(1920, height), nsgotest.Environment(env), nsgotest.Storage(config), nsgotest.Secrets(secrets))
	const trailer = "id:tv-featured-trailer"
	const artwork = "id:tv-featured-backdrop-artwork"
	title := app.WaitFor("id:home-hero-title", 12*time.Second)
	if title.Text != "Waygu Track Test" {
		t.Fatalf("featured title = %q, want the movie with a local trailer", title.Text)
	}
	if target == "" || (target == "linux" && !app.Find("id:home-hero-play").Focused) {
		app.Focus("id:home-hero-play")
	}
	app.WaitUntil(time.Second, func() bool { return app.Find("id:home-hero-play").Focused })
	app.Advance(tvTrailerDwell + 100*time.Millisecond)
	video := app.WaitFor(trailer, 5*time.Second)
	if !video.Video.Muted || video.Video.Controls || video.Video.ResizeMode != core.Cover {
		t.Fatal("the background trailer must be muted, cover the hero and have no controls")
	}
	if target == "" {
		app.Find(artwork)
		app.Play(trailer, core.VideoInfo{Duration: 18 * time.Second})
		app.Progress(trailer, 500*time.Millisecond, 18*time.Second)
	}
	app.WaitForGone(artwork, 3*time.Second)
	if !app.Find("id:home-hero-play").Focused {
		t.Fatal("the background trailer took focus from Play")
	}
	if target != "" {
		app.Screenshot("app-tv-home-muted-trailer")
	}
	if target == "android" {
		app.Key("up")
	} else {
		app.Focus("id:tv-tab-home")
	}
	app.WaitForGone(trailer, time.Second)
	app.WaitFor(artwork, time.Second)
	if target == "" {
		app.Focus("id:home-hero-play")
		app.WaitFor(trailer, 5*time.Second)
		env.Lifecycle = core.LifecycleBackground
		app.SetEnvironment(env)
		app.WaitForGone(trailer, time.Second)
		app.WaitFor(artwork, time.Second)
	}
}
