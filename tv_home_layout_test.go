package main

import (
	"context"
	"net/netip"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/tvpair"
)

func TestDockerTVHomeLayout(t *testing.T) {
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux" && target != "android") {
		t.Skip("requires the Docker fixtures and a headless, GTK or Android TV host")
	}
	config, secrets := dockerReviewSources(t, false)
	platform := core.OSLinux
	if target == "android" {
		platform = core.OSAndroid
	}
	for _, variant := range reviewAppearances {
		if target == "android" && variant.appearance != core.AppearanceDark {
			continue
		}
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(1080)
			if target == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				shellIdiom.Provide(cx, core.IdiomTV)
				tvPairListener.Provide(cx, func(ctx context.Context, apply func(context.Context, tvpair.Setup) error) (tvpair.Target, error) {
					return tvpair.Listen(ctx, netip.MustParseAddr("127.0.0.1"), apply)
				})
				return Root(cx)
			}, nsgotest.Size(1920, height), nsgotest.Environment(core.Environment{OS: platform, Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			expectFocus := func(id string) {
				t.Helper()
				app.WaitUntil(2*time.Second, func() bool {
					return app.Exists("id:"+id) && app.Find("id:"+id).Focused
				})
			}
			originalSources, _ := app.Storage().GetItem(sourcesKey)
			app.WaitFor("id:tv-tab-settings", 8*time.Second)
			app.Press("id:tv-tab-settings")
			app.WaitFor("id:tv-pair-code", 4*time.Second)
			expectFocus("tv-settings-section-sources")
			for range 4 {
				app.Key("down")
			}
			expectFocus("tv-settings-section-appearance")
			app.Key("right")
			expectFocus("tv-settings-appearance")
			app.Key("down")
			expectFocus("tv-open-home-layout")
			app.Key("enter")
			expectFocus("tv-layout-module-continue")
			app.Key("down")
			expectFocus("tv-layout-module-recent")
			app.Key("left")
			expectFocus("tv-layout-module-recent")
			if got := readAppearancePreferences(app.Storage()).Home[0].ID; got != homeRecent {
				t.Fatalf("first home module is %q, want %q", got, homeRecent)
			}
			for range 6 {
				app.Key("down")
			}
			expectFocus("tv-layout-source-emby")
			app.Key("right")
			expectFocus("tv-layout-source-emby")
			app.Key("down")
			expectFocus("tv-layout-source-plex")
			if !app.Find("id:tv-layout-source-plex").Props.Checked {
				t.Fatal("a new source shelf should be visible")
			}
			app.Key("enter")
			row := app.Find("id:tv-layout-source-plex")
			if row.Props.Checked {
				t.Fatal("Select did not turn the shelf switch off")
			}
			if want := row.Frame.Size.Height * 16 / 68; row.Style.CornerRadius < want-0.25 || row.Style.CornerRadius > want+0.25 {
				t.Fatalf("source row radius = %v, want %v at height %v", row.Style.CornerRadius, want, row.Frame.Size.Height)
			}
			wantShelves := []homeModule{{"jellyfin", true}, {"emby", true}, {"plex", false}}
			if got := readAppearancePreferences(app.Storage()).TVSourceShelves; !slices.Equal(got, wantShelves) {
				t.Fatalf("stored source shelves = %v, want %v", got, wantShelves)
			}
			if target != "" {
				app.Screenshot("app-tv-home-layout-" + variant.name)
			}
			app.Key("escape")
			expectFocus("tv-open-home-layout")
			app.Key("escape")
			app.WaitFor("id:home-hero-play", 20*time.Second)
			a := app.FindMounted("id:tv-source-jellyfin").Frame.Origin.Y
			b := app.FindMounted("id:tv-source-emby").Frame.Origin.Y
			if a >= b || len(app.QueryMounted("id:tv-source-plex")) != 0 {
				t.Fatalf("Home did not show Jellyfin before Emby and hide Plex")
			}
			if current, _ := app.Storage().GetItem(sourcesKey); current != originalSources {
				t.Fatal("the Home layout changed the configured sources")
			}
			if target == "linux" {
				app.Scroll("id:tv-home-scroll", 5000)
				app.Scroll("id:tv-home-scroll", -72)
				app.WaitFor("id:tv-source-emby", 3*time.Second)
				app.Advance(500 * time.Millisecond)
				app.Screenshot("app-tv-home-layout-" + variant.name + "-shelves")
			}
		})
	}
}
