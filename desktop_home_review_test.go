package main

import (
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDockerDesktopHomeReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("desktop design capture runs on Linux GTK")
	}
	config, secrets := homeReviewSources(t)
	embyMovie := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Track Test")
	jellyfinMovie := reviewFixtureTitle(t, config, secrets, "jellyfin", "Waygu Track Test")
	plexMovie := reviewFixtureTitle(t, config, secrets, "plex", "Waygu Track Test")
	for _, theme := range []struct {
		name       string
		appearance core.Appearance
	}{{"light", core.AppearanceLight}, {"dark", core.AppearanceDark}} {
		t.Run(theme.name, func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomDesktop)
				cx.SetSubtreeAppearance(theme.appearance)
				return Root(cx)
			}, nsgotest.Size(1440, 1500), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.WaitFor("id:home-hero", 12*time.Second)
			if got := app.Find("id:desktop-sidebar").Frame.Size.Width; got != 240 {
				t.Fatalf("sidebar is %v pt wide, want 240", got)
			}
			if got := app.Find("id:home-hero").Frame.Size.Height; got != 380 {
				t.Fatalf("hero is %v pt tall, want 380", got)
			}
			app.WaitFor("id:desktop-resume-rail", 4*time.Second)
			app.WaitFor("id:desktop-recent-rail", 4*time.Second)
			app.WaitFor("id:desktop-series-rail", 4*time.Second)
			app.Find("id:desktop-home-search >> text:搜索电影、剧集、演员")
			app.Find("id:tab-search >> text:搜索影片、演员")
			if nsgotest.Target() != "" {
				app.Screenshot("app-desktop-main-" + theme.name)
				if theme.name == "light" {
					app.Resize(1440, 900)
					app.Screenshot("app-desktop-main-window")
					app.Scroll("id:desktop-home-scroll", 780)
					app.Screenshot("app-desktop-main-scrolled")
					app.Scroll("id:desktop-home-scroll", -780)
					app.Resize(1440, 1500)
				}
			}
			app.Press("id:home-hero-dot-1")
			app.WaitUntil(time.Second, func() bool { return app.Find("id:home-hero-dot-1").Props.Selected })
			app.FastForward(8100 * time.Millisecond)
			app.WaitUntil(time.Second, func() bool { return app.Find("id:home-hero-dot-2").Props.Selected })
			app.Press("id:desktop-sidebar-close")
			app.WaitForGone("id:desktop-sidebar", 2*time.Second)
			app.Press("id:desktop-sidebar-open")
			app.WaitFor("id:desktop-sidebar", 2*time.Second)
			app.Press("id:desktop-home-search")
			app.WaitForGone("id:desktop-home", 2*time.Second)
			if theme.name == "light" {
				app.Type("id:search-query", "Mei Zhou")
				app.Press("id:search-submit")
				app.WaitFor("id:title-emby-"+embyMovie, 8*time.Second)
				if nsgotest.Target() == "linux" {
					app.Screenshot("app-desktop-search-actor")
				}
				app.Press("id:catalog-source")
				app.Press("role:option Jellyfin 影视库")
				app.WaitFor("id:title-jellyfin-"+jellyfinMovie, 8*time.Second)
				app.WaitForGone("id:title-emby-"+embyMovie, 3*time.Second)
				app.Clear("id:search-query")
				app.WaitForGone("id:title-jellyfin-"+jellyfinMovie, 2*time.Second)
				app.Type("id:search-query", "Waygu Track Test")
				app.Press("id:search-submit")
				app.WaitFor("id:title-jellyfin-"+jellyfinMovie, 8*time.Second)
				app.Press("id:catalog-source")
				app.Press("role:option Plex 影视库")
				app.WaitFor("id:title-plex-"+plexMovie, 8*time.Second)
				app.WaitForGone("id:title-jellyfin-"+jellyfinMovie, 3*time.Second)
			}
			app.Press("id:tab-home")
			app.WaitFor("id:desktop-home", 2*time.Second)
			app.Press("id:tablet-source-jellyfin")
			app.WaitUntil(6*time.Second, func() bool {
				return app.Exists("id:home-hero-source") && app.Find("id:home-hero-source").Text == "Jellyfin 影视库"
			})
		})
	}
}
