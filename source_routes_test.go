package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

type routeFixture struct {
	server     *httptest.Server
	off        atomic.Bool
	denyVideo  atomic.Bool
	denied     atomic.Int64
	videoReads atomic.Int64
}

func newRouteFixture(t *testing.T, upstream string) *routeFixture {
	t.Helper()
	endpoint, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	route := new(routeFixture)
	proxy := httputil.NewSingleHostReverseProxy(endpoint)
	direct := proxy.Director
	proxy.Director = func(request *http.Request) {
		direct(request)
		request.Host = endpoint.Host
		request.Header["X-Forwarded-For"] = nil
	}
	route.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if route.off.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if request.Method == http.MethodGet && (strings.Contains(request.URL.Path, "/stream") || strings.Contains(request.URL.Path, "/library/parts/")) {
			if route.denyVideo.Load() {
				route.denied.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			route.videoReads.Add(1)
		}
		proxy.ServeHTTP(w, request)
	}))
	t.Cleanup(route.server.Close)
	return route
}

func fixtureSourceRoute(t *testing.T, config map[string]string, provider string) (source, *routeFixture, *routeFixture) {
	t.Helper()
	var entries []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.ID != provider {
			continue
		}
		client, err := media.NewClient(entry.Provider, entry.Address, media.NewDevice("Source route test"), media.Session{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		info, err := client.Probe(ctx)
		if err != nil || info.ID == "" {
			t.Fatalf("public identity from %s: %v", provider, err)
		}
		primary, backup := newRouteFixture(t, entry.Address), newRouteFixture(t, entry.Address)
		entry.ServerID = info.ID
		entry.Address, entry.RemoteAddress = primary.server.URL, backup.server.URL
		return entry, primary, backup
	}
	t.Fatalf("fixture source %s not configured", provider)
	return source{}, nil, nil
}

func TestDockerSourceRoutesForProviders(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires local Docker media fixtures")
	}
	config, secrets := homeReviewSources(t)
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			entry, primary, backup := fixtureSourceRoute(t, config, provider)
			_, movie := reviewFixtureItem(t, config, secrets, provider, "Waygu Test Movie")
			a := application{newMedia: media.NewClient, device: media.NewDevice("Route test")}
			a.tokens.Store(entry.credentialKey(), secrets[entry.ID])
			a.routesEnabled.Store(true)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			for _, remote := range []bool{false, true, false} {
				primary.off.Store(remote)
				client, endpoint, err := a.selectSourceClient(ctx, entry, "")
				if err != nil || endpoint.remote != remote {
					t.Fatalf("routing remote=%t: %v", remote, err)
				}
				stream, err := client.Playback(ctx, movie, media.PlaybackOptions{})
				if err != nil {
					t.Fatal(err)
				}
				gateway := primary
				if remote {
					gateway = backup
				}
				if !strings.HasPrefix(stream.File.URL, gateway.server.URL+"/") {
					t.Fatal("the file URL did not use the selected gateway")
				}
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, stream.File.URL, nil)
				if err != nil {
					t.Fatal(err)
				}
				request.Header = stream.File.Headers.Clone()
				if request.Header == nil {
					request.Header = make(http.Header)
				}
				request.Header.Set("Range", "bytes=0-31")
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				data, readErr := io.ReadAll(io.LimitReader(response.Body, 33))
				response.Body.Close()
				if readErr != nil || response.StatusCode != http.StatusPartialContent || len(data) != 32 || !strings.Contains(string(data), "ftyp") {
					t.Fatalf("media through selected gateway: status %d, bytes %d, error %v", response.StatusCode, len(data), readErr)
				}
				if remote {
					entry.RemoteBitrate = 300_000
					stream, err := client.Playback(ctx, movie, media.PlaybackOptions{MaxBitrate: entry.capBitrate(endpoint, 0)})
					if err != nil || stream.Method != media.Transcode {
						t.Fatalf("backup bitrate should request transcoding: %v, %v", stream.Method, err)
					}
				}
			}
		})
	}
}

func wrongIdentityFixture(t *testing.T, secretSeen *atomic.Bool) *httptest.Server {
	t.Helper()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/System/Info/Public" || request.Header.Get("X-Emby-Token") != "" ||
			strings.Contains(request.Header.Get("Authorization"), "Token=") || strings.Contains(request.Header.Get("X-Emby-Authorization"), "Token=") ||
			request.URL.Query().Get("api_key") != "" {
			secretSeen.Store(true)
		}
		_, _ = io.WriteString(w, `{"Id":"different","ServerName":"Unrelated fixture"}`)
	}))
	t.Cleanup(other.Close)
	return other
}

func TestUnverifiedBackupDoesNotReceiveCredentials(t *testing.T) {
	var seen atomic.Bool
	other := wrongIdentityFixture(t, &seen)
	offline := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(offline.Close)
	a := application{newMedia: media.NewClient, device: media.NewDevice("Route identity test")}
	a.routesEnabled.Store(true)
	entry := source{ID: "source", Provider: media.Emby, Address: offline.URL, RemoteAddress: other.URL, ServerID: "expected", AccountID: "viewer"}
	a.tokens.Store(entry.credentialKey(), "fixture-private-token")
	if _, err := a.client(t.Context(), entry); !errors.Is(err, errDifferentServer) || seen.Load() {
		t.Fatalf("backup identity was not enforced: %v, secret sent=%t", err, seen.Load())
	}
}

