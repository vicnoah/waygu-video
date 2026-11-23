package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerSeasonDownloads(t *testing.T) {
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux") {
		t.Skip("requires Docker media fixtures and the headless or GTK player")
	}
	if target == "linux" {
		for _, theme := range reviewAppearances {
			t.Run(theme.name, func(t *testing.T) { testSeasonDownload(t, "emby", core.IdiomPhone, theme.appearance, false) })
		}
		return
	}
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) { testSeasonDownload(t, provider, core.IdiomPhone, core.AppearanceLight, false) })
	}
	t.Run("space", func(t *testing.T) { testSeasonDownload(t, "emby", core.IdiomPhone, core.AppearanceLight, true) })
	for _, layout := range []struct {
		name  string
		idiom core.Idiom
	}{{"desktop", core.IdiomDesktop}, {"tablet", core.IdiomTablet}, {"tv", core.IdiomTV}} {
		t.Run(layout.name, func(t *testing.T) { testSeasonDownload(t, "emby", layout.idiom, core.AppearanceLight, false) })
	}
}

func TestAndroidTVSeasonDownloads(t *testing.T) {
	if nsgotest.Target() != "android" || os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Android TV emulator and Docker media fixtures")
	}
	testSeasonDownload(t, "emby", core.IdiomTV, core.AppearanceDark, false)
}

