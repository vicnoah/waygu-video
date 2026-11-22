package main

import (
	"encoding/json"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerTVDetailReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("the desktop capture harness requires GTK")
	}

	config, secrets := homeReviewSources(t)
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	emby, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	extras, err := emby.(media.MovieFeaturesClient).Extras(t.Context(), movie)
	if err != nil || len(extras) < 2 || !extras[0].Trailer || extras[1].Trailer {
		t.Fatalf("fixture should contain one trailer and a featurette, got %d extras and %v", len(extras), err)
	}
	resume := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Resume Test")
	versions := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Version Test")
	related := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Test Movie")
	single, err := json.Marshal(sources[:1])
	if err != nil {
		t.Fatal(err)
	}
	onlyEmby := maps.Clone(config)
	onlyEmby[sourcesKey] = string(single)

	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(1640)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				shellIdiom.Provide(cx, core.IdiomTV)
				return Root(cx)
			}, nsgotest.Size(1920, height), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Storage(onlyEmby), nsgotest.Secrets(secrets))
			app.WaitFor("id:home-hero-details", 8*time.Second)
			app.Press("id:home-hero-details")
			app.WaitFor("id:tv-detail-hero", 5*time.Second)
			app.WaitFor("id:tv-detail-trailer", 5*time.Second)
			app.WaitFor("id:tv-detail-cast", 5*time.Second)
			app.WaitFor("id:playback-version-value", 5*time.Second)
			app.WaitFor("id:tv-detail-related", 6*time.Second)
			if height := app.Find("id:tv-detail-hero").Frame.Size.Height; height != 980 {
				t.Fatalf("hero height = %v, want 980", height)
			}
			if height := app.Find("id:detail-play").Frame.Size.Height; height != 80 {
				t.Fatalf("primary action height = %v, want 80", height)
			}
			credit := app.Find("id:tv-cast-0").Children[0].Frame.Size
			if credit.Width != 144 || credit.Height != 144 {
				t.Fatalf("credit portrait = %v, want 144 × 144", credit)
			}
			if nsgotest.Target() != "" {
				app.Focus("id:back")
				app.Focus("id:detail-play")
				app.Advance(400 * time.Millisecond)
				app.Screenshot("app-tv-detail-" + variant.name)
				app.Scroll("id:tv-detail-scroll", 850)
				app.Screenshot("app-tv-detail-" + variant.name + "-shelves")
			}
			app.Press("id:tv-cast-0")
			app.WaitFor("id:tv-credit-close", 3*time.Second)
			app.Press("id:tv-credit-close")
			app.WaitForGone("id:tv-credit-close", 3*time.Second)
			app.Press("id:tv-detail-related-title-" + related)
			app.WaitFor("id:tv-detail-title", 5*time.Second)
			if got := app.Find("id:tv-detail-title").Text; !strings.Contains(got, "Waygu Test Movie") {
				t.Fatalf("related card opened %q", got)
			}
			app.Press("id:back")
			app.WaitFor("id:tv-detail-trailer", 5*time.Second)
			app.Press("id:tv-detail-trailer")
			app.WaitFor("id:video-player", 8*time.Second)
			app.WaitFor("id:tv-player", 3*time.Second)
			app.Key("escape")
			app.WaitFor("id:tv-detail-hero", 5*time.Second)
			app.Press("id:back")

			openRecent := func(id string) {
				t.Helper()
				app.WaitFor("id:tv-recent-rail", 8*time.Second)
				app.Scroll("id:tv-home-scroll", 980)
				app.Press("id:title-" + id)
				app.WaitFor("id:tv-detail-hero", 5*time.Second)
			}
			openRecent(resume)
			app.WaitFor("id:detail-progress", 5*time.Second)
			if name := app.Find("id:detail-play").Props.Label; !strings.Contains(name, "1:20") {
				t.Fatalf("resume action does not show the saved position: %q", name)
			}
			app.WaitFor("id:detail-from-start", 5*time.Second)
			if nsgotest.Target() != "" {
				app.Focus("id:back")
				app.Focus("id:detail-play")
				app.Screenshot("app-tv-detail-" + variant.name + "-resume")
			}
			app.Press("id:back")
			openRecent(versions)
			app.WaitUntil(5*time.Second, func() bool { return app.Enabled("id:playback-version") })
			app.Press("id:playback-version")
			option := "role:radio /180p/"
			app.WaitFor(option, 4*time.Second)
			app.Press(option)
			app.WaitForGone("id:tv-detail-version-options", 3*time.Second)
			if value := app.Find("id:playback-version-value").Text; !strings.Contains(value, "180p") {
				t.Fatalf("version control shows %q, want 180p", value)
			}
			app.Press("id:back")
			openRecent(versions)
			app.WaitFor("text:版本 180p", 5*time.Second)
		})
	}
	if nsgotest.Target() == "" {
		t.Run("media-source", func(t *testing.T) {
			shared := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Resume Across Sources")
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomTV)
				return Root(cx)
			}, nsgotest.Size(1920, 1640), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.WaitFor("id:tv-recent-rail", 8*time.Second)
			app.Scroll("id:tv-home-scroll", 940)
			app.Press("id:title-emby-" + shared)
			app.WaitFor("id:tv-detail-formats >> text:Emby 影视库", 5*time.Second)
			app.WaitUntil(5*time.Second, func() bool { return app.Enabled("id:playback-version") })
			app.Press("id:playback-version")
			app.WaitFor("role:radio Jellyfin 影视库", 5*time.Second)
			app.Press("role:radio Jellyfin 影视库")
			app.WaitFor("id:tv-detail-formats >> text:Jellyfin 影视库", 5*time.Second)
		})
	}
}
