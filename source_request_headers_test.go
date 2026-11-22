package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestSourceRequestHeadersStayOnTheirOrigin(t *testing.T) {
	var seen atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Header.Get("X-Proxy-Key") != "")
		_, _ = w.Write([]byte(`{"Id":"local","ServerName":"Fixture"}`))
	}))
	defer other.Close()
	var redirect atomic.Bool
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Proxy-Key") != "review-only" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if redirect.Load() {
			http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write([]byte(`{"Id":"local","ServerName":"Fixture"}`))
	}))
	defer primary.Close()
	a := application{newMedia: media.NewClient, device: media.NewDevice("Headers test")}
	entry := source{Provider: media.Emby, Address: primary.URL, RemoteAddress: other.URL,
		headerDraft: http.Header{"X-Proxy-Key": {"review-only"}}}
	primaryClient, err := a.sourceClient(entry, primary.URL, media.Session{}, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := primaryClient.Probe(ctx); err != nil {
		t.Fatal("primary probe:", err)
	}
	redirect.Store(true)
	if _, err := primaryClient.Probe(ctx); err == nil || seen.Load() {
		t.Fatal("the custom-header request followed a cross-origin redirect")
	}
	backup, err := a.sourceClient(entry, other.URL, media.Session{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Probe(ctx); err != nil || seen.Load() {
		t.Fatalf("backup request used headers from the primary origin: %v", err)
	}
}

func TestDockerSourceAdvancedConnection(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the local Emby fixture")
	}
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("native capture uses Linux GTK")
	}
	config, tokens := homeReviewSources(t)
	entry := tlsFixtureBackend(t, config, "emby")
	client, movie := reviewFixtureItem(t, config, tokens, "emby", "Waygu Track Test")
	libraries, err := client.Libraries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var nonMovieLibrary string
	expectedCounts := make(map[string]string, len(libraries))
	for _, library := range libraries {
		if library.Kind == "tvshows" {
			nonMovieLibrary = library.ID
		}
		page, err := client.Catalog(t.Context(), media.Query{LibraryID: library.ID, Limit: 100})
		if err != nil || len(page.Items) != page.Total {
			t.Fatalf("read complete test library: %v", err)
		}
		expectedCounts[library.ID] = strconv.Itoa(len(page.Items)) + " 部"
	}
	if nonMovieLibrary == "" {
		t.Fatal("fixture needs a TV library to test preservation of selections")
	}
	backend, err := url.Parse(entry.Address)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	var blocked atomic.Bool
	var probes, logins, video, artwork atomic.Int64
	const required = "gateway-fixture-alpha"
	const secondary = "gateway-fixture-beta"
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Waygu-Gateway") != required || r.Header.Get("X-Waygu-Site") != secondary {
			http.Error(w, "missing gateway credentials", http.StatusForbidden)
			return
		}
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/System/Info/Public"):
			probes.Add(1)
			if blocked.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		case strings.HasSuffix(path, "/Users/AuthenticateByName"):
			logins.Add(1)
		case strings.Contains(path, "/Images/"):
			artwork.Add(1)
		case r.Method == http.MethodGet && strings.Contains(path, "/stream"):
			video.Add(1)
		}
		proxy.ServeHTTP(w, r)
	}))
	defer gate.Close()
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			var secure core.Secrets
			var h core.Points = 1240
			if target == "linux" {
				h += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				secure = cx.Secrets()
				return Root(cx)
			}, nsgotest.Size(390, h), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))
			app.Press("id:add-source")
			app.Press("id:choose-emby")
			host, port, _ := splitServerAddress(gate.URL)
			app.Type("id:server-address", host)
			app.Type("id:server-port", port)
			app.Press("id:test-connection")
			app.WaitFor("text:服务器拒绝了连接测试", 5*time.Second)

			app.Press("id:source-request-headers")
			app.WaitFor("id:source-header-name-0", time.Second)
			app.Type("id:source-header-name-0", "Host")
			app.Type("id:source-header-value-0", "example.invalid")
			app.Press("id:source-header-apply")
			app.WaitFor("text:请填写唯一", time.Second)
			app.Clear("id:source-header-name-0")
			app.Clear("id:source-header-value-0")
			app.Type("id:source-header-name-0", "X-Waygu-Gateway")
			app.Type("id:source-header-value-0", required)
			app.Press("id:source-header-add")
			app.Type("id:source-header-name-1", "x-waygu-gateway")
			app.Type("id:source-header-value-1", secondary)
			app.Press("id:source-header-apply")
			app.WaitFor("text:请填写唯一", time.Second)
			app.Clear("id:source-header-name-1")
			app.Type("id:source-header-name-1", "X-Waygu-Site")
			if target == "linux" {
				app.Screenshot("source-headers-editor-" + variant.name)
			}
			app.Press("id:source-header-apply")
			app.WaitFor("text:2 项", time.Second)
			app.Press("id:test-connection")
			app.WaitFor("id:connection-detail", 5*time.Second)
			if detail := app.Find("id:connection-detail").Text; !strings.Contains(detail, "延迟") || !strings.Contains(detail, "局域网") {
				t.Fatalf("unexpected connection details %q", detail)
			}
			polls, auth := probes.Load(), logins.Load()
			app.Press("id:retest-connection")
			app.WaitUntil(5*time.Second, func() bool {
				return probes.Load() > polls && !app.Find("id:retest-connection").Props.Disabled
			})
			app.WaitFor("id:connection-verified", 5*time.Second)
			if logins.Load() != auth {
				t.Fatal("the initial connection test signed into the server")
			}

			password := os.Getenv("WAYGU_TEST_PASSWORD")
			if password == "" {
				password = "waygu-fixture-local"
			}
			app.Type("id:server-name", "Waygu Emby")
			app.Type("id:server-username", "waygu-test")
			app.Type("id:server-password", password)
			app.Press("id:sign-in")
			app.WaitFor("id:import-libraries", 6*time.Second)
			for id, want := range expectedCounts {
				count := app.WaitFor("id:import-library-count-"+id, 6*time.Second)
				if count.Text != want {
					t.Fatalf("library count on the connection form = %q, want %q", count.Text, want)
				}
			}
			if target == "linux" {
				app.Screenshot("source-advanced-" + variant.name)
			}
			optional := "id:import-library-" + nonMovieLibrary
			app.Press(optional)
			checked := app.Find(optional).Props.Checked
			if target == "linux" {
				app.Screenshot("source-import-" + variant.name)
				app.Press("id:import-library-options-" + nonMovieLibrary)
				app.WaitFor("id:library-category-"+nonMovieLibrary, 3*time.Second)
				app.Screenshot("source-import-options-" + variant.name)
				app.Press("id:import-library-options-cancel")
			}
			auth = logins.Load()
			blocked.Store(true)
			app.Press("id:retest-connection")
			app.WaitFor("text:无法读取服务器数据", 5*time.Second)
			if !app.Find("id:import-libraries").Props.Disabled {
				t.Fatal("import remained enabled after the connection failed")
			}
			blocked.Store(false)
			app.Press("id:retest-connection")
			app.WaitFor("id:connection-verified", 5*time.Second)
			if logins.Load() != auth || app.Find(optional).Props.Checked != checked {
				t.Fatal("retesting reset authentication or the library selection")
			}
			app.Press("id:import-libraries")
			app.WaitFor("id:tab-search", 6*time.Second)
			raw, _ := app.Storage().GetItem(sourcesKey)
			var entries []source
			if json.Unmarshal([]byte(raw), &entries) != nil || len(entries) != 1 || entries[0].HeaderSecret == "" ||
				strings.Contains(raw, required) || strings.Contains(raw, secondary) {
				t.Fatal("the source did not store a credential reference independently of its private values")
			}
			secret, found, err := secure.Get(entries[0].HeaderSecret)
			if err != nil || !found || !strings.Contains(secret, required) || !strings.Contains(secret, secondary) {
				t.Fatal("the source request headers were not written to the secure store")
			}
			videoBefore, artworkBefore := video.Load(), artwork.Load()
			app.Press("id:tab-search")
			app.Type("id:search-query", movie.Title)
			app.Press("id:search-submit")
			card := "id:title-" + movie.ID
			app.WaitFor(card, 8*time.Second)
			app.Press(card)
			app.WaitFor("id:detail-play", 6*time.Second)
			app.Press("id:detail-play")
			app.Resize(844, 390)
			app.WaitFor("id:video-player", 8*time.Second)
			if target == "linux" {
				app.WaitUntil(8*time.Second, func() bool {
					return video.Load() > videoBefore && artwork.Load() > artworkBefore && app.Find("id:player-position").Text > "00:00"
				})
			}
			app.Press("id:close-player")
			if target == "" {
				app.Resize(390, h)
				app.Press("id:back")
				app.Press("id:tab-settings")
				app.Press("id:open-phone-source-settings")
				app.Press("id:source-edit-" + entries[0].ID)
				app.WaitFor("text:2 项", 5*time.Second)
				app.Press("id:source-request-headers")
				app.Press("id:source-header-remove-0")
				app.Press("id:source-header-remove-1")
				app.Press("id:source-header-apply")
				app.Press("id:save-source-headers")
				app.WaitFor("id:source-edit-"+entries[0].ID, 5*time.Second)
				updated, _ := app.Storage().GetItem(sourcesKey)
				var records []source
				if json.Unmarshal([]byte(updated), &records) != nil || len(records) != 1 || records[0].HeaderSecret != "" {
					t.Fatal("clearing the saved request headers left a credential reference")
				}
				app.WaitUntil(3*time.Second, func() bool {
					_, found, err := secure.Get(entries[0].HeaderSecret)
					return err == nil && !found
				})
			}
		})
	}
}
