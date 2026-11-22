package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func startDesktopSettingsReview(t *testing.T, locale string, config, secrets map[string]string) *nsgotest.App {
	t.Helper()
	return nsgotest.StartApp(t, runApplication, nsgotest.Size(1280, 820), nsgotest.Idiom(core.IdiomDesktop),
		nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: locale}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
}

func TestDesktopSettingsWindow(t *testing.T) {
	app := startDesktopSettingsReview(t, "en", map[string]string{seenKey: "true", appearanceKey: "light"}, nil)
	app.Key("mod+,")
	window := app.Surface("title:Settings")
	if config := window.Config(); config.Width != 1100 || config.Height != 760 {
		t.Fatalf("settings size = %+v", config)
	}
	if frame := window.Find("id:desktop-settings-sidebar").Frame.Size; frame.Width != 224 {
		t.Fatalf("sidebar width = %v", frame.Width)
	}
	if frame := window.Find("id:desktop-settings-titlebar").Frame.Size; frame.Height != 48 {
		t.Fatalf("titlebar height = %v", frame.Height)
	}
	window.Type("id:desktop-settings-search", "danmaku")
	if len(window.Query("id:desktop-settings-tab-general")) != 0 {
		t.Fatal("search did not narrow settings")
	}
	window.Press("id:desktop-settings-tab-danmaku")
	window.Find("id:desktop-danmaku-preview")
	window.Press("id:desktop-settings-danmaku-area-full")
	window.Press("id:desktop-settings-danmaku-enabled")
	if value := readDanmakuPreferences(app.Storage()); value.Area != 100 || value.Enabled {
		t.Fatalf("saved danmaku preferences = %+v", value)
	}
	window.Press("id:desktop-danmaku-add-filter")
	window.Type("id:danmaku-keywords", "spoiler")
	window.Press("id:danmaku-save-filters")
	app.Advance(400 * time.Millisecond)
	if value := readDanmakuPreferences(app.Storage()); value.Keywords != "spoiler" {
		t.Fatalf("saved filter = %q", value.Keywords)
	}
	window.Press("id:desktop-settings-tab-playback")
	for _, change := range []struct{ id, label string }{
		{"desktop-playback-engine", "Built-in mpv"},
		{"desktop-seek-step", "Back 10s / forward 30s"},
		{"desktop-auto-next", "Next episode in 5 s"},
	} {
		window.Press("id:" + change.id)
		window.Press("role:option " + change.label)
		app.Advance(350 * time.Millisecond)
	}
	if value := readDesktopPreferences(app.Storage()); value.Back != 10 || value.Forward != 30 || value.Countdown != 5 {
		t.Fatalf("saved transport preferences = %+v", value)
	}
	if value, _ := app.Storage().GetItem(mpvKey); value != "true" {
		t.Fatalf("saved engine = %q", value)
	}
	window.Press("id:desktop-settings-tab-general")
	window.Press("id:desktop-startup-last")
	window.Press("id:desktop-appearance-dark")
	if value := readDesktopPreferences(app.Storage()); !value.StartLast {
		t.Fatalf("saved startup preferences = %+v", value)
	}
	if value, _ := app.Storage().GetItem(appearanceKey); value != "dark" {
		t.Fatalf("saved appearance = %q", value)
	}
	window.Press("id:desktop-settings-tab-parental")
	window.Find("id:parental-enabled")
	window.Press("id:desktop-settings-tab-sources")
	window.Press("id:source-settings-add")
	app.WaitFor("id:lan-discovery", 3*time.Second)
	app.Press("id:back")
	window.Find("id:source-settings-add")
	window.Press("id:desktop-settings-overview")
	window.Find("id:desktop-settings-quick-playback")
	window.Press("id:desktop-settings-close")
	app.WaitUntil(3*time.Second, func() bool { return len(app.Surfaces()) == 1 })
	app.Key("mod+,")
	window = app.Surface("title:Settings")
	window.Find("id:desktop-settings-quick-playback")
	window.Press("id:desktop-settings-tab-general")
	window.Press("id:desktop-settings-language")
	window.Press("role:option 简体中文")
	if value := readDesktopPreferences(app.Storage()); value.Language != "zh-Hans" {
		t.Fatalf("saved language = %q", value.Language)
	}
	window.Find("text:通用")
}

