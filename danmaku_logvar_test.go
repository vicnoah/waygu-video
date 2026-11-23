package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
)

func TestPlayerLogvarSettingsAndMatching(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" && target != "android" {
		t.Skip("requires the headless, GTK or Android phone harness")
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			password := ""
			if theme.name == "dark" {
				password = "local-fixture-password"
			}
			path := "/api/v2/"
			if password != "" {
				path = "/" + password + path
			}
			var matches, comments atomic.Int32
			exact := theme.name == "light"
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Header.Get("X-Signature") != "" || request.Header.Get("Authorization") != "" {
					t.Error("Logvar received credentials for another provider")
				}
				switch request.URL.Path {
				case path + "match":
					matches.Add(1)
					var match danmaku.FileMatch
					if json.NewDecoder(request.Body).Decode(&match) != nil || match.Name != "Fixture Film" || match.Hash != "" {
						t.Errorf("unexpected Logvar match by title: %q", match.Name)
					}
					if exact {
						fmt.Fprint(w, `{"success":true,"isMatched":true,"matches":[{"episodeId":521,"animeTitle":"Fixture Film","episodeTitle":"Movie"}]}`)
					} else {
						fmt.Fprint(w, `{"success":true,"isMatched":false,"matches":[]}`)
					}
				case path + "search/episodes":
					fmt.Fprint(w, `{"success":true,"animes":[{"animeId":520,"animeTitle":"Fixture Film","episodes":[{"episodeId":521,"episodeTitle":"Movie"}]}]}`)
				case path + "comment/521":
					comments.Add(1)
					fmt.Fprint(w, `{"comments":[{"p":"0.5,1,16777215,selfhosted","m":"Logvar fixture comment"}]}`)
				default:
					w.WriteHeader(http.StatusUnauthorized)
				}
			}))
			t.Cleanup(provider.Close)
			video := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				http.ServeContent(w, request, "fixture.mp4", time.Time{}, bytes.NewReader(nativePlaybackFixture))
			}))
			t.Cleanup(video.Close)
			entry := source{ID: "fixture", Provider: media.Jellyfin, Address: "https://media.example.test", Name: "Fixture",
				Libraries: []sourceLibrary{{Library: media.Library{ID: "movies"}}}}
			config, _ := json.Marshal([]source{entry})
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(theme.appearance)
				return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
					return dandanVideoFixture{fixtureServer: fixtureServer{session: session}, url: video.URL + "/fixture.mp4"}, nil
				}, nil)
			}, nsgotest.Idiom(core.IdiomPhone), nsgotest.Size(820, 1000),
				nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(map[string]string{sourcesKey: string(config), desktopPreferencesKey: `{"Language":"zh-Hans"}`}),
				nsgotest.Secrets(map[string]string{"fixture": "fixture-token"}))
			settingsTab, homeTab := "role:tab 设置", "role:tab 首页"
			if nsgotest.Target() == "linux" {
				settingsTab, homeTab = "id:tab-settings", "id:tab-home"
			}
			app.Press(settingsTab)
			app.Press("id:open-phone-danmaku-settings")
			app.Press("id:phone-danmaku-logvar")
			app.WaitFor("id:logvar-address", 3*time.Second)
			if nsgotest.Target() != "" {
				app.Advance(500 * time.Millisecond)
				app.Screenshot("logvar-setup-" + theme.name)
			}
			app.Type("id:logvar-address", provider.URL)
			if password != "" {
				app.Type("id:logvar-password", password)
			}
			app.Press("id:logvar-save")
			app.WaitFor("id:logvar-remove", 3*time.Second)
			app.Press("id:close-phone-danmaku-editor")
			app.WaitForGone("id:logvar-address", 3*time.Second)
			app.Press("id:back")
			app.Press(homeTab)
			app.WaitFor("id:home-hero-details", 3*time.Second)
			app.Press("id:home-hero-details")
			app.WaitFor("id:detail-play", 3*time.Second)
			app.Press("id:detail-play")
			app.WaitFor("id:video-player", 5*time.Second)
			if !exact {
				app.Press("id:danmaku-sources")
				app.WaitFor("id:danmaku-tab-source", 3*time.Second)
				app.Press("id:danmaku-tab-source")
				if nsgotest.Target() == "android" {
					app.Press("id:danmaku-manual-match")
				}
				app.WaitFor("id:logvar-episode-521", 3*time.Second)
				app.Press("id:logvar-search")
				app.WaitFor("id:logvar-episode-521", 3*time.Second)
				if nsgotest.Target() == "linux" {
					app.Advance(350 * time.Millisecond)
					app.Screenshot("logvar-player-match")
				}
				app.Press("id:logvar-episode-521")
				app.Advance(350 * time.Millisecond)
				app.WaitForGone("id:danmaku-sources-dialog", 3*time.Second)
			}
			app.WaitFor("id:danmaku-canvas", 5*time.Second)
			server := danmaku.LogvarConfig{Address: provider.URL, Token: password}
			key := logvarBindingKey(route{source: entry, item: media.Item{ID: "test-film"}}, server)
			app.WaitUntil(3*time.Second, func() bool {
				value, found := app.Storage().GetItem(key)
				return found && strings.Contains(value, "521")
			})
			for _, key := range app.Storage().Keys() {
				if raw, _ := app.Storage().GetItem(key); password != "" && strings.Contains(raw, password) {
					t.Fatal("password was saved in plain app storage")
				}
			}
			track := downloadLogvar(t.Context(), media.Item{ID: "test-film"}, danmakuPreferences{}, server,
				readDanmakuBindings(app.Storage(), key, "").item, t.TempDir())
			if track == nil || len(track.Comments) != 1 || track.Comments[0].Text != "Logvar fixture comment" {
				t.Fatal("offline queue could not retrieve the bound Logvar comments")
			}
			app.Press("id:close-player")
			app.WaitFor("id:detail-play", 3*time.Second)
			app.Press("id:detail-play")
			app.WaitFor("id:danmaku-canvas", 5*time.Second)
			if matches.Load() != 1 || comments.Load() != 2 {
				t.Fatalf("expected one match and two comment reads including the download: %d matches, %d comments", matches.Load(), comments.Load())
			}
		})
	}
}
