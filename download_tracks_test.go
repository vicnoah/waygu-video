package main

import (
	"encoding/json"
	"fmt"
	"io"
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

	"waygu-video/internal/danmaku"
	"waygu-video/internal/downloads"
)

func TestDockerOfflineDownloadTracks(t *testing.T) {
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux") {
		t.Skip("requires local Docker media servers and the headless or GTK player")
	}
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		for _, height := range []int{0, 720} {
			t.Run(fmt.Sprintf("%s/%d", provider, height), func(t *testing.T) {
				testDockerOfflineTracks(t, provider, height, true)
			})
		}
	}
	if target == "" {
		t.Run("disabled", func(t *testing.T) { testDockerOfflineTracks(t, "emby", 0, false) })
	}
}

func TestAndroidOfflineDownloadTracks(t *testing.T) {
	if nsgotest.Target() != "android" || os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires an Android phone emulator and the local Docker media servers")
	}
	testDockerOfflineTracks(t, "emby", 0, true)
}

func testDockerOfflineTracks(t *testing.T, provider string, height int, include bool) {
	t.Helper()
	target := nsgotest.Target()
	config, secrets := dockerReviewSources(t, false)
	fixture := "Waygu Test Movie"
	chinese, english := "理髮以後再出發。", "Waygu Video playback fixture"
	if provider == "jellyfin" || provider == "plex" && height > 0 {
		fixture, english = "Waygu Portable Captions Test", "The train leaves at dawn."
	}
	_, movie := reviewFixtureItem(t, config, secrets, provider, fixture)
	var configured []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &configured); err != nil {
		t.Fatal(err)
	}
	var selected source
	for _, entry := range configured {
		if entry.ID == provider {
			selected = entry
			break
		}
	}
	upstream, err := url.Parse(selected.Address)
	if err != nil || upstream.Host == "" {
		t.Fatalf("invalid fixture server: %v", err)
	}
	var disconnected atomic.Bool
	var serverOfflineCalls, danmakuCalls atomic.Int32
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	forward := proxy.Director
	proxy.Director = func(request *http.Request) {
		forward(request)
		request.Host = upstream.Host
		request.Header["X-Forwarded-For"] = nil
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if disconnected.Load() {
			serverOfflineCalls.Add(1)
			http.Error(w, "fixture disconnected", http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(gateway.Close)
	selected.Address = gateway.URL
	dandan := func(credentials danmaku.DandanCredentials) *danmaku.DandanClient {
		return danmaku.NewDandanClient(credentials, dandanTestTransport(func(request *http.Request) (*http.Response, error) {
			danmakuCalls.Add(1)
			if disconnected.Load() || request.URL.Path != "/api/v2/comment/421" || request.Header.Get("X-Signature") == "" {
				t.Errorf("unexpected danmaku request %s", request.URL.Path)
			}
			const body = `{"count":1,"comments":[{"p":"1,5,16777215,fixture","m":"Saved offline comment"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}))
	}
	state := map[string]string{
		sourcesKey:             downloadFixtureJSON(t, []source{selected}),
		subtitlePreferencesKey: `{"Languages":["zh-Hant","en"],"Mode":"always"}`,
		playbackPreferencesKey: `{"DisableProgressRecording":true}`,
		danmakuBindingKey(route{source: selected, item: movie}): downloadFixtureJSON(t, danmaku.DandanEpisode{ID: 421, Shift: 0.25}),
	}
	if target == "android" {
		state[downloadWiFiKey] = "false"
	}
	secrets[dandanCredentialKey] = `{"AppID":"offline-fixture","Secret":"private-dandan-fixture"}`
	var dataDir string
	start := func(storage, credentials map[string]string, previous string) *nsgotest.App {
		return nsgotest.Start(t, func(cx *core.Scope) core.Node {
			shellIdiom.Provide(cx, core.IdiomPhone)
			dataDir = cx.DataDir()
			if previous != "" {
				if err := os.CopyFS(dataDir, os.DirFS(previous)); err != nil {
					t.Fatal(err)
				}
			}
			return appRootWithDandan(cx, nil, nil, dandan)
		}, nsgotest.Size(390, 1417), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
			nsgotest.Idiom(core.IdiomPhone), nsgotest.Storage(storage), nsgotest.Secrets(credentials))
	}
	app := start(state, secrets, "")
	if !include {
		app.Press("id:tab-settings")
		app.Press("id:settings-downloads")
		app.Press("id:download-include-tracks")
		app.Press("id:back")
	}
	app.Press("id:tab-search")
	app.Type("id:search-query", movie.Title)
	app.Press("id:search-submit")
	card := "id:title-" + movie.ID
	app.WaitFor(card, 10*time.Second)
	app.Press(card)
	app.WaitFor("id:detail-download", 5*time.Second)
	app.Press("id:detail-download")
	if height > 0 {
		app.Press("id:detail-download-quality-" + fmt.Sprint(height))
	}
	app.Press("id:detail-download-confirm")
	app.FastForward(350 * time.Millisecond)
	app.WaitForGone("id:detail-download-quality", 3*time.Second)
	id := downloadID(mediaAccountKey(selected), movie.ID)
	app.WaitUntil(50*time.Second, func() bool {
		entry := findWatchedDownload(t, app, id)
		if entry.State == downloadFailed {
			t.Fatalf("download failed: %s", entry.Failure)
		}
		return entry.State == downloadComplete
	})
	store := downloads.Store{Dir: filepath.Join(dataDir, "downloads")}
	queue, _ := app.Storage().GetItem(downloadQueueKey)
	entry := findWatchedDownload(t, app, id)
	_, movieFile, err := store.Open(id)
	if err != nil {
		t.Fatal("the completed video was unavailable:", err)
	}
	tracks, err := store.ReadTracks(id)
	if !include {
		if !os.IsNotExist(err) || danmakuCalls.Load() != 0 || entry.IncludeTracks {
			t.Fatal("the disabled option still saved or requested supported tracks")
		}
		return
	}
	if err != nil || len(tracks.Subtitles) < 2 || tracks.Danmaku == nil || len(tracks.Danmaku.Comments) != 1 || tracks.Danmaku.Shift != 0.25 {
		t.Fatalf("missing offline tracks: %d subtitles, danmaku=%+v (%d requests), requested=%t, error=%v",
			len(tracks.Subtitles), tracks.Danmaku, danmakuCalls.Load(), entry.IncludeTracks, err)
	}
	if danmakuCalls.Load() != 1 || tracks.Danmaku.Comments[0].Text != "Saved offline comment" {
		t.Fatal("the bound danmaku was not downloaded")
	}
	if entry.Total != store.Bytes(id) || entry.Total <= movieFile.Size {
		t.Fatal("the completed size did not include subtitles and danmaku")
	}
	sidecar, err := os.ReadFile(filepath.Join(store.Dir, id, "tracks.json"))
	if token := secrets[provider]; err != nil || strings.Contains(string(sidecar), gateway.URL) || (token != "" && strings.Contains(string(sidecar), token)) ||
		strings.Contains(string(sidecar), "private-dandan-fixture") {
		t.Fatalf("downloaded tracks retained credentials or a remote URL: %v", err)
	}

	if target == "android" {
		app.Press("id:back")
	} else {
		disconnected.Store(true)
		app = start(map[string]string{seenKey: "true", downloadQueueKey: queue, subtitlePreferencesKey: state[subtitlePreferencesKey],
			playbackPreferencesKey: state[playbackPreferencesKey]}, nil, dataDir)
	}
	app.Press("id:tab-settings")
	if target == "android" {
		app.Scroll("id:phone-settings-home", 440)
	}
	app.Press("id:settings-downloads")
	app.WaitFor("id:download-play-"+id, 5*time.Second)
	disconnected.Store(true)
	app.Press("id:download-play-" + id)
	if target == "linux" {
		app.Resize(844, 427)
	} else if target == "" {
		app.Resize(844, 390)
	}
	app.WaitFor("id:video-player", 10*time.Second)
	if target == "" {
		app.Play("id:video-player", core.VideoInfo{Duration: 18 * time.Second})
		app.Progress("id:video-player", 2*time.Second, 18*time.Second)
	} else {
		resolution := app.WaitFor("id:phone-player-resolution", 10*time.Second)
		if target == "android" && resolution.Text != "360p" {
			t.Fatalf("the downloaded 360p fixture is labeled %q", resolution.Text)
		}
		const play = "id:phone-player-play-small"
		if !app.Exists(play) {
			app.Press("id:phone-player-surface")
		}
		if app.WaitFor(play, 3*time.Second).Props.Label == "暂停" {
			app.Press(play)
		}
		const rail = "id:phone-player-position-rail"
		width := app.Find(rail).Frame.Size.Width
		app.PressAt(rail, core.Point{X: width / 9, Y: 8})
	}
	if node := app.WaitFor("text:"+chinese, 8*time.Second); node.Props.TestID != "subtitle-caption" {
		t.Fatal("the saved Chinese subtitle did not render as the primary line")
	}
	if node := app.WaitFor("text:"+english, 3*time.Second); node.Props.TestID != "subtitle-caption-secondary" {
		t.Fatal("the saved English subtitle did not render as the secondary line")
	}
	app.WaitFor("id:danmaku-canvas", 3*time.Second)
	if serverOfflineCalls.Load() != 0 || danmakuCalls.Load() != 1 {
		t.Fatal("the offline player contacted a provider")
	}
	if target != "" {
		app.Advance(200 * time.Millisecond)
		app.Screenshot(fmt.Sprintf("offline-tracks-%s-%d", provider, height))
	}
}
