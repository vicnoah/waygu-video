package main

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPhoneAppearanceReview(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("design comparisons use the Linux GTK backend")
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			height := core.Points(1700)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(theme.appearance)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(map[string]string{
					seenKey: "true", desktopPreferencesKey: `{"Language":"zh-Hans"}`, appearancePreferencesKey: `{"Night":true,"PosterAmbient":true}`,
				}))
			app.Press("id:tab-settings")
			app.Press("id:open-appearance-settings")
			screen := app.WaitFor("id:phone-appearance-settings", 2*time.Second)
			if nsgotest.Target() == "" {
				want := core.RGB(0xf4f4f7)
				if theme.appearance == core.AppearanceDark {
					want = core.RGB(0x0f1017)
				}
				if got := screen.Parent.Style.Background; got != want {
					t.Fatalf("appearance page background = %#x, want %#x", got, want)
				}
			}
			if nsgotest.Target() == "linux" {
				app.Advance(450 * time.Millisecond)
				app.Screenshot("settings-appearance-" + theme.name)
			}

			if theme.name == "light" {
				app.Press("id:appearance-night")
				app.Press("id:appearance-theme-dark")
				if value, _ := app.Storage().GetItem(appearanceKey); value != "dark" {
					t.Fatalf("appearance saved as %q", value)
				}
				app.Press("id:appearance-accent-emerald")
				app.Press("id:appearance-oled")
				app.Press("id:appearance-module-random")
				app.Drag("id:appearance-module-move-series", core.Point{Y: -105})
				app.Press("id:appearance-poster-style")
				app.WaitFor("role:option 横版剧照", time.Second)
				app.Press("role:option 横版剧照")
				prefs := readAppearancePreferences(app.Storage())
				if prefs.Night || prefs.Accent != "emerald" || !prefs.OLED || prefs.Poster != "landscape" || !prefs.enabled(homeRandom) ||
					prefs.Home[0].ID != homeSeries {
					t.Fatalf("appearance options = %+v", prefs)
				}
				app.Press("id:appearance-language")
				app.WaitFor("role:option English", time.Second)
				app.Press("role:option English")
				app.WaitFor("text:Appearance", 2*time.Second)
				if got := readDesktopPreferences(app.Storage()).Language; got != "en" {
					t.Fatalf("saved language = %q", got)
				}
			}
		})
	}
}

func TestAppearanceNightAndPosterColor(t *testing.T) {
	equinox := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC)
	equator := &solarLocation{0, 0}
	if !isAppearanceNight(equinox, equator) || isAppearanceNight(equinox.Add(12*time.Hour), equator) {
		t.Fatal("solar schedule does not distinguish midday and midnight")
	}
	pole := &solarLocation{89, 0}
	june := time.Date(2026, time.June, 21, 0, 0, 0, 0, time.UTC)
	if isAppearanceNight(june, pole) || !isAppearanceNight(june.AddDate(0, 6, 0), pole) {
		t.Fatal("solar schedule does not distinguish polar day and polar night")
	}
	if isAppearanceNight(equinox.Add(17*time.Hour), nil) || !isAppearanceNight(equinox.Add(18*time.Hour), nil) {
		t.Fatal("the schedule should use local hours when it has no approximate location")
	}
	poster := image.NewRGBA(image.Rect(0, 0, 80, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 80; x++ {
			poster.SetRGBA(x, y, color.RGBA{R: 24, G: 145, B: 184, A: 255})
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = png.Encode(w, poster) }))
	defer server.Close()
	got := sampleHomePoster(t.Context(), homeBackdrop{server.URL, server.URL + "/poster"})
	if want := core.RGB(0x1891b8); got != want {
		t.Fatalf("poster color = %#x, want %#x", got, want)
	}
	if other := sampleHomePoster(t.Context(), homeBackdrop{"http://localhost:1", server.URL}); other != core.Transparent {
		t.Fatalf("a poster at a different origin supplied color %#x", other)
	}
	a := application{}
	entry := source{Address: server.URL, HeaderSecret: "artwork-credential"}
	a.artworkSources([]source{entry})
	defer a.artworkSources(nil)
	mapped := a.proxyArtwork(entry.artworkAccess(server.URL), http.Header{"X-Artwork-Key": {"fixture"}}, nil)
	item := catalogItem{source: entry, item: media.Item{Poster: mapped(server.URL + "/poster")}}
	if color := sampleHomePoster(t.Context(), a.sourceBackdrop(item)); color != got {
		t.Fatalf("source-owned artwork proxy supplied color %#x, want %#x", color, got)
	}
	item.source.HeaderSecret = "different-artwork-credential"
	if color := sampleHomePoster(t.Context(), a.sourceBackdrop(item)); color != core.Transparent {
		t.Fatalf("another source's artwork proxy supplied color %#x", color)
	}
}

func TestAppearanceNightResolvesRunningTheme(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses the test clock to cross the night boundary")
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.WallTime(time.Date(2026, 11, 22, 17, 59, 56, 0, time.UTC)),
		nsgotest.Storage(map[string]string{seenKey: "true", appearanceKey: "light",
			appearancePreferencesKey: `{"Night":true,"OLED":true}`}))
	background := func() core.Color { return app.Find("id:phone-home").Style.Background }
	if got := background(); got != core.RGB(0xf4f4f7) {
		t.Fatalf("home before sunset = %#x", got)
	}
	app.FastForward(5 * time.Second)
	if got := background(); got != core.RGB(0x000000) {
		t.Fatalf("home after sunset = %#x, want black", got)
	}
}

func TestDockerHomeAppearance(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the local Docker media fixtures")
	}
	if nsgotest.Target() != "" && nsgotest.Target() != "linux" {
		t.Skip("fixture endpoints run on the local Linux test host")
	}
	config, secrets := homeReviewSources(t)
	client, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	favorite := client.(media.FavoriteClient)
	details, err := client.Detail(t.Context(), movie)
	if err != nil {
		t.Fatal(err)
	}
	if err := favorite.SetFavorite(t.Context(), movie, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := favorite.SetFavorite(ctx, movie, details.Favorite); err != nil {
			t.Error(err)
		}
	})
	config[appearancePreferencesKey] = `{"PosterAmbient":true}`
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, 1700), nsgotest.Environment(core.Environment{Locale: "en"}), nsgotest.Storage(config), nsgotest.Secrets(secrets))

	app.WaitFor("id:home-favorites-rail", 8*time.Second)
	app.WaitFor("id:home-resume-rail", 8*time.Second)
	app.Press("id:tab-settings")
	app.Press("id:open-appearance-settings")
	app.Press("id:appearance-module-continue")
	app.Press("id:appearance-module-sources")
	app.Press("id:appearance-module-random")
	app.Press("id:appearance-quality")
	app.Press("id:appearance-poster-style")
	app.WaitFor("role:option Landscape stills", 2*time.Second)
	app.Press("role:option Landscape stills")
	app.Press("id:back")
	app.Press("id:tab-home")
	app.WaitForGone("id:home-resume-rail", 2*time.Second)
	app.WaitFor("id:home-group-emby-rail", 6*time.Second)
	app.WaitFor("id:home-random-rail", 2*time.Second)
	app.Press("id:tab-library")
	id := "id:library-poster-emby-" + movie.ID
	cover := app.WaitFor(id, 8*time.Second).Frame.Size
	if cover.Width < 160 || cover.Width/cover.Height < 1.6 {
		t.Fatalf("landscape cover = %+v", cover)
	}
	app.WaitForGone("id:library-quality-emby-"+movie.ID, time.Second)
}