func TestDesktopSettingsRestore(t *testing.T) {
	app := startDesktopSettingsReview(t, "en", map[string]string{
		seenKey: "true", desktopPreferencesKey: `{"StartLast":true}`,
		desktopLastPageKey: `{"Tab":1,"Category":"series"}`,
	}, nil)
	app.WaitFor("id:desktop-library", 3*time.Second)
	app.Press("id:tab-search")
	raw, _ := app.Storage().GetItem(desktopLastPageKey)
	var saved desktopLastPage
	if err := json.Unmarshal([]byte(raw), &saved); err != nil || saved.Tab != 2 {
		t.Fatalf("saved desktop page = %s, %v", raw, err)
	}
}

func TestDockerDesktopSettingsReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("desktop design capture runs on Linux GTK")
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			config, secrets := homeReviewSources(t)
			config[appearanceKey] = theme.name
			config[desktopPreferencesKey] = `{"Language":"zh-Hans","Back":10,"Forward":30,"Countdown":5}`
			config[mpvKey] = "true"
			config[danmakuPreferencesKey] = `{"Opacity":80,"FontScale":85,"Density":50,"Keywords":"剧透\n前方高能\n空降","Patterns":"^6{3,}$"}`
			secrets[dandanCredentialKey] = `{"AppID":"review-app","Secret":"review-secret"}`
			app := startDesktopSettingsReview(t, "zh-Hans", config, secrets)
			app.Key("mod+,")
			window := app.Surface("title:设置")
			capture := func(page string) {
				if nsgotest.Target() != "" {
					app.Advance(450 * time.Millisecond)
					window.Screenshot("app-desktop-settings-" + page + "-" + theme.name)
				}
			}
			app.WaitUntil(7*time.Second, func() bool { return len(window.Query("text:3 个媒体源 · 3 在线")) > 0 })
			capture("overview")
			for _, page := range []string{"general", "playback", "danmaku", "sources"} {
				window.Press("id:desktop-settings-tab-" + page)
				window.Find("id:desktop-settings-page-" + page)
				capture(page)
			}
			for _, id := range []string{"emby", "jellyfin", "plex"} {
				if state := window.Find("id:source-settings-status-" + id).Text; state != "在线" {
					t.Fatalf("%s source status = %q", id, state)
				}
			}
			if theme.name != "light" {
				return
			}
			var entries []source
			if err := json.Unmarshal([]byte(config[sourcesKey]), &entries); err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.ID != "jellyfin" || len(entry.Libraries) == 0 {
					continue
				}
				window.Press("id:desktop-source-row-" + entry.ID)
				id := entry.Libraries[0].ID
				window.Press("id:desktop-source-library-" + id)
				want := !entry.Libraries[0].Hidden
				app.WaitUntil(2*time.Second, func() bool {
					raw, _ := app.Storage().GetItem(sourcesKey)
					var saved []source
					_ = json.Unmarshal([]byte(raw), &saved)
					for _, item := range saved {
						if item.ID == entry.ID && len(item.Libraries) > 0 && item.Libraries[0].ID == id {
							return item.Libraries[0].Hidden == want
						}
					}
					return false
				})
				window.Press("id:desktop-source-sync")
				app.WaitUntil(6*time.Second, func() bool {
					return strings.Contains(window.Find("id:source-settings-status-jellyfin").Text, "在线")
				})
				return
			}
			t.Fatal("Jellyfin fixture has no media library")
		})
	}
}
