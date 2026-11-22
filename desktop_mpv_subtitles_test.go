package main

import (
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDockerDesktopEmbeddedSubtitles(t *testing.T) {
	if nsgotest.Target() != "linux" || os.Getenv("WAYGU_TEST_SERVERS") != "1" || os.Getenv("WAYGU_TEST_DESKTOP_MPV") != "1" {
		t.Skip("requires the Linux GTK libmpv host and the Docker fixtures")
	}
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			config, secrets := homeReviewSources(t)
			_, movie := reviewFixtureItem(t, config, secrets, provider, "Waygu Embedded Subtitle Test")
			config[mpvKey] = "true"
			config[playbackPreferencesKey] = `{"DisableProgressRecording":true}`
			config[subtitlePreferencesKey] = `{"Languages":["zh-Hant","en"],"Mode":"always"}`
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomDesktop)
				return Root(cx)
			}, nsgotest.Size(1440, 847), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.Press("id:tab-library")
			card := "id:title-" + provider + "-" + movie.ID
			app.WaitFor(card, 10*time.Second)
			app.Press(card)
			app.WaitFor("id:detail-play", 10*time.Second)
			app.Press("id:detail-play")
			app.WaitFor("id:desktop-player", 10*time.Second)
			app.WaitFor("id:video-player", 10*time.Second)
			app.Press("id:toggle-play")
			app.WaitUntil(3*time.Second, func() bool { return app.Find("id:toggle-play").Props.Label == "播放" })
			app.Press("id:player-tracks")
			app.WaitUntil(4*time.Second, func() bool { return app.Find("id:decoder-subtitle-1").Props.Checked })
			app.Press("id:close-player-tracks")
			app.WaitForGone("id:player-tracks-dialog", 3*time.Second)
			const rail = "id:desktop-player-position-rail"
			width := app.Find(rail).Frame.Size.Width
			app.PressAt(rail, core.Point{X: width / 6, Y: 8})
			const chinese, english = "text:理髮以後再出發。", "text:The train leaves at dawn."
			if node := app.WaitFor(chinese, 9*time.Second); node.Props.TestID != "subtitle-caption" {
				t.Fatal("the converted embedded track was not displayed as the primary caption")
			}
			if node := app.WaitFor(english, 4*time.Second); node.Props.TestID != "subtitle-caption-secondary" {
				t.Fatal("the embedded English track was not displayed as the secondary caption")
			}
			app.Screenshot("desktop-embedded-bilingual-" + provider)
			position := app.Find("id:player-position").Text
			app.Press("id:player-tracks")
			app.WaitFor("id:decoder-subtitle-3", 4*time.Second)
			if !app.Find("id:decoder-subtitle-1").Props.Checked || app.Find("id:decoder-subtitle-2").Props.Checked {
				t.Fatal("the track picker marked the translation as the primary subtitle")
			}
			app.Press("id:decoder-subtitle-2")
			app.WaitUntil(4*time.Second, func() bool { return app.Find("id:decoder-subtitle-2").Props.Checked })
			app.Press("id:decoder-subtitle-1")
			app.WaitUntil(4*time.Second, func() bool { return app.Find("id:decoder-subtitle-1").Props.Checked })

			app.Press("id:decoder-subtitle-3")
			app.WaitUntil(4*time.Second, func() bool { return app.Find("id:decoder-subtitle-3").Props.Checked })
			app.WaitForGone("id:subtitle-caption", 3*time.Second)
			app.Press("id:close-player-tracks")
			app.WaitForGone("id:player-tracks-dialog", 3*time.Second)
			app.Screenshot("desktop-embedded-ass-original-" + provider)
			app.Press("id:player-tracks")
			const scroll = "id:player-tracks-dialog-scroll"
			app.Press("id:player-subtitle-style")
			app.Press("id:subtitle-language-order")
			app.Press("id:subtitle-language-up-en")
			if order := readSubtitlePreferences(app.Storage()).Languages; order[0] != "en" {
				t.Fatal("the desktop subtitle editor did not save the new language order")
			}
			app.Press("id:subtitle-language-up-zh-Hant")
			app.Press("id:player-subtitle-language-back")
			app.Scroll(scroll, 1350)
			app.Screenshot("desktop-embedded-ass-settings-enabled-" + provider)
			app.Press("id:subtitle-keep-ass")
			app.Press("id:subtitle-embedded-fonts")
			prefs := readSubtitlePreferences(app.Storage())
			if !prefs.FlattenASS || !prefs.DisableEmbeddedFonts {
				t.Fatal("ASS subtitle preferences did not persist")
			}
			app.Screenshot("desktop-embedded-ass-settings-" + provider)
			app.Press("id:close-player-tracks")
			app.WaitForGone("id:player-tracks-dialog", 3*time.Second)
			if node := app.WaitFor("text:Rótulo ASS de prueba", 4*time.Second); node.Props.TestID != "subtitle-caption" {
				t.Fatal("ASS did not switch to the app's text style")
			}
			app.Screenshot("desktop-embedded-ass-text-" + provider)
			if app.Find("id:player-position").Text != position || app.Find("id:toggle-play").Props.Label != "播放" {
				t.Fatal("subtitle selection or styling changed the paused playhead")
			}
		})
	}
}
