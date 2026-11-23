package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/downloads"
	"waygu-video/internal/media"
)

const deleteWatchedSwitch = "id:download-delete-watched"

func TestWatchedDownloadCleanup(t *testing.T) {
	provider := source{ID: "offline-fixture", Provider: media.Emby, Address: "https://offline.example.test", AccountID: "viewer", Name: "Emby", ProgressDisabled: true}
	movie := watchedDownloadFixture(mediaAccountKey(provider), "completed", "Playback fixture")
	unwatched := watchedDownloadFixture("different-account", movie.Item.ID, "Other account")
	app, dir := startWatchedDownloadApp(t, time.Now(), core.AppearanceLight, map[string]string{
		sourcesKey: downloadFixtureJSON(t, []source{provider}),
	}, movie, unwatched)
	app.Press(deleteWatchedSwitch)
	policy, _ := app.Storage().GetItem(downloadCleanupKey)
	if policy == "" {
		t.Fatal("enabling cleanup did not save the preference")
	}
	if nsgotest.Target() == "android" {
		app.Scroll("id:storage-downloads-scroll", -350)
	}
	play := func() {
		app.Press("id:download-play-" + movie.ID)
		app.WaitFor("id:video-player", 5*time.Second)
	}
	const duration = 3 * time.Second
	if nsgotest.Target() == "" {
		play()
		app.Play("id:video-player", core.VideoInfo{Duration: duration})
		app.Progress("id:video-player", 2*time.Second, duration)
		app.Press("id:close-player")
		entry := findWatchedDownload(t, app, movie.ID)
		if entry.Completion != nil {
			t.Fatal("closing before the end scheduled deletion")
		}
	}
	play()
	if nsgotest.Target() == "" {
		app.Play("id:video-player", core.VideoInfo{Duration: duration})
		app.Progress("id:video-player", duration, duration)
	}
	app.WaitUntil(12*time.Second, func() bool { return findWatchedDownload(t, app, movie.ID).Completion != nil })
	entry := findWatchedDownload(t, app, movie.ID)
	if entry.Completion.Policy != policy || entry.Completion.At <= 0 {
		t.Fatalf("saved completion = %+v", entry.Completion)
	}
	app.FastForward(25 * time.Hour)
	assertWatchedDownloadFile(t, dir, movie.ID, true)
	app.Press("id:close-player")
	app.FastForward(time.Second)
	app.WaitForGone("id:download-play-"+movie.ID, 5*time.Second)
	assertWatchedDownloadFile(t, dir, movie.ID, false)
	assertWatchedDownloadFile(t, dir, unwatched.ID, true)
	key := (pendingProgress{Account: mediaAccountKey(provider), Update: media.OfflineProgress{ItemID: movie.Item.ID}}).key()
	raw, _ := app.Storage().GetItem(key)
	var saved pendingProgress
	if err := json.Unmarshal([]byte(raw), &saved); err != nil || !saved.Update.Completed {
		t.Fatalf("deletion lost watched progress: %+v, %v", saved.Update, err)
	}
}

func TestWatchedDownloadCleanupAfterRestart(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("checks precise elapsed time with the simulated clock")
	}
	const policy = "enabled-before-restart"
	finished := time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC)
	completed := watchedDownloadFixture("fixture", "watched", "Watched")
	completed.Completion = &downloadCompletion{At: finished.UnixNano(), Policy: policy}
	legacy := watchedDownloadFixture("fixture", "legacy", "Older download")
	legacy.Item.Watched = true
	invalid := watchedDownloadFixture("fixture", "invalid", "Invalid completion")
	invalid.Completion = &downloadCompletion{Policy: policy}

	t.Run("24 hours", func(t *testing.T) {
		deadline := finished.Add(downloadCleanupAge)
		app, dir := startWatchedDownloadApp(t, deadline.Add(-time.Second), core.AppearanceLight,
			map[string]string{downloadCleanupKey: policy}, completed, legacy, invalid)
		app.FastForward(deadline.Sub(app.Clock.WallTime()) - time.Nanosecond)
		assertWatchedDownloadFile(t, dir, completed.ID, true)
		app.FastForward(time.Nanosecond)
		app.WaitForGone("id:download-play-"+completed.ID, 3*time.Second)
		assertWatchedDownloadFile(t, dir, completed.ID, false)
		assertWatchedDownloadFile(t, dir, legacy.ID, true)
		assertWatchedDownloadFile(t, dir, invalid.ID, true)
	})

	t.Run("disabled and enabled again", func(t *testing.T) {
		app, dir := startWatchedDownloadApp(t, finished.Add(time.Hour), core.AppearanceLight,
			map[string]string{downloadCleanupKey: policy}, completed)
		app.Press(deleteWatchedSwitch)
		app.FastForward(25 * time.Hour)
		assertWatchedDownloadFile(t, dir, completed.ID, true)
		app.Press(deleteWatchedSwitch)
		app.FastForward(time.Hour)
		assertWatchedDownloadFile(t, dir, completed.ID, true)
	})
}

