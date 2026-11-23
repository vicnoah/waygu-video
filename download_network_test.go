package main

import (
	"encoding/json"
	"fmt"
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
)

func TestDockerDownloadNetworkPolicy(t *testing.T) {
	target := nsgotest.Target()
	android := target == "android"
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" ||
		(android && os.Getenv("WAYGU_TEST_ANDROID_DOWNLOAD_NETWORK") != "1") ||
		(target != "" && !android) {
		t.Skip("requires Docker and a compatible network test host")
	}
	config, secrets := dockerReviewSources(t, false)
	_, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Download Test")
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	entry := sources[0]
	address, err := url.Parse(entry.Address)
	if err != nil {
		t.Fatal(err)
	}
	ranges := make(chan string, 16)
	var fast atomic.Bool
	proxy := httputil.NewSingleHostReverseProxy(address)
	original := proxy.Director
	proxy.Director = func(request *http.Request) {
		original(request)
		request.Host = address.Host
		request.Header["X-Forwarded-For"] = nil
	}
	proxy.ModifyResponse = func(result *http.Response) error {
		if interval := result.Request.Header.Get("Range"); interval != "" && strings.HasPrefix(result.Header.Get("Content-Type"), "video/") {
			ranges <- interval
			// Keep the fixture's validator constant across the two routes.
			result.Header.Set("ETag", `"download-network-fixture"`)
			if !fast.Load() {
				result.Body = pacedFixtureBody{ReadCloser: result.Body, ctx: result.Request.Context()}
			}
		}
		return nil
	}
	gateway := httptest.NewServer(proxy)
	t.Cleanup(func() {
		gateway.CloseClientConnections()
		gateway.Close()
	})
	entry.Address = gateway.URL
	id := downloadID(mediaAccountKey(entry), movie.ID)
	saved, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(saved)
	var host *core.Host
	var observed atomic.Uint32
	var app *nsgotest.App
	change := func(kind string) {
		if android {
			fmt.Println("WAYGU_DOWNLOAD_NETWORK_" + strings.ToUpper(kind))
		} else if err := host.Send(androidNetworkChange, kind); err != nil {
			t.Fatal(err)
		}
		app.WaitUntil(15*time.Second, func() bool { return downloadNetwork(observed.Load()) == parseDownloadNetwork(kind) })
	}
	if !android {
		host = core.NewHost()
		host.Handle(androidNetworkTypeCall, func(call *core.HostCall) { call.Resolve("wifi") })
	}
	app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.OnHostEvent(androidNetworkChange, func(value any) { observed.Store(uint32(parseDownloadNetwork(value))) })
		return Root(cx)
	}, nsgotest.Size(390, 1417), nsgotest.Idiom(core.IdiomPhone),
		nsgotest.Environment(core.Environment{OS: core.OSAndroid, Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets), nsgotest.Host(host))
	qualityPicker := func(title, id string) {
		app.Press("id:tab-search")
		app.Clear("id:search-query")
		app.Type("id:search-query", title)
		app.Press("id:search-submit")
		app.WaitFor("id:title-"+id, 10*time.Second)
		app.Press("id:title-" + id)
		app.WaitFor("id:detail-download", 8*time.Second)
		app.Press("id:detail-download")
		app.WaitFor("id:detail-download-quality", 5*time.Second)
	}
	choose := func(selector string) {
		app.Press(selector)
		app.FastForward(350 * time.Millisecond)
		app.WaitForGone("id:detail-download-quality", 5*time.Second)
	}
	storage := func() {
		app.Press("id:back")
		app.Press("id:tab-settings")
		app.WaitFor("id:phone-settings-home", 5*time.Second)
		if android {
			app.Scroll("id:phone-settings-home", 440)
		}
		app.Press("id:settings-downloads")
	}
	qualityPicker(movie.Title, movie.ID)
	choose("id:detail-download-confirm")
	storage()
	waitRange := func() string {
		t.Helper()
		app.WaitUntil(12*time.Second, func() bool { return len(ranges) > 0 })
		return <-ranges
	}
	state := func() string { return app.Find("id:download-state-" + id).Text }
	waitStatus := func(prefix string) {
		t.Helper()
		app.WaitUntil(15*time.Second, func() bool { return strings.HasPrefix(state(), prefix) })
	}
	if first := waitRange(); first != "bytes=0-" {
		t.Fatalf("initial request range = %q", first)
	}
	wantProgress := func() {
		t.Helper()
		app.WaitUntil(12*time.Second, func() bool {
			text := state()
			return strings.HasPrefix(text, "下载中") && strings.Contains(text, "/") && !strings.Contains(text, "0 B /")
		})
	}
	wantProgress()
	change("cellular")
	waitStatus("等待 Wi-Fi")
	if target != "" {
		app.Screenshot("download-waiting-for-wifi")
	}
	if len(ranges) != 0 {
		t.Fatal("download requested again on cellular")
	}
	app.Press("id:download-pause-" + id)
	waitStatus("已暂停")
	change("wifi")
	waitStatus("已暂停")
	app.Press("id:download-resume-" + id)
	if resumed := waitRange(); resumed == "bytes=0-" {
		t.Fatal("resume discarded the validated partial file")
	}
	wantProgress()
	change("offline")
	waitStatus("等待 Wi-Fi")
	if len(ranges) != 0 {
		t.Fatal("download requested again offline")
	}
	fast.Store(true)
	change("wifi")
	app.WaitFor("id:download-play-"+id, 25*time.Second)
	if resumed := waitRange(); resumed == "bytes=0-" {
		t.Fatal("automatic resume discarded the validated partial file")
	}
	if android {
		return
	}

	app.Press("id:download-wifi-only")
	if saved, _ := app.Storage().GetItem(downloadWiFiKey); saved != "false" {
		t.Fatalf("saved Wi-Fi preference = %q", saved)
	}
	_, other := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	otherID := downloadID(mediaAccountKey(entry), other.ID)
	change("cellular")
	app.Press("id:back")
	qualityPicker(other.Title, other.ID)
	app.WaitFor("text:立即使用移动网络", 3*time.Second)
	choose("id:detail-download-wait-wifi")
	storage()
	status := "id:download-state-" + otherID
	app.WaitUntil(4*time.Second, func() bool { return strings.HasPrefix(app.Find(status).Text, "等待 Wi-Fi") })
	if len(ranges) != 0 {
		t.Fatal("a copy reserved for Wi-Fi used mobile data")
	}
	app.Press("id:download-cellular-" + otherID)
	app.WaitFor("id:download-play-"+otherID, 20*time.Second)
	if requested := waitRange(); requested != "bytes=0-" {
		t.Fatalf("new transfer on mobile data requested %q", requested)
	}
}
