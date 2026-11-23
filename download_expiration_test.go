package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerExpiredDownloads(t *testing.T) {
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux") {
		t.Skip("requires Docker fixtures and the headless or GTK host")
	}
	for _, variant := range reviewAppearances {
		if target == "" && variant.name != "light" {
			continue
		}
		t.Run(variant.name, func(t *testing.T) {
			config, secrets := dockerReviewSources(t, false)
			var sources []source
			if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
				t.Fatal(err)
			}
			emby, client, series, _ := reviewSeriesFixture(t, config, secrets, "emby")
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			seasons, err := client.Children(ctx, series)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			_, episodes := seriesFixtureEpisodes(t, ctx, client, seasons, 1, 5)
			cancel()
			healthy := watchedDownloadFixture(mediaAccountKey(emby), episodes[0].ID, episodes[0].Title)
			healthy.Item = episodes[0]
			if target == "linux" {
				sources = []source{emby}
			}
			config[sourcesKey] = downloadFixtureJSON(t, sources)
			now := time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC)
			config[downloadCleanupKey] = "fixture-cleanup"
			entries := []downloadEntry{healthy}
			for _, source := range sources {
				id := "987654321"
				if source.Provider == media.Jellyfin {
					id = "ed64c344981b47f4b8b9faa7702241e6"
				}
				entry := watchedDownloadFixture(mediaAccountKey(source), id, "Waygu Expired · "+string(source.Provider))
				entry.Completion = &downloadCompletion{At: now.UnixNano(), Policy: "fixture-cleanup"}
				entries = append(entries, entry)
			}
			app, dir := startDownloadTestApp(t, now, variant.appearance, config, entries, nsgotest.Secrets(secrets))
			for _, entry := range entries {
				app.WaitFor("id:download-play-"+entry.ID, time.Second)
			}
			app.Press("id:back")
			app.Press("id:open-phone-source-settings")
			for _, source := range sources {
				app.Press("id:source-sync-" + source.ID)
			}
			for _, entry := range entries[1:] {
				app.WaitUntil(10*time.Second, func() bool {
					return findWatchedDownload(t, app, entry.ID).Availability.Expired == downloadMissingFromSource
				})
			}
			app.Press("id:back")
			app.Press("id:settings-downloads")
			app.WaitForGone("id:download-expired-"+healthy.ID, time.Second)
			expired := entries[1]
			app.WaitFor("id:download-expired-"+expired.ID, 3*time.Second)
			if target == "linux" {
				app.Advance(500 * time.Millisecond)
				app.Screenshot("storage-expired-" + variant.name)
			}
			app.Press("id:download-keep-" + expired.ID)
			app.WaitFor("text:已保留本地副本", 3*time.Second)
			kept := findWatchedDownload(t, app, expired.ID)
			if !kept.Availability.Keep || kept.Completion != nil {
				t.Fatalf("Keep did not persist: %+v", kept.Availability)
			}
			app.Press("id:download-play-" + expired.ID)
			video := app.WaitFor("id:video-player", 5*time.Second)
			if _, err := os.Stat(video.Video.Source); err != nil {
				t.Fatalf("expired download did not open a local file: %v", err)
			}
			if target == "linux" {
				app.WaitUntil(8*time.Second, func() bool { return app.Find("id:player-position").Text != "00:00" })
			}
			app.Press("id:close-player")
			app.WaitFor("id:download-play-"+expired.ID, 3*time.Second)
			if target == "" {
				app.FastForward(25 * time.Hour)
				for _, entry := range entries[1:] {
					assertWatchedDownloadFile(t, dir, entry.ID, true)
				}
			}
			app.Press("id:download-remove-" + expired.ID)
			app.Press("id:confirm-remove-download-" + expired.ID)
			app.WaitForGone("id:download-play-"+expired.ID, 5*time.Second)
			assertWatchedDownloadFile(t, dir, expired.ID, false)
			assertWatchedDownloadFile(t, dir, healthy.ID, true)
		})
	}
}

