package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
	"waygu-video/internal/mediatls"
)

const reviewFlymooToken = "flymoo-review-token"

type flymooGatewayFixture struct {
	server       *httptest.Server
	public       atomic.Int64
	logins       atomic.Int64
	video        atomic.Int64
	images       atomic.Int64
	offline      atomic.Bool
	offlineReads atomic.Int64
}

func newFlymooGatewayFixture(t *testing.T, tls bool, prefix string, clip, poster []byte) *flymooGatewayFixture {
	t.Helper()
	gateway := new(flymooGatewayFixture)
	movie := fmt.Sprintf(`{"guid":"movie-1","title":"Waygu Flymoo Review","type":"Movie","mdb_guid":"movies","poster":%q,"backdrop":"fixture.jpg"}`,
		prefix+"/api/v1/sys/img/fixture.jpg?size=small")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gateway.offline.Load() {
			if strings.HasPrefix(r.URL.Path, prefix+"/api/v1/media/") {
				gateway.offlineReads.Add(1)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		route, accepted := strings.CutPrefix(r.URL.Path, prefix+"/api/v1/")
		if !accepted {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Route-Draft") != "" {
			t.Error("a custom header from the original port reached the moved gateway")
		}
		switch route {
		case "media/range/media-1", "media/cache/fixture.mp4", "sys/img/fixture.jpg":
			if r.Header.Get("Authorization") != reviewFlymooToken || r.Header.Get("authx") == "" {
				t.Error("Flymoo media read lacks the token or request signature")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if route == "sys/img/fixture.jpg" {
				gateway.images.Add(1)
				if len(poster) == 0 {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(poster)
				return
			}
			if route == "media/range/media-1" {
				http.Redirect(w, r, prefix+"/api/v1/media/cache/fixture.mp4?range=original", http.StatusTemporaryRedirect)
				return
			}
			if len(clip) == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			gateway.video.Add(1)
			w.Header().Set("ETag", `"flymoo-review-1"`)
			w.Header().Set("Content-Type", "video/mp4")
			http.ServeContent(w, r, "fixture.mp4", time.Time{}, bytes.NewReader(clip))
			return
		}
		if r.Header.Get("authx") == "" {
			t.Error("the verified API endpoint did not receive an API signature")
		}
		if route != "login" && !strings.HasPrefix(route, "sys/") && r.Header.Get("Authorization") != reviewFlymooToken {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var data string
		switch route {
		case "sys/config":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" {
				t.Error("the public gateway probe received credentials")
			}
			gateway.public.Add(1)
			data = `{"server_guid":"nas-review","server_name":"飞牛测试 NAS"}`
		case "sys/version":
			data = `{"version":"0.9.1"}`
		case "login":
			var credentials struct {
				Username string
				Password string
				AppName  string `json:"app_name"`
			}
			if gateway.public.Load() == 0 || r.Method != http.MethodPost ||
				json.NewDecoder(r.Body).Decode(&credentials) != nil ||
				credentials.Username != "viewer" || credentials.Password != "fixture-password" || credentials.AppName != "fnmovie" {
				t.Error("login was not sent as a new request to the verified gateway")
			}
			gateway.logins.Add(1)
			data = `{"token":"` + reviewFlymooToken + `","uid":"viewer-id"}`
		case "mdb/list":
			data = `[{"guid":"movies","name":"Movies","category":"movies"}]`
		case "item/list", "search/list":
			data = `{"total":1,"list":[` + movie + `]}`
		case "movie/movie-1":
			data = movie
		case "play/list":
			data = `[]`
		case "play/info":
			data = `{"media_guid":"media-1","file_name":"fixture.mp4"}`
		case "stream":
			data = `{"video_stream":{"duration":24,"width":320,"height":180,"codec_name":"h264"}}`
		case "play/record":
			data = "{}"
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"code":0,"data":%s}`, data)
	})
	if tls {
		gateway.server = httptest.NewTLSServer(handler)
	} else {
		gateway.server = httptest.NewServer(handler)
	}
	t.Cleanup(gateway.server.Close)
	return gateway
}

func flymooGatewayRedirector(t *testing.T, target string) *httptest.Server {
	t.Helper()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/sys/config" || r.Header.Get("Authorization") != "" {
			t.Error("the original Flymoo address received more than a public probe")
		}
		http.Redirect(w, r, target+"/api/v1/sys/config", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(front.Close)
	return front
}

func TestFlymooGatewayCompatibilityAndScope(t *testing.T) {
	a := application{newMedia: media.NewClient, device: media.NewDevice("Gateway test")}
	input := media.Credentials{Username: "viewer", Password: "fixture-password"}
	t.Run("default media path", func(t *testing.T) {
		gateway := newFlymooGatewayFixture(t, false, "/v", nil, nil)
		connected, err := a.connectServer(t.Context(), source{Provider: media.Flymoo, Address: gateway.server.URL}, input, passwordConnection, nil)
		if err != nil || connected.source.Address != gateway.server.URL+"/v" || connected.session.Token != reviewFlymooToken ||
			len(connected.libraries) != 1 || gateway.logins.Load() != 1 {
			t.Fatalf("Flymoo /v fallback failed: %v", err)
		}
	})
	t.Run("TLS upgrade and custom header scope", func(t *testing.T) {
		gateway := newFlymooGatewayFixture(t, true, "/relay/v", nil, nil)
		front := flymooGatewayRedirector(t, gateway.server.URL+"/relay/v")
		entry := source{Provider: media.Flymoo, Address: front.URL, headerDraft: http.Header{"X-Route-Draft": {"fixture"}}}
		_, err := a.connectServer(t.Context(), entry, input, passwordConnection, nil)
		var failure *sourceCertificateError
		if !errors.As(err, &failure) || failure.address != gateway.server.URL+"/relay/v" || gateway.logins.Load() != 0 {
			t.Fatalf("expected certificate approval for the destination: %v", err)
		}
		pin, ok := mediatls.Candidate(failure.address, err)
		if !ok {
			t.Fatal("missing destination pin")
		}
		entry.Certificates = []mediatls.Trust{pin}
		connected, err := a.connectServer(t.Context(), entry, input, passwordConnection, nil)
		if err != nil || connected.source.Address != failure.address ||
			connected.source.headerDraft != nil || len(connected.source.Certificates) != 1 || gateway.logins.Load() != 1 {
			t.Fatalf("verified upgrade failed: %v", err)
		}
	})
	t.Run("different hostname", func(t *testing.T) {
		gateway := newFlymooGatewayFixture(t, false, "/v", nil, nil)
		different := strings.Replace(gateway.server.URL, "127.0.0.1", "localhost", 1)
		front := flymooGatewayRedirector(t, different+"/v")
		_, err := a.connectServer(t.Context(), source{Provider: media.Flymoo, Address: front.URL}, input, passwordConnection, nil)
		if !errors.Is(err, media.ErrFlymooGateway) || gateway.public.Load() != 0 || gateway.logins.Load() != 0 {
			t.Fatalf("expected rejection of cross-host redirect, got %v", err)
		}
	})
	t.Run("moved target lacks public identity", func(t *testing.T) {
		target := httptest.NewServer(http.NotFoundHandler())
		defer target.Close()
		front := flymooGatewayRedirector(t, target.URL+"/v")
		_, err := a.connectServer(t.Context(), source{Provider: media.Flymoo, Address: front.URL}, input, passwordConnection, nil)
		if !errors.Is(err, media.ErrFlymooGateway) {
			t.Fatalf("expected rejection of an unverified target, got %v", err)
		}
	})
	t.Run("redirect loop", func(t *testing.T) {
		var count atomic.Int64
		front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			http.Redirect(w, r, r.URL.Path, http.StatusTemporaryRedirect)
		}))
		defer front.Close()
		_, err := a.connectServer(t.Context(), source{Provider: media.Flymoo, Address: front.URL}, input, passwordConnection, nil)
		if !errors.Is(err, media.ErrFlymooGateway) || count.Load() != 1 {
			t.Fatalf("redirect loop made %d requests and returned %v", count.Load(), err)
		}
	})
	t.Run("HTTPS downgrade", func(t *testing.T) {
		gateway := newFlymooGatewayFixture(t, false, "/v", nil, nil)
		front := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, gateway.server.URL+"/v/api/v1/sys/config", http.StatusTemporaryRedirect)
		}))
		defer front.Close()
		entry := source{Provider: media.Flymoo, Address: front.URL}
		_, err := a.connectServer(t.Context(), entry, input, passwordConnection, nil)
		pin, ok := mediatls.Candidate(front.URL, err)
		if !ok {
			t.Fatalf("expected certificate request: %v", err)
		}
		entry.Certificates = []mediatls.Trust{pin}
		_, err = a.connectServer(t.Context(), entry, input, passwordConnection, nil)
		if !errors.Is(err, media.ErrFlymooGateway) || gateway.public.Load() != 0 || gateway.logins.Load() != 0 {
			t.Fatalf("expected rejection of HTTP downgrade, got %v", err)
		}
	})
	t.Run("saved server identity", func(t *testing.T) {
		gateway := newFlymooGatewayFixture(t, false, "/v", nil, nil)
		front := flymooGatewayRedirector(t, gateway.server.URL+"/v")
		_, err := a.connectServer(t.Context(), source{Provider: media.Flymoo, Address: front.URL, ServerID: "different-nas"}, input, passwordConnection, nil)
		if !errors.Is(err, errDifferentServer) || gateway.logins.Load() != 0 {
			t.Fatalf("expected rejection of a different server identity, got %v", err)
		}
	})
}

func TestFlymooDefaultPortsAndFNID(t *testing.T) {
	for _, entry := range []struct {
		input, port string
		https       bool
		want        string
	}{
		{"192.0.2.1", "", false, "http://192.0.2.1:5666"},
		{"[2001:db8::1]", "", true, "https://[2001:db8::1]:5667"},
		{"nas.local", "", true, "https://nas.local:5667"},
		{"nas.example.net", "", true, "https://nas.example.net"},
		{"http://192.0.2.1", "", true, "http://192.0.2.1"},
		{"192.0.2.1:8005", "5666", true, "https://192.0.2.1:8005"},
		{"nas.local/v", "8000", false, "http://nas.local:8000/v"},
	} {
		address, err := sourceAddress(media.Flymoo, entry.input, entry.port, entry.https)
		if err != nil || address.String() != entry.want {
			t.Errorf("%q = %v, %v, want %q", entry.input, address, err, entry.want)
		}
		host, port, https := sourceAddressFields(media.Flymoo, entry.want)
		reopened, err := sourceAddress(media.Flymoo, host, port, https)
		if err != nil || reopened.String() != entry.want {
			t.Errorf("editing %q changed the endpoint: %v, %v", entry.want, reopened, err)
		}
	}
	if got := flymooRemoteAddress(" Living-Room7 "); got != "https://living-room7.fnos.net/v" {
		t.Errorf("unexpected FN address %q", got)
	}
	for _, invalid := range []string{"", "other/path", "example.org", "guest@nas", "-leading", "trailing-", "中文"} {
		if address := flymooRemoteAddress(invalid); address != "" {
			t.Errorf("accepted invalid identifier %q", invalid)
		}
	}
}

func TestFlymooGatewayConnectionReview(t *testing.T) {
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("native review runs on the Linux host")
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
	gateway := newFlymooGatewayFixture(t, false, "/relay/v", clip, poster)
	front := flymooGatewayRedirector(t, gateway.server.URL+"/relay/v")
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			gateway.offline.Store(false)
			height := core.Points(1240)
			if target == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(variant.appearance)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))
			app.Press("id:add-source")
			app.Press("id:choose-flymoo")
			app.Press("id:flymoo-fn-connect")
			app.Type("id:flymoo-fn-id", "living/room")
			app.Press("id:flymoo-fn-apply")
			app.WaitFor("text:请输入由字母", 2*time.Second)
			app.Clear("id:flymoo-fn-id")
			app.Type("id:flymoo-fn-id", "living-room7")
			if target == "linux" {
				app.Screenshot("flymoo-fn-connect-" + variant.name)
			}
			app.Press("id:flymoo-fn-apply")
			if address := app.Find("id:server-address").Text; address != "living-room7.fnos.net/v" || !app.Find("id:server-https").Props.Checked {
				t.Fatalf("FN ID did not fill the HTTPS endpoint: %q", address)
			}
			app.Clear("id:server-address")
			app.Type("id:server-address", front.URL)
			app.Press("id:test-connection")
			app.WaitFor("id:connection-verified", 5*time.Second)
			host, port, _ := splitServerAddress(gateway.server.URL + "/relay/v")
			if app.Find("id:server-address").Text != host || app.Find("id:server-port").Text != port {
				t.Fatal("the tested gateway did not update the connection fields")
			}
			app.Type("id:server-username", "viewer")
			app.Type("id:server-password", "fixture-password")
			app.Press("id:sign-in")
			app.WaitFor("id:import-library-count-movies", 5*time.Second)
			if target == "linux" {
				app.Screenshot("flymoo-gateway-connected-" + variant.name)
			}
			beforeArtwork := gateway.images.Load()
			app.Press("id:import-libraries")
			app.WaitFor("id:tab-search", 5*time.Second)
			value, _ := app.Storage().GetItem(sourcesKey)
			var sources []source
			if json.Unmarshal([]byte(value), &sources) != nil || len(sources) != 1 ||
				sources[0].Address != gateway.server.URL+"/relay/v" || sources[0].ServerID != "nas-review" {
				t.Fatal("the authenticated gateway address was not stored")
			}
			app.Press("id:tab-search")
			app.Type("id:search-query", "Waygu Flymoo Review")
			app.Press("id:search-submit")
			app.WaitFor("id:title-movie-1", 5*time.Second)
			app.Press("id:title-movie-1")
			app.WaitFor("id:detail-play", 5*time.Second)
			video := gateway.video.Load()
			app.Press("id:detail-play")
			app.Resize(844, 390)
			app.WaitFor("id:video-player", 5*time.Second)
			if target == "linux" {
				app.WaitUntil(8*time.Second, func() bool {
					return gateway.video.Load() > video && gateway.images.Load() > beforeArtwork &&
						app.Find("id:player-position").Text > "00:00:00"
				})
				app.Press("id:toggle-play")
				const rail = "id:phone-player-position-rail"
				width := app.Find(rail).Frame.Size.Width
				app.DragAt(rail, core.Point{X: width / 4, Y: 8}, core.Point{X: width / 3})
				app.WaitUntil(3*time.Second, func() bool { return app.Find("id:player-position").Text > "00:00:12" })
				seeked := app.Find("id:player-position").Text
				app.Press("id:toggle-play")
				app.WaitUntil(5*time.Second, func() bool { return app.Find("id:player-position").Text > seeked })
				app.Press("id:toggle-play")
				app.Screenshot("flymoo-signed-playback-" + variant.name)
			}
			app.Press("id:close-player")
			if target != "linux" {
				return
			}
			app.Resize(390, height)
			app.WaitFor("id:detail-download", 5*time.Second)
			app.Press("id:detail-download")
			app.WaitFor("role:button 已下载", 10*time.Second)
			raw, _ := app.Storage().GetItem(downloadQueueKey)
			if raw == "" || strings.Contains(raw, reviewFlymooToken) || strings.Contains(raw, gateway.server.URL) {
				t.Fatal("the download queue must not persist the signed resource")
			}
			offlineReads := gateway.offlineReads.Load()
			gateway.offline.Store(true)
			app.Press("id:detail-download")
			app.Resize(844, 390)
			app.WaitFor("id:video-player", 5*time.Second)
			app.WaitFor("text:离线播放", 5*time.Second)
			app.WaitUntil(8*time.Second, func() bool { return app.Find("id:player-position").Text > "00:00:00" })
			app.Press("id:toggle-play")
			app.Screenshot("flymoo-signed-offline-" + variant.name)
			if gateway.offlineReads.Load() != offlineReads {
				t.Fatal("offline playback sent another media request")
			}
			app.Press("id:close-player")
		})
	}
}
