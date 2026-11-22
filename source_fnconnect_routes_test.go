package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestFlymooFNRouteRefreshSharesLookup(t *testing.T) {
	const grant = "fn_gate=old-fixture"
	offline := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != grant || r.Header.Get("Authorization") != "" {
			t.Error("original gateway should get only its scoped grant")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(offline.Close)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("a candidate with a different identity received something other than the public probe")
		}
		if r.URL.Path == "/v/api/v1/sys/version" {
			_, _ = io.WriteString(w, `{"code":0,"data":{"version":"0.9.1"}}`)
			return
		}
		if r.URL.Path != "/v/api/v1/sys/config" {
			t.Errorf("unexpected request to unrelated NAS: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"server_guid":"different-nas"}}`)
	}))
	t.Cleanup(other.Close)
	replacement := newFlymooGatewayFixture(t, false, "/v", nil, nil)
	entry := source{ID: "fn", Provider: media.Flymoo, Address: offline.URL + "/v", FNID: "living-room7",
		ServerID: "nas-review", AccountID: "viewer-id", HeaderSecret: "gateway"}
	address := replacement.server.URL + "/v"
	var lookups atomic.Int64
	a := application{lifetime: t.Context(), newMedia: media.NewClient, device: media.NewDevice("FN route test")}
	a.refreshFNConnect = func(ctx context.Context, id, expected string, allowed func(string) bool, probe func(context.Context, string) (media.ServerInfo, error)) (media.FNConnectRoute, error) {
		lookups.Add(1)
		if id != entry.FNID || expected != entry.ServerID || allowed(entry.Address) || !allowed(address) {
			t.Error("refresher received an incorrect FN identity or route filter")
		}
		if _, err := probe(ctx, other.URL+"/v"); !errors.Is(err, errDifferentServer) {
			t.Errorf("public probe of unrelated NAS = %v", err)
		}
		if _, err := probe(ctx, address); err != nil {
			return media.FNConnectRoute{}, err
		}
		return media.FNConnectRoute{Address: address, Remote: true}, nil
	}
	a.routesEnabled.Store(true)
	a.artworkSources([]source{entry})
	defer a.artworkSources(nil)
	a.tokens.Store(entry.credentialKey(), reviewFlymooToken)
	a.sourceHeaders.Store(entry.HeaderSecret, http.Header{"Cookie": {grant}})

	var checks sync.WaitGroup
	for range 8 {
		checks.Go(func() {
			client, endpoint, err := a.selectSourceClient(t.Context(), entry, "")
			if err != nil || endpoint.address != address || !endpoint.remote {
				t.Errorf("resolved endpoint = %+v, %v", endpoint, err)
				return
			}
			libraries, err := client.Libraries(t.Context())
			if err != nil || len(libraries) != 1 || libraries[0].ID != "movies" {
				t.Errorf("libraries from refreshed route = %v, %v", libraries, err)
			}
		})
	}
	checks.Wait()
	if lookups.Load() != 1 {
		t.Fatalf("shared FN portal lookups = %d, want 1", lookups.Load())
	}
	if fnRouteAllowed(source{Address: "https://saved.example.test/v"}, nil, address) {
		t.Fatal("an HTTPS source can downgrade to HTTP")
	}
	a.routesEnabled.Store(false)
	a.artworkSources([]source{entry})
	endpoint, err := a.resolveSource(t.Context(), entry, "")
	if err != nil || endpoint.address != entry.Address || a.cachedFNRoute(entry).address != "" {
		t.Fatalf("disabled failover reused refreshed route = %+v, %v", endpoint, err)
	}
}