func TestDockerPhoneSourceFailover(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires local Docker media fixtures")
	}
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("native snapshots use the Linux GTK host")
	}
	config, secrets := homeReviewSources(t)
	_, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Test Movie")
	entry, primary, backup := fixtureSourceRoute(t, config, "emby")
	alternate := entry.RemoteAddress
	entry.RemoteAddress = ""
	catalog, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(catalog)
	var secretSeen atomic.Bool
	other := wrongIdentityFixture(t, &secretSeen)
	height := core.Points(1000)
	if target == "linux" {
		height += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	settings := func() {
		app.Press("id:tab-settings")
		app.Press("id:open-phone-source-settings")
	}
	settings()
	app.Press("id:source-edit-emby")
	app.WaitFor("id:server-remote-address", time.Second)
	app.Type("id:server-remote-address", other.URL)
	app.Press("id:save-backup-address")
	app.WaitFor("text:两个地址必须指向同一台媒体服务器。", 4*time.Second)
	if secretSeen.Load() {
		t.Fatal("an unverified route received credentials")
	}
	app.Clear("id:server-remote-address")
	app.Type("id:server-remote-address", alternate)
	if target == "linux" {
		app.Screenshot("phone-edit-backup-address")
	}
	app.Press("id:save-backup-address")
	const status = "id:source-settings-status-emby"
	app.WaitFor(status, 4*time.Second)
	app.WaitUntil(4*time.Second, func() bool { return app.Find(status).Text == "在线 · 局域网" })
	app.Press("id:back")
	app.Press("id:tab-search")
	app.Type("id:search-query", movie.Title)
	app.Press("id:search-submit")
	card := "id:title-" + movie.ID
	app.WaitFor(card, 8*time.Second)
	app.Press(card)
	app.WaitFor("id:detail-play", 5*time.Second)
	if target == "linux" {
		primary.denyVideo.Store(true)
	}
	app.Press("id:detail-play")
	app.Resize(844, 390)
	const video = "id:video-player"
	if target == "" {
		old := app.WaitFor(video, 8*time.Second)
		app.Play(video, core.VideoInfo{Duration: 18 * time.Second})
		app.Progress(video, 7*time.Second, 18*time.Second)
		primary.off.Store(true)
		app.Report(nsgotest.Event{Kind: nsgotest.EventError, ID: old.ID, Text: "fixture gateway disconnected"})
		app.WaitUntil(8*time.Second, func() bool { return len(app.Query(video)) == 1 && app.Find(video).ID != old.ID })
		app.Play(video, core.VideoInfo{Duration: 18 * time.Second})
		mounted := app.FindMounted(video)
		if len(mounted.Seeks) == 0 || mounted.Seeks[len(mounted.Seeks)-1] != 7*time.Second || mounted.Video.Paused {
			t.Fatalf("new playback did not seek to the old position: %v", mounted.Seeks)
		}
	} else {
		app.WaitUntil(12*time.Second, func() bool {
			return primary.denied.Load() > 0 && backup.videoReads.Load() > 0 &&
				len(app.Query("id:player-position")) == 1 && app.Find("id:player-position").Text >= "00:00:03"
		})
		primary.off.Store(true)
		primary.denyVideo.Store(false)
	}
	app.Press("id:close-player")
	app.Resize(390, height)
	app.Press("id:back")
	settings()
	app.Press("id:source-sync-emby")
	app.WaitUntil(5*time.Second, func() bool { return app.Find(status).Text == "在线 · 外网" })
	if target == "linux" {
		app.Screenshot("phone-source-online-remote")
	}
	app.Press("id:source-settings-failover")
	app.WaitUntil(5*time.Second, func() bool { return app.Find(status).Text == "离线" })
	if stored, _ := app.Storage().GetItem(sourceFailoverKey); stored != "false" {
		t.Fatal("the master failover switch was not saved")
	}
	app.Press("id:source-settings-failover")
	app.WaitUntil(5*time.Second, func() bool { return app.Find(status).Text == "在线 · 外网" })
	primary.off.Store(false)
	app.Press("id:source-sync-emby")
	app.WaitUntil(5*time.Second, func() bool { return app.Find(status).Text == "在线 · 局域网" })
}

func TestDockerDesktopSourceRouteReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires local Docker media fixtures")
	}
	config, secrets := homeReviewSources(t)
	entry, primary, _ := fixtureSourceRoute(t, config, "emby")
	primary.off.Store(true)
	var catalog []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &catalog); err != nil {
		t.Fatal(err)
	}
	catalog[0] = entry
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(data)
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			config[appearanceKey] = theme.name
			config[desktopPreferencesKey] = `{"Language":"zh-Hans"}`
			app := startDesktopSettingsReview(t, "zh-Hans", config, secrets)
			app.Key("mod+,")
			window := app.Surface("title:设置")
			window.Press("id:desktop-settings-tab-sources")
			const status = "id:source-settings-status-emby"
			app.WaitUntil(6*time.Second, func() bool { return window.Find(status).Text == "外网" })
			window.Press("id:desktop-source-remote-bitrate")
			window.Press("role:option 20 Mbps")
			var saved []source
			value, _ := app.Storage().GetItem(sourcesKey)
			if err := json.Unmarshal([]byte(value), &saved); err != nil || saved[0].RemoteBitrate != 20_000_000 {
				t.Fatalf("desktop bitrate did not persist: %v", err)
			}
			if nsgotest.Target() != "" {
				app.Advance(350 * time.Millisecond)
				window.Screenshot(fmt.Sprintf("desktop-sources-route-%s", theme.name))
			}
			window.Press("id:desktop-source-auto-route")
			app.WaitUntil(6*time.Second, func() bool { return window.Find(status).Text == "离线" })
		})
	}
}
