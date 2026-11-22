package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerPhoneSourceSettingsReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media servers")
	}
	config, secrets := homeReviewSources(t)
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(1160)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(variant.appearance)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			capture := func(name string) {
				if nsgotest.Target() != "" {
					app.Advance(450 * time.Millisecond)
					app.Screenshot(name + "-" + variant.name)
				}
			}
			read := func() []source {
				raw, _ := app.Storage().GetItem(sourcesKey)
				var entries []source
				if err := json.Unmarshal([]byte(raw), &entries); err != nil {
					t.Fatal(err)
				}
				return entries
			}
			app.Press("id:tab-settings")
			app.WaitFor("text:3 个媒体源在线", 10*time.Second)
			capture("settings")
			app.Resize(390, height+160)
			app.Press("id:open-phone-source-settings")
			for _, id := range []string{"emby", "jellyfin", "plex"} {
				selector := "id:source-settings-status-" + id
				app.WaitFor(selector, time.Second)
				app.WaitUntil(10*time.Second, func() bool { return app.Find(selector).Text == "在线" })
				app.Press("id:source-sync-" + id)
				app.WaitUntil(8*time.Second, func() bool {
					return strings.HasPrefix(app.Find("id:source-settings-last-sync-"+id).Text, "同步于 ")
				})
			}
			capture("settings-sources")
			app.Press("id:source-settings-timeout")
			app.Press("role:option 20 秒")
			if stored, _ := app.Storage().GetItem(sourceTimeoutKey); stored != "20" {
				t.Fatalf("saved connection timeout = %q", stored)
			}
			app.Press("id:manage-libraries-jellyfin")
			app.WaitFor("id:save-libraries", time.Second)
			app.Press("id:back")
			app.Press("id:source-edit-emby")
			app.WaitFor("id:server-address", time.Second)
			app.Press("id:back")
			app.Press("id:source-settings-edit")
			app.Press("id:move-source-up-jellyfin")
			if entries := read(); len(entries) != 3 || entries[0].ID != "jellyfin" || entries[1].ID != "emby" {
				t.Fatal("moving the source did not update playback priority")
			}
			capture("settings-sources-editing")
			app.Press("id:source-settings-edit")
			app.Drag("id:source-reorder-jellyfin", core.Point{Y: 40}, core.Point{Y: 280})
			if entries := read(); entries[0].ID != "emby" || entries[1].ID != "jellyfin" {
				t.Fatalf("dragged order = %s, %s", entries[0].ID, entries[1].ID)
			}
			app.Press("id:merge-identical-titles")
			if stored, _ := app.Storage().GetItem(mergeTitlesKey); stored != "false" {
				t.Fatalf("saved merge preference = %q", stored)
			}
			app.DragAt("id:source-settings-card-plex", core.Point{X: 160, Y: 36}, core.Point{X: -24}, core.Point{X: -100})
			app.Press("id:source-remove-plex")
			app.WaitFor("id:source-settings-remove-dialog", time.Second)
			app.Press("id:confirm-remove-source")
			app.WaitForGone("id:source-settings-card-plex", time.Second)
			if slices.ContainsFunc(read(), func(s source) bool { return s.ID == "plex" }) {
				t.Fatal("removed provider still exists in the saved sources")
			}
			app.Press("id:back")
			app.WaitFor("text:2 个媒体源在线", time.Second)
		})
	}
}

func TestPhoneSourceSettingsOfflineAndEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	data, err := json.Marshal([]source{{ID: "offline", Name: "离线 Jellyfin", Provider: media.Jellyfin, Address: server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	height := core.Points(800)
	if nsgotest.Target() == "linux" {
		height += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(map[string]string{sourcesKey: string(data)}), nsgotest.Secrets(map[string]string{"offline": "fixture-token"}))
	app.Press("id:tab-settings")
	app.Press("id:open-phone-source-settings")
	app.WaitFor("id:source-settings-status-offline", time.Second)
	app.WaitUntil(time.Second, func() bool { return app.Find("id:source-settings-status-offline").Text == "离线" })
	if nsgotest.Target() != "" {
		app.Screenshot("settings-sources-offline")
	}
	app.Press("id:source-settings-edit")
	app.Press("id:source-remove-offline")
	app.Press("id:confirm-remove-source")
	app.WaitForGone("id:source-settings-card-offline", time.Second)
	app.WaitFor("id:source-settings-add", time.Second)
	if nsgotest.Target() != "" {
		app.Advance(400 * time.Millisecond)
		app.Screenshot("settings-sources-empty")
	}
	app.Press("id:source-settings-add")
	app.WaitFor("id:choose-jellyfin", time.Second)
}
