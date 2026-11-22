package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestFlymooFNConnectAccessReview(t *testing.T) {
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("native review uses Linux GTK")
	}
	const grant = "fn_gate=temporary-fixture"
	backend := newFlymooGatewayFixture(t, false, "/v", nil, nil)
	base, err := url.Parse(backend.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var denied atomic.Int64
	proxy := httputil.NewSingleHostReverseProxy(base)
	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != grant {
			denied.Add(1)
			http.Error(w, "access code required", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	defer gateway.Close()

	for _, appearance := range reviewAppearances {
		t.Run(appearance.name, func(t *testing.T) {
			var secure core.Secrets
			var attempts atomic.Int64
			height := core.Points(1240)
			if target == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(appearance.appearance)
				secure = cx.Secrets()
				fnConnectResolver.Provide(cx, func(_ context.Context, id, code string) (media.FNConnectResult, error) {
					attempts.Add(1)
					if id != "living-room7" {
						return media.FNConnectResult{}, media.ErrFNConnectAbsent
					}
					if code != "123456" {
						return media.FNConnectResult{}, media.ErrFNConnectCode
					}
					return media.FNConnectResult{Address: gateway.URL + "/v", Name: "飞牛测试 NAS", ServerID: "nas-review", Cookie: grant}, nil
				})
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))
			app.Press("id:add-source")
			app.Press("id:choose-flymoo")
			app.Press("id:flymoo-fn-connect")
			app.Type("id:flymoo-fn-id", "living-room7")
			app.Type("id:flymoo-fn-code", "654321")
			app.Press("id:flymoo-fn-apply")
			app.WaitFor("text:网关未接受访问码", time.Second)
			if attempts.Load() != 1 || app.Exists("id:flymoo-fn-default") {
				t.Fatal("the rejected access code did not keep the protected flow in the dialog")
			}
			if !app.Find("id:flymoo-fn-code").Input.Masked {
				t.Fatal("the access code field must mask its value")
			}
			if target == "linux" {
				app.Screenshot("fn-connect-rejected-" + appearance.name)
			}
			app.Clear("id:flymoo-fn-code")
			app.Type("id:flymoo-fn-code", "123456")
			app.Press("id:flymoo-fn-apply")
			app.WaitForGone("id:flymoo-fn-dialog", 5*time.Second)
			if attempts.Load() != 2 || app.Find("id:server-name").Text != "飞牛测试 NAS" || !app.Find("id:server-https").Props.Checked {
				t.Fatal("FN Connect did not populate the verified HTTPS endpoint")
			}
			app.Press("id:test-connection")
			app.WaitFor("id:approve-source-certificate", 5*time.Second)
			app.Press("id:approve-source-certificate")
			app.WaitFor("id:connection-verified", 5*time.Second)
			app.Type("id:server-username", "viewer")
			app.Type("id:server-password", "fixture-password")
			app.Press("id:sign-in")
			app.WaitFor("id:import-library-count-movies", 5*time.Second)
			if target == "linux" {
				app.Screenshot("fn-connect-authorized-" + appearance.name)
			}
			app.Press("id:import-libraries")
			app.WaitFor("id:tab-search", 5*time.Second)
			raw, found := app.Storage().GetItem(sourcesKey)
			var saved []source
			if !found || strings.Contains(raw, grant) || strings.Contains(raw, "123456") ||
				json.Unmarshal([]byte(raw), &saved) != nil || len(saved) != 1 ||
				saved[0].Address != gateway.URL+"/v" || saved[0].HeaderSecret == "" {
				t.Fatal("FN Connect did not save the scoped source and reference")
			}
			value, found, err := secure.Get(saved[0].HeaderSecret)
			if err != nil || !found || !strings.Contains(value, grant) || strings.Contains(value, "123456") {
				t.Fatal("the secure store should contain the gateway grant")
			}
			app.Press("id:tab-search")
			app.Type("id:search-query", "Waygu Flymoo Review")
			app.Press("id:search-submit")
			app.WaitFor("id:title-movie-1", 5*time.Second)
			if denied.Load() != 0 {
				t.Fatal("a saved protected source lost its gateway grant")
			}
		})
	}
}
