package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDockerTVHomeReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("the desktop capture harness requires GTK")
	}
	config, secrets := homeReviewSources(t)
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			const width, height = 1920, 1760
			windowHeight := core.Points(height)
			if nsgotest.Target() == "linux" {
				windowHeight += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				shellIdiom.Provide(cx, core.IdiomTV)
				return Root(cx)
			}, nsgotest.Size(width, windowHeight),
				nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.WaitFor("id:home-hero-details", 10*time.Second)
			app.WaitFor("id:tv-resume-rail", time.Second)
			app.WaitFor("id:tv-recent-rail", time.Second)
			if got := app.Find("id:tv-featured").Frame.Size.Height; got != 900 {
				t.Errorf("featured height = %v, want 900", got)
			}
			if got := app.Find("id:home-hero-play").Frame.Size.Height; got != 80 {
				t.Errorf("play button height = %v, want 80", got)
			}
			for _, shelf := range []struct {
				id            string
				width, height core.Points
			}{
				{"tv-resume-rail", 420, 236},
				{"tv-recent-rail", 220, 330},
			} {
				card := app.FindMounted("id:" + shelf.id + " >> role:button >> nth=0")
				if size := card.Children[0].Frame.Size; size.Width != shelf.width || size.Height != shelf.height {
					t.Errorf("%s artwork = %+v, want %v × %v", shelf.id, size, shelf.width, shelf.height)
				}
			}
			if nsgotest.Target() != "" {
				app.Advance(400 * time.Millisecond)
				app.Screenshot("app-tv-home-" + variant.name)
			}
			if nsgotest.Target() == "" {
				selectedDot := func() string {
					for _, dot := range app.Find("id:tv-featured-dots").Children {
						if dot.Props.Selected {
							return dot.Props.TestID
						}
					}
					t.Fatal("the carousel has no selected indicator")
					return ""
				}
				app.Focus("id:tv-tab-home")
				app.Focus("id:home-hero-play")
				dot := selectedDot()
				app.FastForward(6 * time.Second)
				if selectedDot() == dot {
					t.Fatal("the carousel did not advance after holding focus for six seconds")
				}
				if !app.Find("id:home-hero-play").Focused {
					t.Fatal("the Play button lost focus when the carousel advanced")
				}
				app.Focus("id:tv-tab-home")
				dot = selectedDot()
				app.FastForward(7 * time.Second)
				if selectedDot() != dot {
					t.Fatal("the carousel advanced after the hero lost focus")
				}
			}
			app.Scroll("id:tv-home-scroll", 890)
			for _, source := range []string{"emby", "jellyfin", "plex"} {
				app.FindMounted("id:tv-source-" + source)
			}
			if nsgotest.Target() != "" {
				app.Screenshot("app-tv-home-" + variant.name + "-sources")
			}
			if nsgotest.Target() == "" {
				cards := app.Query("id:tv-resume-rail >> role:button")
				card := "id:" + cards[0].Props.TestID
				app.LongPress(card)
				app.Press("id:tv-resume-remove")
				app.WaitForGone(card, time.Second)
				if data, ok := app.Storage().GetItem(tvResumeDismissedKey); !ok || data == "" {
					t.Fatal("the removed bookmark was not persisted")
				}
				app.ScrollTo("id:tv-home-scroll", core.Point{})
			} else {
				app.Scroll("id:tv-home-scroll", -890)
			}
			app.WaitFor("id:home-hero-details", 10*time.Second)
			app.Press("id:home-hero-details")
			app.WaitFor("id:detail-play", 5*time.Second)
			app.Press("id:back")
			app.WaitFor("id:home-hero-play", 5*time.Second)
			app.Press("id:tv-tab-library")
			if !app.Find("id:tv-tab-library").Props.Selected {
				t.Fatal("movie shortcut did not select Movies")
			}
			app.Press("id:tv-tab-series")
			if !app.Find("id:tv-tab-series").Props.Selected {
				t.Fatal("series shortcut did not select Series")
			}
			app.Press("id:tv-tab-anime")
			if !app.Find("id:tv-tab-anime").Props.Selected {
				t.Fatal("anime shortcut did not select Anime")
			}
			app.Press("id:tv-tab-favorites")
			app.WaitFor("id:library-view-favorites", 5*time.Second)
			app.Press("id:tv-tab-search")
			app.Find("id:search-query")
			if nsgotest.Target() == "" {
				app.Type("id:search-query", "Waygu Resume Test")
				app.Press("id:search-submit")
				app.WaitUntil(5*time.Second, func() bool {
					return strings.Contains(strings.Join(app.Texts(), " "), "Waygu Resume Test")
				})
			}
			app.Press("id:tv-tab-home")
			app.WaitFor("id:tv-recent-all", 5*time.Second)
			app.Press("id:tv-recent-all")
			app.WaitFor("id:catalog-category", 5*time.Second)
		})
	}
}
