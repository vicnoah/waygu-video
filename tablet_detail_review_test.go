package main

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func reviewFixtureItem(t *testing.T, config, secrets map[string]string, sourceID, title string) (media.Client, media.Item) {
	t.Helper()
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, entry := range sources {
		if entry.ID != sourceID {
			continue
		}
		client, err := media.NewClient(entry.Provider, entry.Address, media.NewDevice("Waygu detail review"),
			media.Session{UserID: entry.AccountID, Token: secrets[entry.ID]})
		if err != nil {
			t.Fatal(err)
		}
		for _, library := range entry.Libraries {
			if library.Name != "Movies" {
				continue
			}
			result, err := client.Catalog(ctx, media.Query{LibraryID: library.ID, Search: title})
			if err != nil || len(result.Items) != 1 {
				t.Fatalf("read detail fixture %q: %v, %d items", title, err, len(result.Items))
			}
			return client, result.Items[0]
		}
	}
	t.Fatalf("fixture source %q does not contain the movie library", sourceID)
	return nil, media.Item{}
}

func reviewFixtureTitle(t *testing.T, config, secrets map[string]string, sourceID, title string) string {
	t.Helper()
	_, item := reviewFixtureItem(t, config, secrets, sourceID, title)
	return item.ID
}

func TestDockerTabletDetailReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	title := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Track Test")
	versionFixture := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Version Test")
	resumeFixture := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Resume Test")
	sharedFixture := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Resume Across Sources")
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(820)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				shellIdiom.Provide(cx, core.IdiomTablet)
				return Root(cx)
			}, nsgotest.Size(1180, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.WaitFor("id:tablet-source-emby", 5*time.Second)
			app.Press("id:tablet-source-emby")
			app.WaitFor("id:title-emby-"+title, 10*time.Second)
			app.Press("id:title-emby-" + title)
			app.WaitFor("id:detail-play", 5*time.Second)
			app.WaitFor("id:detail-audio", 5*time.Second)
			app.WaitFor("id:detail-subtitle", 5*time.Second)
			if width := app.Find("id:tablet-detail-poster").Frame.Size.Width; width != 420 {
				t.Fatalf("tablet poster width = %v, want 420", width)
			}
			versionWidth := app.Find("id:playback-version").Frame.Size.Width
			if width := app.Find("id:detail-subtitle").Frame.Size.Width; width < versionWidth-2 || width > versionWidth+2 {
				t.Fatalf("version width %v differs from subtitle width %v", versionWidth, width)
			}
			if height := app.Find("id:detail-play").Frame.Size.Height; height != 48 {
				t.Fatalf("play button height = %v, want 48", height)
			}
			if nsgotest.Target() != "" {
				app.Advance(time.Second)
				app.Screenshot("tablet-detail-" + variant.name)
			}
			selectOption := func(control, contains string) string {
				t.Helper()
				app.Press("id:" + control)
				selector := "role:option /" + regexp.QuoteMeta(contains) + "/"
				label := app.WaitFor(selector, time.Second).Props.Label
				app.Press(selector)
				return label
			}
			audio := selectOption("detail-audio", "Japanese")
			if got := app.Find("id:detail-audio-value").Text; got != audio {
				t.Fatalf("selected audio = %q, want %q", got, audio)
			}
			subtitle := selectOption("detail-subtitle", "外挂")
			if got := app.Find("id:detail-subtitle-value").Text; got != subtitle {
				t.Fatalf("selected subtitle = %q, want %q", got, subtitle)
			}
			app.Press("id:detail-play")
			app.WaitFor("id:video-player", 8*time.Second)
			if nsgotest.Target() == "" {
				app.Play("id:video-player", core.VideoInfo{Duration: 24 * time.Second})
				app.Progress("id:video-player", 2*time.Second, 24*time.Second)
			}
			app.WaitFor("text:Waygu track selection fixture", 12*time.Second)
			app.Press("id:player-tracks")
			app.WaitFor("id:player-tracks-dialog", time.Second)
			app.Advance(400 * time.Millisecond)
			app.Press("id:track-tab-audio")
			if !app.Find("role:radio /Japanese/").Props.Checked {
				t.Fatal("the player did not select the requested audio track")
			}
			app.Press("id:close-player-tracks")
			app.WaitForGone("id:player-tracks-dialog", time.Second)
			app.Advance(400 * time.Millisecond)
			app.Press("id:close-player")
			app.WaitFor("id:tablet-detail-poster", 5*time.Second)
			app.Press("id:back")

			open := func(item string) {
				app.WaitFor("id:title-emby-"+item, 5*time.Second)
				app.Press("id:title-emby-" + item)
				app.WaitFor("id:tablet-detail-poster", 5*time.Second)
			}
			open(versionFixture)
			app.WaitFor("id:playback-version", 5*time.Second)
			version := selectOption("playback-version", "180p")
			app.Press("id:back")
			open(versionFixture)
			app.WaitFor("id:playback-version", 5*time.Second)
			if got := app.Find("id:playback-version-value").Text; got != version {
				t.Fatalf("remembered version = %q, want %q", got, version)
			}
			app.Press("id:back")
			if variant.appearance != core.AppearanceLight {
				return
			}
			open(resumeFixture)
			app.WaitFor("id:detail-progress", 5*time.Second)
			if width := app.WaitFor("id:detail-from-start", time.Second).Frame.Size.Width; width != 82 {
				t.Fatalf("play from beginning width = %v, want 82", width)
			}
			if nsgotest.Target() != "" {
				app.Advance(500 * time.Millisecond)
				app.Screenshot("tablet-detail-resume-light")
			}
			app.Press("id:detail-from-start")
			app.WaitFor("id:video-player", 5*time.Second)
			if nsgotest.Target() == "" {
				app.Play("id:video-player", core.VideoInfo{Duration: 6 * time.Minute})
				if got := app.Find("id:player-position").Text; got != "00:00" {
					t.Fatalf("play from beginning started at %q", got)
				}
			}
			app.Press("id:close-player")
			app.WaitFor("id:tablet-detail-poster", 5*time.Second)
			app.Press("id:back")

			app.Press("id:tab-home")
			open(sharedFixture)
			app.WaitFor("id:playback-source", 5*time.Second)
			selectOption("playback-source", "Jellyfin")
			app.WaitFor("text:来源 · Jellyfin 影视库", 5*time.Second)
		})
	}
}
