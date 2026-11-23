package main

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
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
			for _, category := range []libraryCategory{libraryMovies, librarySeries, libraryAnime, libraryDocumentaries} {
				app.WaitFor("id:tablet-category-count-"+string(category), 8*time.Second)
			}
			if nsgotest.Target() != "" {
				app.Advance(500 * time.Millisecond)
				app.Screenshot("tablet-home-" + variant.name)
			} else {
				want := core.StatusBarDark
				if variant.appearance == core.AppearanceDark {
					want = core.StatusBarLight
				}
				if got := app.StatusBar.Style; got != want {
					t.Errorf("status bar foreground = %v, want %v", got, want)
				}
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
		physical := nsgotest.Target() == "android" && os.Getenv("WAYGU_TEST_ANDROID_TABLET_EDGE") == "1"
		if physical {
			app.Screenshot("tablet-home-portrait-closed")
			edge := app.Find("id:tablet-drawer-edge").Bounds()
			if edge.Size.Width != 12 || edge.Size.Height != 192 {
				t.Fatalf("Android drawer edge = %v, want 12 × 192", edge)
			}
			fmt.Printf("waygu-tablet-edge %d\n", int(edge.Origin.Y+edge.Size.Height/2))
			app.WaitFor("id:tablet-drawer", 15*time.Second)
		} else {
			app.Drag("id:tablet-drawer-edge", core.Point{X: 100})
			app.WaitFor("id:tablet-drawer", time.Second)
		}
		if nsgotest.Target() != "" {
			app.Advance(250 * time.Millisecond)
			app.Screenshot("tablet-home-portrait-drawer")
		}
		if physical {
			fmt.Println("waygu-tablet-back")
			app.WaitForGone("id:tablet-drawer", 10*time.Second)
			app.Press("id:tablet-sidebar-open")
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
		if physical {
			app.Press("id:tablet-sidebar-open")
			app.WaitFor("id:tablet-drawer", time.Second)
			fmt.Println("waygu-tablet-rotate")
			app.WaitForGone("id:tablet-drawer", 15*time.Second)
			app.WaitFor("id:tablet-sidebar", 5*time.Second)
			app.WaitForGone("id:tablet-drawer-edge", time.Second)
			app.WaitFor("id:bitrate-limit", time.Second)
		}
	})
}

func TestTabletHomeCatalogFormats(t *testing.T) {
	item := media.Item{ID: "film", Title: "Fixture Film", Kind: media.Movie, Year: 2024,
		Genres: []string{"科幻", "冒险"}, Duration: 2*time.Hour + 46*time.Minute,
		VideoProfiles: []media.VideoProfile{{Width: 3840, Height: 2160, Range: "DOVI"}}}
	entry := source{ID: "fixture", Name: "Fixture Emby", Address: "http://fixture.example", Provider: media.Emby,
		Libraries: []sourceLibrary{{Library: media.Library{ID: "movies", Name: "Movies", Kind: "movies"}}}}
	config := map[string]string{sourcesKey: downloadFixtureJSON(t, []source{entry})}
	pages := map[string][]media.Item{"movies": {item}}
	var favorite atomic.Bool
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomTablet)
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return desktopSidebarCatalog{fixtureServer{session: session}, pages, &favorite}, nil
		}, nil)
	}, nsgotest.Size(1180, 820), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(map[string]string{"fixture": "fixture-token"}))
	if count := app.WaitFor("id:tablet-category-count-movies", 5*time.Second).Text; count != "1" {
		t.Fatalf("tablet movie count = %q", count)
	}
	if facts := app.WaitFor("id:tablet-hero-facts", 5*time.Second).Text; facts != "2024 · 科幻 / 冒险 · 2 小时 46 分 · 4K 杜比视界" {
		t.Fatalf("tablet hero facts = %q", facts)
	}
	if quality := app.WaitFor("id:poster-quality", 5*time.Second).Text; quality != "4K" {
		t.Fatalf("tablet poster quality = %q", quality)
	}
}
