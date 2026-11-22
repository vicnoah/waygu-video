package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDockerTabletHomeReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	start := func(t *testing.T, appearance core.Appearance, width, height core.Points) *nsgotest.App {
		t.Helper()
		if nsgotest.Target() == "linux" {
			height += 37
		}
		return nsgotest.Start(t, func(cx *core.Scope) core.Node {
			cx.SetSubtreeAppearance(appearance)
			shellIdiom.Provide(cx, core.IdiomTablet)
			return Root(cx)
		}, nsgotest.Size(width, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
			nsgotest.Storage(config), nsgotest.Secrets(secrets))
	}
	allSources := func(app *nsgotest.App) {
		app.WaitUntil(5*time.Second, func() bool {
			return strings.HasPrefix(app.Find("id:home-summary").Text, "3 个媒体源在线")
		})
	}
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			app := start(t, variant.appearance, 1180, 820)
			app.WaitFor("text:最近添加", 10*time.Second)
			app.WaitFor("text:继续观看", 5*time.Second)
			allSources(app)
			if nsgotest.Target() != "" {
				app.Advance(500 * time.Millisecond)
				app.Screenshot("tablet-home-" + variant.name)
			}
			if width := app.Find("id:tablet-sidebar").Frame.Size.Width; width != 224 {
				t.Errorf("tablet sidebar width = %v, want 224", width)
			}
			if height := app.Find("id:home-hero").Frame.Size.Height; height != 260 {
				t.Errorf("tablet hero height = %v, want 260", height)
			}
			app.DragAt("id:home-hero", core.Point{X: 480, Y: 100}, core.Point{X: -100})
			if !app.Find("id:home-hero-dot-1").Props.Selected {
				t.Fatal("swiping left did not select the next feature")
			}
			app.DragAt("id:home-hero", core.Point{X: 480, Y: 100}, core.Point{X: 100})
			if !app.Find("id:home-hero-dot-0").Props.Selected {
				t.Fatal("swiping right did not select the previous feature")
			}
			app.Press("id:home-hero-dot-1")
			if !app.Find("id:home-hero-dot-1").Props.Selected {
				t.Fatal("pressing a dot did not select the next feature")
			}
			app.Press("id:home-recent-all")
			app.WaitFor("text:收起", time.Second)
			app.Press("id:home-recent-all")
			app.WaitFor("id:home-recent-rail", time.Second)
			app.Press("id:tablet-source-emby")
			app.WaitFor("id:home-hero-source", 5*time.Second)
			if name := app.Find("id:home-hero-source").Text; name != "Emby 影视库" {
				t.Fatalf("featured source after selecting Emby = %q", name)
			}
			app.Press("id:home-source-filter")
			app.Press("role:option 全部媒体源")
			allSources(app)
			app.Press("id:tablet-library-movies")
			app.WaitFor("id:catalog-category", time.Second)
			if !app.Find("id:tablet-library-movies").Props.Selected {
				t.Fatal("the movies shortcut did not select the category")
			}
			app.Press("id:tablet-library-series")
			if !app.Find("id:tablet-library-series").Props.Selected {
				t.Fatal("the series shortcut did not change the library category")
			}
			app.Press("id:tab-search")
			app.WaitFor("id:search-query", time.Second)
			app.Press("id:tab-home")
			app.Key("mod+k")
			app.WaitFor("id:search-query", time.Second)
			app.Press("id:tab-home")
			app.WaitFor("id:home-hero-details", 5*time.Second)
			app.Press("id:home-hero-details")
			app.WaitFor("id:detail-play", 5*time.Second)
			app.Press("id:back")
			app.WaitFor("id:home-hero-play", 5*time.Second)
			app.Press("id:home-hero-play")
			app.WaitFor("id:video-player", 5*time.Second)
			app.Press("id:close-player")
			app.WaitFor("id:tablet-sidebar-close", time.Second)
			app.Press("id:tablet-sidebar-close")
			app.WaitForGone("id:tablet-sidebar", time.Second)
			app.Press("id:tablet-sidebar-open")
			app.WaitFor("id:tablet-sidebar", time.Second)
		})
	}
	t.Run("portrait", func(t *testing.T) {
		app := start(t, core.AppearanceLight, 820, 1180)
		app.WaitFor("id:home-hero", 10*time.Second)
		app.WaitFor("id:tablet-sidebar-open", time.Second)
		app.Drag("id:tablet-drawer-edge", core.Point{X: 100})
		app.WaitFor("id:tablet-drawer", time.Second)
		if nsgotest.Target() != "" {
			app.Advance(250 * time.Millisecond)
			app.Screenshot("tablet-home-portrait-drawer")
		}
		app.Press("id:tablet-drawer-backdrop")
		app.WaitForGone("id:tablet-drawer", time.Second)
		app.Press("id:tablet-sidebar-open")
		app.Press("id:tab-settings")
		app.WaitForGone("id:tablet-drawer", time.Second)
		app.WaitFor("id:bitrate-limit", time.Second)
		app.Press("id:tablet-sidebar-open")
		app.Press("id:tablet-sidebar-close")
		app.WaitForGone("id:tablet-drawer", time.Second)
		app.Press("id:tablet-sidebar-open")
		app.DragAt("id:tablet-sidebar", core.Point{X: 180, Y: 300}, core.Point{X: -100})
		app.WaitForGone("id:tablet-drawer", time.Second)
	})
}
