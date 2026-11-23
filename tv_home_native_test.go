package main

import (
	"bytes"
	"context"
	_ "embed"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
	"waygu-video/internal/tvpair"
)

//go:embed design/assets/posters/hero.jpg
var tvReviewArtwork []byte

func TestTVHomeNativeFixture(t *testing.T) {
	target := nsgotest.Target()
	if target != "" && target != "tvos_simulator" && target != "android" && target != "linux" {
		t.Skip("requires the TV focus layout")
	}
	var clipReads, reports atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/poster.jpg":
			http.ServeContent(w, r, "poster.jpg", time.Time{}, bytes.NewReader(tvReviewArtwork))
		case "/trailer.mp4":
			if r.Header.Get("X-Waygu-Fixture") != "trailer" {
				http.Error(w, "missing trailer authorization", http.StatusForbidden)
				return
			}
			clipReads.Add(1)
			http.ServeContent(w, r, "trailer.mp4", time.Time{}, bytes.NewReader(backgroundAudioFixture))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	library := sourceLibrary{Library: media.Library{ID: "movies", Name: "Movies"}}
	sources := []source{
		{ID: "emby", Provider: media.Emby, Address: server.URL, Name: "Emby", Libraries: []sourceLibrary{library}},
		{ID: "jellyfin", Provider: media.Jellyfin, Address: server.URL, Name: "Jellyfin", Libraries: []sourceLibrary{library}},
		{ID: "plex", Provider: media.Plex, Address: server.URL, Name: "Plex", Libraries: []sourceLibrary{library}},
	}
	storedSources := downloadFixtureJSON(t, sources)
	cfg := map[string]string{sourcesKey: storedSources, appearanceKey: "dark"}
	secrets := map[string]string{"emby": "fixture-token", "jellyfin": "fixture-token"}
	factory := func(provider media.Provider, address string, _ media.Device, session media.Session) (media.Client, error) {
		if address != server.URL || (provider != media.Plex && session.Token != "fixture-token") {
			return nil, media.ErrAuthentication
		}
		title, rating := "The Dune Archive", 9.0
		if provider == media.Jellyfin {
			title, rating = "An Autumn Journey", 8
		} else if provider == media.Plex {
			title, rating = "The Night Train", 7
		}
		return tvHomeNativeFixture{
			item: media.Item{ID: string(provider), Kind: media.Movie, Title: title, LibraryID: library.ID,
				Year: 2025, Rating: rating, Overview: "A courier crosses the dunes to bring a lost archive home.",
				Duration: 2 * time.Hour, Poster: address + "/poster.jpg", Backdrop: address + "/poster.jpg"},
			address: address, reports: &reports,
		}, nil
	}

	height := core.Points(1080)
	env := core.Environment{Locale: "zh-Hans"}
	switch target {
	case "linux":
		height += 37
	case "", "tvos_simulator":
		// The test model uses these values to recognize remote focus commands.
		env.OS, env.Idiom = core.OSIOS, core.IdiomTV
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomTV)
		tvPairListener.Provide(cx, func(ctx context.Context, apply func(context.Context, tvpair.Setup) error) (tvpair.Target, error) {
			return tvpair.Listen(ctx, netip.MustParseAddr("127.0.0.1"), apply)
		})
		return appRoot(cx, factory, nil)
	}, nsgotest.Idiom(core.IdiomTV), nsgotest.Size(1920, height), nsgotest.Environment(env),
		nsgotest.Storage(cfg), nsgotest.Secrets(secrets))

	if target == "tvos_simulator" {
		t.Cleanup(func() {
			if t.Failed() {
				app.Screenshot("tv-home-test-failure")
			}
		})
	}
	expectFocus := func(id string) {
		t.Helper()
		selector := "id:" + id
		app.WaitUntil(3*time.Second, func() bool { return app.Exists(selector) && app.Find(selector).Focused })
	}
	if title := app.WaitFor("id:home-hero-title", 10*time.Second).Text; title != "The Dune Archive" {
		t.Fatalf("featured title = %q, want The Dune Archive", title)
	}
	if target == "" {
		app.Key("tab")
	} else if target == "linux" && !app.Find("id:home-hero-play").Focused {
		app.Focus("id:home-hero-play")
	}
	expectFocus("home-hero-play")
	if target == "tvos_simulator" {
		// Wait for the app to style its native focus before advancing its clock.
		ring := core.Ring(4, core.RGB(0x8b5cf6))
		app.WaitUntil(3*time.Second, func() bool {
			effects := app.Find("id:home-hero-play").Style.Effects
			return effects != nil && slices.Contains(effects.Shadows, ring)
		})
	}
	app.Advance(tvTrailerDwell + 100*time.Millisecond)
	const preview = "id:tv-featured-trailer"
	video := app.WaitFor(preview, 5*time.Second).Video
	if !video.Muted || video.Controls || video.ResizeMode != core.Cover {
		t.Fatal("the featured trailer must be muted and fill the hero without player controls")
	}
	const artwork = "id:tv-featured-backdrop-artwork"
	if target == "" {
		app.Find(artwork)
		app.Play(preview, core.VideoInfo{Duration: 30 * time.Second})
		app.Progress(preview, 500*time.Millisecond, 30*time.Second)
	}
	app.WaitForGone(artwork, 5*time.Second)
	expectFocus("home-hero-play")
	if target != "" && clipReads.Load() == 0 {
		t.Fatal("the native player did not read the authorized trailer")
	}
	app.Screenshot("tv-home-trailer")
	captureTVReviewDisplay(t)
	app.Find(preview)
	if target != "linux" {
		app.Key("up")
	} else {
		app.Focus("id:tv-tab-home")
	}
	app.WaitForGone(preview, 3*time.Second)
	app.WaitFor(artwork, 3*time.Second)
	if target == "" || target == "tvos_simulator" {
		expectFocus("tv-tab-home")
	}
	if target == "tvos_simulator" {
		app.Screenshot("tv-home-trailer-exit")
	}

	app.Press("id:tv-tab-settings")
	app.Press("id:tv-settings-section-appearance")
	app.Press("id:tv-open-home-layout")
	expectFocus("tv-layout-module-continue")
	for range 6 {
		app.Key("down")
	}
	expectFocus("tv-layout-source-emby")
	app.Key("right")
	expectFocus("tv-layout-source-emby")
	app.Key("down")
	expectFocus("tv-layout-source-plex")
	app.Key("enter")
	want := []homeModule{{"jellyfin", true}, {"emby", true}, {"plex", false}}
	if got := readAppearancePreferences(app.Storage()).TVSourceShelves; !slices.Equal(got, want) {
		t.Fatalf("saved source rows = %v, want %v", got, want)
	}
	app.Screenshot("tv-home-layout")
	app.Key("escape")
	expectFocus("tv-open-home-layout")
	app.Key("escape")
	app.WaitFor("id:home-hero-play", 10*time.Second)
	app.WaitUntil(5*time.Second, func() bool {
		return len(app.QueryMounted("id:tv-source-jellyfin")) == 1 && len(app.QueryMounted("id:tv-source-emby")) == 1
	})
	jellyfin := app.FindMounted("id:tv-source-jellyfin").Frame.Origin.Y
	emby := app.FindMounted("id:tv-source-emby").Frame.Origin.Y
	if jellyfin >= emby || len(app.QueryMounted("id:tv-source-plex")) != 0 {
		t.Fatal("the Home screen should show Jellyfin before Emby and hide Plex")
	}
	if updated, _ := app.Storage().GetItem(sourcesKey); updated != storedSources {
		t.Fatal("the Home layout changed source playback priority")
	}
	if got := reports.Load(); got != 0 {
		t.Fatalf("the preview reported %d playback events", got)
	}
}