func TestFlymooFNRouteRecoveryReview(t *testing.T) {
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("native review uses Linux GTK")
	}
	var clip, poster []byte
	if target == "linux" {
		path := os.Getenv("WAYGU_TEST_FLYMOO_CLIP")
		if path == "" {
			t.Skip("set WAYGU_TEST_FLYMOO_CLIP to the generated Track Test fixture")
		}
		var err error
		clip, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		poster, err = os.ReadFile(filepath.Join(filepath.Dir(path), "fanart.jpg"))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			original := newFlymooGatewayFixture(t, false, "/v", clip, poster)
			replacement := newFlymooGatewayFixture(t, false, "/v", clip, poster)
			entry := source{ID: "flymoo", Provider: media.Flymoo, Address: original.server.URL + "/v", FNID: "living-room7",
				Name: "飞牛测试 NAS", ServerID: "nas-review", AccountID: "viewer-id", Username: "viewer",
				Libraries: []sourceLibrary{{Library: media.Library{ID: "movies", Name: "Movies", Kind: "movies"}}}}
			address := replacement.server.URL + "/v"
			data, err := json.Marshal([]source{entry})
			if err != nil {
				t.Fatal(err)
			}
			height := core.Points(1000)
			if target == "linux" {
				height = 1357
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(theme.appearance)
				fnConnectRefresher.Provide(cx, func(ctx context.Context, _, _ string, _ func(string) bool,
					probe func(context.Context, string) (media.ServerInfo, error)) (media.FNConnectRoute, error) {
					_, err := probe(ctx, address)
					return media.FNConnectRoute{Address: address, Remote: true}, err
				})
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(map[string]string{sourcesKey: string(data)}),
				nsgotest.Secrets(map[string]string{entry.ID: reviewFlymooToken}))
			app.Press("id:tab-search")
			app.Type("id:search-query", "Waygu Flymoo Review")
			app.Press("id:search-submit")
			app.WaitFor("id:title-movie-1", 5*time.Second)
			app.Press("id:title-movie-1")
			app.WaitFor("id:detail-play", 5*time.Second)
			if target == "linux" {
				original.denyVideo.Store(true)
			}
			app.Press("id:detail-play")
			app.Resize(844, 390)
			const video = "id:video-player"
			if target == "" {
				old := app.WaitFor(video, 8*time.Second)
				app.Play(video, core.VideoInfo{Duration: 24 * time.Second})
				app.Progress(video, 7*time.Second, 24*time.Second)
				original.offline.Store(true)
				app.Report(nsgotest.Event{Kind: nsgotest.EventError, ID: old.ID, Text: "original FN route disconnected"})
				app.WaitUntil(8*time.Second, func() bool { return len(app.Query(video)) == 1 && app.Find(video).ID != old.ID })
				app.Play(video, core.VideoInfo{Duration: 24 * time.Second})
				mounted := app.FindMounted(video)
				if len(mounted.Seeks) == 0 || mounted.Seeks[len(mounted.Seeks)-1] != 7*time.Second || mounted.Video.Paused {
					t.Fatalf("refreshed playback did not resume at 7 seconds: %v", mounted.Seeks)
				}
			} else {
				app.WaitUntil(12*time.Second, func() bool {
					return original.offlineReads.Load() > 0 && replacement.video.Load() > 0 &&
						len(app.Query("id:player-position")) == 1 && app.Find("id:player-position").Text >= "00:00:03"
				})
				original.offline.Store(true)
			}
			app.Press("id:close-player")
			app.Resize(390, height)
			app.Press("id:back")
			app.Press("id:tab-settings")
			app.Press("id:open-phone-source-settings")
			const status = "id:source-settings-status-flymoo"
			app.WaitFor(status, 4*time.Second)
			app.Press("id:source-sync-flymoo")
			app.WaitUntil(5*time.Second, func() bool { return app.Find(status).Text == "在线 · 外网" && app.Exists("text:"+address) })
			if target == "linux" {
				app.Screenshot("phone-source-fn-refreshed-" + theme.name)
			}
			raw, _ := app.Storage().GetItem(sourcesKey)
			var saved []source
			if json.Unmarshal([]byte(raw), &saved) != nil || len(saved) != 1 ||
				saved[0].Address != entry.Address || saved[0].RemoteAddress != "" || saved[0].FNID != entry.FNID {
				t.Fatal("runtime refresh overwrote the saved FN connection")
			}
			app.Press("id:source-edit-flymoo")
			app.Press("id:flymoo-fn-connect")
			if field := app.Find("id:flymoo-fn-id"); field.Text != entry.FNID || app.Find("id:flymoo-fn-code").Text != "" {
				t.Fatal("connection editor should prefill only the saved FN ID")
			}
		})
	}
}
