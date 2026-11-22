package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
	"waygu-video/internal/tvpair"
)

func TestPhoneSyncAccountsReview(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("design comparisons use the Linux GTK backend")
	}
	sources, _ := json.Marshal([]source{
		{ID: "emby", Provider: media.Emby, Address: "https://emby.example.test", Name: "家庭 Emby"},
		{ID: "jellyfin", Provider: media.Jellyfin, Address: "https://jellyfin.example.test", Name: "书房 NAS · Jellyfin"},
		{ID: "flymoo", Provider: media.Flymoo, Address: "https://flymoo.example.test", Name: "飞牛影视"},
	})
	now := time.Date(2026, 11, 22, 21, 40, 0, 0, time.UTC)
	devices, _ := json.Marshal([]pairedDevice{
		{Device: tvpair.Device{ID: "tablet", Name: "iPad Pro", Kind: "tablet"}, SeenAt: now.Add(-2 * time.Hour)},
		{Device: tvpair.Device{ID: "laptop", Name: "MacBook Pro", Kind: "desktop"}, SeenAt: now.AddDate(0, 0, -1)},
		{Device: tvpair.Device{ID: "apple-tv", Name: "客厅 Apple TV 4K", Kind: "tv"}, SeenAt: now.AddDate(0, 0, -3)},
		{Device: tvpair.Device{ID: "android-tv", Name: "卧室 Android TV", Kind: "tv"}, SeenAt: now.AddDate(0, 0, -7)},
	})
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			height := core.Points(1560)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(theme.appearance)
				return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
					return fixtureServer{session: session}, nil
				}, nil)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans", DeviceName: "测试 iPhone"}),
				nsgotest.Storage(map[string]string{sourcesKey: string(sources), pairedDevicesKey: string(devices)}))
			app.Press("id:tab-settings")
			app.Press("id:open-sync-accounts")
			app.WaitFor("id:phone-sync-accounts", time.Second)
			app.WaitFor("id:sync-device-android-tv", time.Second)
			if nsgotest.Target() == "linux" {
				app.Advance(350 * time.Millisecond)
				app.Screenshot("settings-sync-" + theme.name)
			}
			if theme.name != "light" {
				return
			}
			app.Press("id:sync-progress-emby")
			app.Press("id:sync-progress-flymoo")
			app.Press("id:sync-progress-flymoo")
			raw, _ := app.Storage().GetItem(sourcesKey)
			var saved []source
			if json.Unmarshal([]byte(raw), &saved) != nil || len(saved) != 3 || !saved[0].ProgressDisabled ||
				saved[1].ProgressDisabled || saved[2].ProgressDisabled {
				t.Fatalf("source sync preferences = %+v", saved)
			}
			app.Press("id:sync-dandan")
			app.WaitFor("id:dandan-app-id", time.Second)
			app.Type("id:dandan-app-id", "sync-fixture-app")
			app.Type("id:dandan-app-secret", "sync-fixture-secret")
			app.Press("id:dandan-save")
			app.WaitFor("id:dandan-remove", time.Second)
			app.Press("id:close-sync-dandan")
			app.WaitForGone("id:sync-dandan-dialog", time.Second)
			app.WaitFor("id:sync-dandan >> text:已配置", time.Second)
			for _, key := range app.Storage().Keys() {
				if value, _ := app.Storage().GetItem(key); strings.Contains(value, "sync-fixture-secret") {
					t.Fatal("the account secret reached app storage")
				}
			}
			app.Press("id:open-phone-tv-pair")
			app.WaitFor("id:phone-tv-pair", time.Second)
		})
	}
}