func captureTVReviewDisplay(t *testing.T) {
	t.Helper()
	if os.Getenv("WAYGU_CAPTURE_TV_DISPLAY") != "1" {
		return
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(cache, "waygu-tv-review-ready")
	if err := os.WriteFile(ready, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(ready) })
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); os.IsNotExist(err) {
			return
		}
		<-time.After(100 * time.Millisecond)
	}
	t.Fatal("the host did not capture the Apple TV display")
}

type tvHomeNativeFixture struct {
	fixtureServer
	item    media.Item
	address string
	reports *atomic.Int32
}

func (f tvHomeNativeFixture) Catalog(context.Context, media.Query) (media.Page, error) {
	return media.Page{Items: []media.Item{f.item}, Total: 1}, nil
}

func (f tvHomeNativeFixture) Extras(context.Context, media.Item) ([]media.Item, error) {
	return []media.Item{{ID: "interview", Kind: media.Video}, {ID: "trailer", Kind: media.Video, Trailer: true}}, nil
}

func (f tvHomeNativeFixture) Playback(_ context.Context, item media.Item, _ media.PlaybackOptions) (media.Stream, error) {
	if item.ID != "trailer" {
		return media.Stream{}, media.ErrNoStream
	}
	return media.Stream{ItemID: item.ID, Method: media.DirectPlay, URL: f.address + "/trailer.mp4",
		Headers: http.Header{"X-Waygu-Fixture": {"trailer"}}}, nil
}

func (f tvHomeNativeFixture) Report(context.Context, media.PlaybackState) error {
	f.reports.Add(1)
	return nil
}