func TestWatchedDownloadExpiresInOpenDetails(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("checks the detail change with the simulated clock")
	}
	provider := source{ID: "offline-fixture", Provider: media.Emby, Address: "https://offline.example.test", Name: "Emby",
		AccountID: "viewer", Libraries: []sourceLibrary{{Library: media.Library{ID: "movies", Kind: "movies"}}}}
	movie := watchedDownloadFixture(mediaAccountKey(provider), "watched", "Watched offline")
	movie.Item.LibraryID = "movies"
	now := time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC)
	movie.Completion = &downloadCompletion{At: now.Add(time.Minute - downloadCleanupAge).UnixNano(), Policy: "enabled"}
	app, dir := startWatchedDownloadApp(t, now, core.AppearanceLight, map[string]string{
		downloadCleanupKey: "enabled", sourcesKey: downloadFixtureJSON(t, []source{provider}),
	}, movie)
	app.Press("id:back")
	app.Press("id:tab-library")
	app.WaitFor("id:download-badge-"+movie.Item.ID, 3*time.Second)
	app.Press("id:title-" + movie.Item.ID)
	app.WaitFor("id:detail-play", 3*time.Second)
	app.FastForward(time.Minute + time.Second)
	app.WaitUntil(3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(dir, "downloads", movie.ID))
		return os.IsNotExist(err)
	})
	app.Press("id:detail-play")
	app.WaitFor("text:登录未通过，请检查账号密码或访问令牌。", 3*time.Second)
}

func TestStorageWatchedCleanupReview(t *testing.T) {
	if nsgotest.Target() != "linux" {
		t.Skip("design captures use the Linux GTK backend")
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			movie := watchedDownloadFixture("fixture", "movie", "Waygu Playback Test")
			app, _ := startWatchedDownloadApp(t, time.Now(), theme.appearance, nil, movie)
			app.Press(deleteWatchedSwitch)
			const includeTracks = "id:download-include-tracks"
			if !app.Find(includeTracks).Props.Checked {
				t.Fatal("new downloads should include optional tracks by default")
			}
			app.Press(includeTracks)
			if value, _ := app.Storage().GetItem(downloadTracksKey); value != "false" {
				t.Fatalf("disabling subtitle and danmaku downloads saved %q", value)
			}
			app.Press(includeTracks)
			app.Advance(350 * time.Millisecond)
			app.Screenshot("storage-watched-cleanup-" + theme.name)
		})
	}
}

func watchedDownloadFixture(account, id, title string) downloadEntry {
	return downloadEntry{ID: downloadID(account, id), Account: account, SourceName: "Emby", State: downloadComplete,
		Item: media.Item{ID: id, Title: title, Kind: media.Movie, Duration: 3 * time.Second}}
}

func startWatchedDownloadApp(t *testing.T, now time.Time, theme core.Appearance, config map[string]string, entries ...downloadEntry) (*nsgotest.App, string) {
	return startDownloadTestApp(t, now, theme, config, entries)
}

func startDownloadTestApp(t *testing.T, now time.Time, theme core.Appearance, config map[string]string, entries []downloadEntry, options ...nsgotest.Option) (*nsgotest.App, string) {
	t.Helper()
	if config == nil {
		config = make(map[string]string)
	}
	config[seenKey] = "true"
	config[downloadQueueKey] = downloadFixtureJSON(t, entries)
	var dataDir string
	height := core.Points(1380)
	if nsgotest.Target() == "linux" {
		height += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetSubtreeAppearance(theme)
		dataDir = cx.DataDir()
		manifest := downloads.File{Extension: ".mp4", Size: int64(len(nativePlaybackFixture)),
			SHA256: fmt.Sprintf("%x", sha256.Sum256(nativePlaybackFixture)), Height: 90}
		for _, entry := range entries {
			dir := filepath.Join(dataDir, "downloads", entry.ID)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "video.mp4"), nativePlaybackFixture, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "complete.json"), []byte(downloadFixtureJSON(t, manifest)), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return Root(cx)
	}, append([]nsgotest.Option{nsgotest.Size(390, height), nsgotest.Idiom(core.IdiomPhone), nsgotest.WallTime(now),
		nsgotest.Environment(core.Environment{Locale: "zh-Hans"}), nsgotest.Storage(config)}, options...)...)
	app.Press("id:tab-settings")
	if nsgotest.Target() == "android" {
		app.Scroll("id:phone-settings-home", 440)
	}
	app.Press("id:settings-downloads")
	app.WaitFor(deleteWatchedSwitch, 3*time.Second)
	if nsgotest.Target() == "android" {
		app.Scroll("id:storage-downloads-scroll", 350)
	}
	return app, dataDir
}

func downloadFixtureJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func findWatchedDownload(t *testing.T, app *nsgotest.App, id string) downloadEntry {
	t.Helper()
	raw, _ := app.Storage().GetItem(downloadQueueKey)
	var entries []downloadEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("download %s is missing", id)
	return downloadEntry{}
}

func assertWatchedDownloadFile(t *testing.T, dir, id string, exists bool) {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, "downloads", id, "video.mp4"))
	if exists && err != nil || !exists && !os.IsNotExist(err) {
		t.Fatalf("download %s exists=%t, want %t: %v", id, err == nil, exists, err)
	}
}