func TestDownloadExpirationAfterSourceOutage(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses a simulated wall clock to cross the outage threshold")
	}
	var online, present atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !online.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/Users/viewer/Views":
			w.Write([]byte(`{"Items":[]}`))
		case "/Users/viewer/Items/offline":
			if !present.Load() {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(`{"Id":"offline","Type":"Movie","Name":"Offline fixture"}`))
		default:
			w.Write([]byte(`{"Items":[]}`))
		}
	}))
	t.Cleanup(server.Close)
	now := time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC)
	provider := source{ID: "outage", Provider: media.Emby, Address: server.URL, AccountID: "viewer", Name: "Emby fixture"}
	entry := watchedDownloadFixture(mediaAccountKey(provider), "offline", "Offline fixture")
	entry.Availability.OfflineSince = now.Add(time.Minute - downloadSourceOfflineAge).Unix()
	entry.Completion = &downloadCompletion{At: now.UnixNano(), Policy: "fixture-cleanup"}
	config := map[string]string{sourcesKey: downloadFixtureJSON(t, []source{provider}), downloadCleanupKey: "fixture-cleanup"}
	app, dir := startDownloadTestApp(t, now, core.AppearanceLight, config, []downloadEntry{entry},
		nsgotest.Secrets(map[string]string{"outage": "fixture-token"}))
	app.WaitForGone("id:download-expired-"+entry.ID, time.Second)
	app.FastForward(2 * time.Minute)
	app.Press("id:back")
	app.Press("id:open-phone-source-settings")
	app.Press("id:source-sync-outage")
	app.WaitUntil(5*time.Second, func() bool {
		return findWatchedDownload(t, app, entry.ID).Availability.Expired == downloadSourceOffline
	})
	app.Press("id:back")
	app.Press("id:settings-downloads")
	app.WaitFor("text:距首次无法连接媒体服务器已满 30 天。", 3*time.Second)
	app.FastForward(25 * time.Hour)
	assertWatchedDownloadFile(t, dir, entry.ID, true)
	online.Store(true)
	app.Press("id:back")
	app.Press("id:open-phone-source-settings")
	app.Press("id:source-sync-outage")
	app.WaitUntil(5*time.Second, func() bool {
		state := findWatchedDownload(t, app, entry.ID).Availability
		return state.Expired == downloadMissingFromSource && state.OfflineSince == 0
	})
	app.Press("id:back")
	app.Press("id:settings-downloads")
	assertWatchedDownloadFile(t, dir, entry.ID, true)
	app.Press("id:download-keep-" + entry.ID)
	app.WaitFor("text:已保留本地副本", 3*time.Second)
	present.Store(true)
	app.Press("id:back")
	app.Press("id:open-phone-source-settings")
	app.Press("id:source-sync-outage")
	app.WaitUntil(5*time.Second, func() bool {
		state := findWatchedDownload(t, app, entry.ID).Availability
		return state.Expired == "" && state.Keep
	})
	app.Press("id:back")
	app.Press("id:settings-downloads")
	app.WaitForGone("id:download-expired-"+entry.ID, time.Second)
	assertWatchedDownloadFile(t, dir, entry.ID, true)
}

func TestAndroidExpiredDownload(t *testing.T) {
	if nsgotest.Target() != "android" {
		t.Skip("captures the storage flow on an Android phone")
	}
	entry := watchedDownloadFixture("local-test", "expired", "Waygu Expired")
	entry.Availability.Expired = downloadMissingFromSource
	app, dir := startWatchedDownloadApp(t, time.Now(), core.AppearanceLight, nil, entry)
	app.Scroll("id:storage-downloads-scroll", -350)
	app.WaitFor("id:download-expired-"+entry.ID, 3*time.Second)
	app.Advance(500 * time.Millisecond)
	app.Screenshot("storage-expired-android")
	if os.Getenv("WAYGU_CAPTURE_STORAGE_DISPLAY") == "1" {
		marker := filepath.Join(dir, "storage-capture.ready")
		if err := os.WriteFile(marker, nil, 0600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(marker) })
		time.Sleep(15 * time.Second)
	}
	app.Press("id:download-keep-" + entry.ID)
	app.WaitFor("text:已保留本地副本", 3*time.Second)
	app.Press("id:download-play-" + entry.ID)
	app.WaitFor("id:video-player", 8*time.Second)
	app.WaitUntil(8*time.Second, func() bool { return app.Find("id:player-position").Text != "00:00" })
	assertWatchedDownloadFile(t, dir, entry.ID, true)
}