func testSeasonDownload(t *testing.T, provider string, idiom core.Idiom, appearance core.Appearance, lowSpace bool) {
	t.Helper()
	config, secrets := dockerReviewSources(t, false)
	server, client, series, seasons := reviewSeriesFixture(t, config, secrets, provider)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	first, seasonOne := seriesFixtureEpisodes(t, ctx, client, seasons, 1, 5)
	second, seasonTwo := seriesFixtureEpisodes(t, ctx, client, seasons, 2, 2)
	cancel()
	address, err := url.Parse(server.Address)
	if err != nil {
		t.Fatal(err)
	}
	var disconnected atomic.Bool
	proxy := httputil.NewSingleHostReverseProxy(address)
	forward := proxy.Director
	proxy.Director = func(request *http.Request) {
		forward(request)
		request.Host = address.Host
		request.Header["X-Forwarded-For"] = nil
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.StatusCode >= 400 {
			t.Logf("fixture request %s returned HTTP %d", response.Request.URL.Path, response.StatusCode)
		}
		return nil
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if disconnected.Load() {
			http.Error(w, "fixture disconnected", http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(gateway.Close)
	server.Address = gateway.URL
	config[sourcesKey] = downloadFixtureJSON(t, []source{server})
	config[playbackPreferencesKey] = `{"DisableProgressRecording":true}`
	config[downloadSlotsKey] = "1"
	if nsgotest.Target() == "android" {
		config[downloadWiFiKey] = "false"
	}
	var dir string
	width, height := core.Points(390), core.Points(880)
	if idiom != core.IdiomPhone {
		width, height = 1280, 960
	}
	if nsgotest.Target() == "linux" {
		height += 37
	}
	space := int64(1 << 33)
	if lowSpace {
		space = 1
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, idiom)
		cx.SetSubtreeAppearance(appearance)
		dir = cx.DataDir()
		return Root(cx)
	}, nsgotest.Size(width, height), nsgotest.Idiom(idiom), nsgotest.Storage(config), nsgotest.Secrets(secrets),
		nsgotest.Environment(core.Environment{Locale: "zh-Hans"}), nsgotest.DiskSpace(core.DiskSpace{Available: space}, nil))
	if idiom == core.IdiomDesktop {
		app.Press("id:tab-library")
		app.Press("id:desktop-library-series")
	} else if idiom == core.IdiomTV {
		app.Press("id:tv-tab-search")
		app.Type("id:search-query", series.Title)
		app.Press("id:search-submit")
	} else if provider == "plex" {
		app.Press("id:tab-library")
		app.Press("id:library-category-series")
	} else {
		app.Press("id:tab-search")
		app.Type("id:search-query", series.Title)
		app.Press("id:search-submit")
	}
	app.WaitFor("id:title-"+series.ID, 8*time.Second)
	app.Press("id:title-" + series.ID)
	launch := func() {
		if idiom == core.IdiomPhone {
			app.Press("id:detail-hero-more")
		}
		app.WaitFor("id:season-download", 5*time.Second)
		app.Press("id:season-download")
		app.WaitFor("id:season-download-quality", 5*time.Second)
		app.Advance(300 * time.Millisecond)
	}
	if idiom == core.IdiomPhone {
		app.WaitFor("id:series-tab-"+second.ID, 5*time.Second)
		app.Press("id:series-tab-" + second.ID)
		app.WaitFor("id:child-"+seasonTwo[0].ID, 5*time.Second)
	}
	launch()
	episode := func(item media.Item) string { return "id:season-download-episode-" + item.ID }
	if idiom == core.IdiomPhone {
		app.WaitFor(episode(seasonTwo[0]), 5*time.Second)
		app.Press("id:season-download-season")
		app.WaitFor("role:option 第 1 季", 3*time.Second)
		app.Press("role:option 第 1 季")
		app.WaitForGone(episode(seasonTwo[0]), 5*time.Second)
	}
	other := 1
	if provider == "jellyfin" {
		other = 2
	}
	firstRow, secondRow := episode(seasonOne[0]), episode(seasonOne[other])
	app.WaitFor(firstRow, 5*time.Second)
	app.WaitFor("text:已选 5 集", 3*time.Second)
	app.Press("id:season-download-all")
	app.WaitFor("text:已选 0 集", 3*time.Second)
	if !app.Find("id:season-download-confirm").Props.Disabled {
		t.Fatal("a batch with no episodes could be submitted")
	}
	app.Press(firstRow)
	app.Press(secondRow)
	app.WaitFor("text:已选 2 集", 3*time.Second)
	if lowSpace {
		app.Press("id:season-download-quality-720")
		app.WaitFor("id:season-download-insufficient-space", 3*time.Second)
		if _, exists := app.Storage().GetItem(downloadQueueKey); exists || !app.Find("id:season-download-confirm").Props.Disabled {
			t.Fatal("the space check admitted a batch larger than the available disk")
		}
		return
	}
	chosenHeight := 0
	if provider == "jellyfin" {
		chosenHeight = 720
		app.Press("id:season-download-quality-720")
	}
	if nsgotest.Target() != "" {
		name := "light"
		if appearance == core.AppearanceDark {
			name = "dark"
		}
		if idiom == core.IdiomTV {
			name = "tv"
		}
		app.Screenshot("season-download-" + name)
	}
	app.Press("id:season-download-confirm")
	ids := []string{downloadID(mediaAccountKey(server), seasonOne[0].ID), downloadID(mediaAccountKey(server), seasonOne[other].ID)}
	var entries []downloadEntry
	app.WaitUntil(20*time.Second, func() bool {
		raw, _ := app.Storage().GetItem(downloadQueueKey)
		_ = json.Unmarshal([]byte(raw), &entries)
		if len(entries) != 2 || entries[0].ID != ids[0] || entries[1].ID != ids[1] {
			t.Fatalf("batch did not save only the two selected episodes in season order: %+v", entries)
		}
		for _, entry := range entries {
			if entry.Height != chosenHeight {
				t.Fatalf("episode %d queued with the wrong quality: %dp", entry.Item.Episode, entry.Height)
			}
			if entry.State == downloadFailed {
				t.Fatalf("episode %d download failed: %s", entry.Item.Episode, entry.Failure)
			}
			if entry.State != downloadComplete {
				return false
			}
		}
		return true
	})
	if idiom == core.IdiomPhone {
		app.Press("id:back")
		if !app.WaitFor("id:series-tab-"+second.ID, 5*time.Second).Props.Selected {
			t.Fatal("the previous season was lost after visiting the download queue")
		}
		app.Press("id:series-tab-" + first.ID)
		app.WaitFor("id:child-"+seasonOne[0].ID, 5*time.Second)
		launch()
		if !app.WaitFor(firstRow, 5*time.Second).Props.Disabled || !app.Find(secondRow).Props.Disabled {
			t.Fatal("a completed episode could be queued again")
		}
		app.WaitFor("text:已选 3 集", 3*time.Second)
		app.Press("role:button 取消")
		app.FastForward(350 * time.Millisecond)
		disconnected.Store(true)
		app.Press("id:child-" + seasonOne[0].ID)
	} else {
		disconnected.Store(true)
		app.Press("id:download-play-" + ids[0])
	}
	video := app.WaitFor("id:video-player", 10*time.Second)
	if !strings.HasPrefix(strings.TrimPrefix(video.Video.Source, "file://"), filepath.Join(dir, "downloads", ids[0])) {
		t.Fatalf("completed episode used %q instead of its local file", video.Video.Source)
	}
	if nsgotest.Target() != "" {
		app.WaitForGone("text:正在加载…", 8*time.Second)
		before := app.Find("id:player-position").Text
		app.WaitUntil(8*time.Second, func() bool { return app.Find("id:player-position").Text != before })
	}
}
