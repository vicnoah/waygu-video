package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPhoneAboutReview(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("design comparisons use the Linux GTK backend")
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			height := core.Points(1320)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(theme.appearance)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(map[string]string{seenKey: "true", sourcesKey: "[]",
					playbackPreferencesKey: `{"Rate":1.5}`, parentalUsageKey: "{}"}))
			app.Press("id:tab-settings")
			app.Press("id:settings-about")
			app.WaitFor("id:phone-about-settings", time.Second)
			if nsgotest.Target() == "linux" {
				app.Advance(350 * time.Millisecond)
				app.Screenshot("settings-about-" + theme.name)
			}
			if theme.name != "light" {
				return
			}
			for _, panel := range []string{"help", "formats", "privacy", "licenses"} {
				app.Press("id:about-" + panel)
				app.WaitFor("id:about-detail", time.Second)
				app.Press("id:close-about-detail")
				app.WaitForGone("id:about-detail", time.Second)
			}
			app.Press("id:about-network-logs")
			app.Press("id:about-player-overlay")
			if prefs := readDiagnosticPreferences(app.Storage()); !prefs.Network || !prefs.Overlay {
				t.Fatalf("diagnostics not saved: %+v", prefs)
			}
			app.Press("id:about-reset")
			app.WaitFor("id:about-cancel-reset", time.Second)
			app.Advance(350 * time.Millisecond)
			app.Press("id:about-cancel-reset")
			app.Advance(350 * time.Millisecond)
			app.WaitForGone("id:about-cancel-reset", time.Second)
			if got := readPlaybackPreferences(app.Storage()).Rate; got != 1.5 {
				t.Fatalf("cancelled reset changed playback rate to %v", got)
			}
			app.Press("id:about-reset")
			app.WaitFor("id:about-confirm-reset", time.Second)
			app.Advance(350 * time.Millisecond)
			app.Press("id:about-confirm-reset")
			app.Advance(350 * time.Millisecond)
			app.WaitFor("id:about-status", time.Second)
			if got := readPlaybackPreferences(app.Storage()).Rate; got != 1 {
				t.Fatalf("reset playback rate to %v", got)
			}
			if got := readDiagnosticPreferences(app.Storage()); got != (diagnosticPreferences{}) {
				t.Fatalf("reset diagnostics = %+v", got)
			}
			for _, key := range []string{seenKey, sourcesKey, parentalUsageKey} {
				if _, found := app.Storage().GetItem(key); !found {
					t.Fatalf("reset removed %q", key)
				}
			}
		})
	}
}

func TestAboutExportAndPlayerDiagnostics(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses the file-picker and player fixtures")
	}
	entry := source{ID: "fixture", Provider: media.Jellyfin, Address: "https://media.example.test", Name: "Fixture",
		Libraries: []sourceLibrary{{Library: media.Library{ID: "movies"}}}}
	config, _ := json.Marshal([]source{entry})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return dandanVideoFixture{fixtureServer{session: session}, "https://media.example.test/video.mp4"}, nil
		}, nil)
	}, nsgotest.Offscreen(), nsgotest.Size(390, 1320), nsgotest.Environment(core.Environment{Locale: "en"}),
		nsgotest.Storage(map[string]string{sourcesKey: string(config)}), nsgotest.Secrets(map[string]string{entry.ID: "fixture-token"}))
	path := filepath.Join(t.TempDir(), "network.zip")
	app.SaveFile = func(_ nsgotest.FileSave) string { return path }
	app.Press("id:tab-settings")
	app.Press("id:settings-about")
	app.Press("id:about-export-logs")
	app.WaitFor("text:Network log archive saved.", time.Second)
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "network.jsonl" {
		t.Fatal("expected an empty log because recording was off")
	}
	app.Press("id:about-player-overlay")
	app.Press("id:back")
	app.Press("id:tab-home")
	app.WaitFor("id:home-hero-details", time.Second)
	app.Press("id:home-hero-details")
	app.WaitFor("id:detail-play", time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	app.Play("id:video-player", core.VideoInfo{Duration: 3 * time.Second, Size: core.Size{Width: 1280, Height: 720}})
	app.WaitFor("id:player-diagnostics-overlay", time.Second)
	app.WaitFor("text:/1280×720/", time.Second)
}

func TestAboutDiagnosticsArchive(t *testing.T) {
	now := time.Date(2026, 11, 22, 22, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	logger := &networkDiagnostics{dir: dir}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api-key-fixture/System/Info/Public" {
			t.Errorf("unexpected fixture path %s", req.URL.Path)
		}
		http.Error(w, "password-fixture", http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := media.NewObservedClient(media.Jellyfin, server.URL+"/api-key-fixture", media.NewDevice("private-device-name"),
		media.Session{Token: "token-fixture"}, func(event media.RequestMetric) { logger.record(now, media.Jellyfin, event) })
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.Probe(t.Context())
	if _, err := os.Stat(filepath.Join(dir, networkLogName(now))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("logging worked while disabled")
	}
	logger.enabled.Store(true)
	logger.record(now.AddDate(0, 0, -7), media.Emby, media.RequestMetric{Method: http.MethodGet, Status: 202})
	_, err = client.Probe(t.Context())
	if !errors.Is(err, media.ErrAuthentication) {
		t.Fatalf("fixture request = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte("private-log-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := logger.archive(now)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != networkLogName(now) {
		t.Fatalf("archive files = %+v", archive.File)
	}
	file, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil || !strings.Contains(string(raw), `"http_status":401`) || !strings.Contains(string(raw), `"provider":"jellyfin"`) {
		t.Fatalf("network export = %q, %v", raw, err)
	}
	for _, unexpected := range []string{"password", "token", "api-key", "private", "127.0.0.1"} {
		if bytes.Contains(raw, []byte(unexpected)) {
			t.Fatalf("network export contained %q", unexpected)
		}
	}
}

func TestAboutUpdateShowsPublishedRelease(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses a local release fixture")
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return aboutHeader(cx, func(context.Context) (string, error) { return "v99.0.0", nil })
	}, nsgotest.Environment(core.Environment{Locale: "en"}))
	app.Press("id:about-check-update")
	app.WaitFor("id:about-update-status", time.Second)
	app.WaitFor("text:/v99.0.0/", time.Second)
}

func TestDockerAboutPlayerOverlay(t *testing.T) {
	if nsgotest.Target() != "linux" || os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the GTK player and the local Docker media fixtures")
	}
	config, secrets := homeReviewSources(t)
	_, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, 1357), nsgotest.Environment(core.Environment{Locale: "en"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:tab-settings")
	app.Press("id:settings-about")
	app.Press("id:about-player-overlay")
	app.Press("id:back")
	app.Press("id:tab-search")
	app.Type("id:search-query", "Waygu Track Test")
	app.Press("id:search-source-emby")
	app.WaitFor("id:title-emby-"+movie.ID, 10*time.Second)
	app.Press("id:title-emby-" + movie.ID)
	app.WaitFor("id:detail-play", 10*time.Second)
	app.Press("id:detail-play")
	app.Resize(844, 390)
	app.WaitFor("id:video-player", 10*time.Second)
	app.WaitFor("id:player-diagnostics-overlay >> text:/320×180/", 10*time.Second)
	app.WaitFor("id:player-diagnostics-overlay >> text:/H264/", 3*time.Second)
	app.Screenshot("settings-about-player-overlay")
}