func TestDockerSyncSourceWatchedIsolation(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SYNC") != "1" || nsgotest.Target() != "" {
		t.Skip("requires dedicated Docker servers and the local UI test")
	}
	config, secrets := homeReviewSources(t)
	emby, embyItem := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	jellyfin, jfItem := reviewFixtureItem(t, config, secrets, "jellyfin", "Waygu Track Test")
	before := func(client media.Client, item media.Item) media.Item {
		t.Helper()
		result, err := client.Detail(t.Context(), item)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	embyBefore, jfBefore := before(emby, embyItem), before(jellyfin, jfItem)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := jellyfin.(media.WatchedClient).SetWatched(ctx, jfItem, jfBefore.Watched); err != nil {
			t.Error(err)
		}
		if jfBefore.Position > 0 {
			setDockerCopyProgress(t, ctx, jellyfin, jfItem, jfBefore.Position)
		}
	})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, 1550), nsgotest.Environment(core.Environment{Locale: "en"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:tab-settings")
	app.Press("id:open-sync-accounts")
	app.Press("id:sync-progress-emby")
	app.Press("id:back")
	openTitle := func(id, item string) {
		t.Helper()
		app.Press("id:tab-search")
		app.Clear("id:search-query")
		app.Type("id:search-query", "Waygu Track Test")
		app.Press("id:search-source-" + id)
		app.WaitFor("id:title-"+id+"-"+item, 8*time.Second)
		app.Press("id:title-" + id + "-" + item)
		app.WaitFor("id:detail-watched", 5*time.Second)
	}
	mark := func(initiallyWatched bool) {
		t.Helper()
		app.Press("id:detail-watched")
		changed := "Mark unwatched"
		if initiallyWatched {
			changed = "Mark watched"
		}
		app.WaitFor("role:button "+changed, 4*time.Second)
	}
	openTitle("emby", embyItem.ID)
	mark(embyBefore.Watched)
	if after := before(emby, embyItem); after.Watched != embyBefore.Watched || after.Position != embyBefore.Position {
		t.Fatal("disabled Emby source received a watched update")
	}
	keys := app.Storage().Keys()
	if !slices.ContainsFunc(keys, func(key string) bool {
		value, _ := app.Storage().GetItem(key)
		return strings.HasPrefix(key, progressKeyPrefix) && strings.Contains(value, `"SourceID":"emby"`) && strings.Contains(value, `"LocalOnly":true`)
	}) {
		t.Fatal("the Emby watched update was not stored on the device")
	}
	app.Press("id:back")
	openTitle("jellyfin", jfItem.ID)
	mark(jfBefore.Watched)
	if after := before(jellyfin, jfItem); after.Watched == jfBefore.Watched {
		t.Fatal("enabled Jellyfin source did not receive the watched update")
	}
}

func TestSourceSyncReplayKeepsLocalCheckpoints(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses the local playback replay clock")
	}
	sources := []source{
		{ID: "paused", Provider: media.Emby, Address: "https://paused.example.test", AccountID: "viewer", ProgressDisabled: true},
		{ID: "online", Provider: media.Jellyfin, Address: "https://online.example.test", AccountID: "viewer"},
	}
	config, _ := json.Marshal(sources)
	storage := map[string]string{sourcesKey: string(config)}
	now := time.Now()
	seed := func(entry source, title string, local bool) {
		value := pendingProgress{Account: mediaAccountKey(entry), SourceID: entry.ID, Revision: title, LocalOnly: local,
			Update: media.OfflineProgress{ItemID: title, At: now, Position: 42 * time.Second}}
		raw, _ := json.Marshal(value)
		storage[value.key()] = string(raw)
	}
	seed(sources[0], "queued-before-opt-out", false)
	seed(sources[0], "played-locally", true)
	seed(sources[1], "enabled-source", false)
	paused := &offlineFixture{restored: make(chan media.OfflineProgress, 4)}
	online := &offlineFixture{restored: make(chan media.OfflineProgress, 4)}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return appRoot(cx, func(provider media.Provider, _ string, _ media.Device, _ media.Session) (media.Client, error) {
			if provider == media.Emby {
				return paused, nil
			}
			return online, nil
		}, nil)
	}, nsgotest.Size(390, 1550), nsgotest.Environment(core.Environment{Locale: "en"}),
		nsgotest.Storage(storage), nsgotest.Secrets(map[string]string{"paused": "token", "online": "token"}))
	app.FastForward(2 * time.Second)
	app.WaitUntil(time.Second, func() bool { return len(online.restored) == 1 })
	if entry := <-online.restored; entry.ItemID != "enabled-source" || len(paused.restored) != 0 {
		t.Fatalf("unexpected replay: %+v. Paused count: %d", entry, len(paused.restored))
	}
	app.Press("id:tab-settings")
	app.Press("id:open-sync-accounts")
	app.Press("id:sync-progress-paused")
	app.FastForward(32 * time.Second)
	app.WaitUntil(time.Second, func() bool { return len(paused.restored) == 1 })
	if entry := <-paused.restored; entry.ItemID != "queued-before-opt-out" {
		t.Fatalf("unexpected checkpoint reached the server: %+v", entry)
	}
	app.FastForward(35 * time.Second)
	if len(paused.restored) != 0 {
		t.Fatal("local-only playback reached the server after re-enabling it")
	}
}
