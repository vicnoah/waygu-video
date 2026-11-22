package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
)

func TestPhoneDanmakuSettingsReview(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("design comparisons use the Linux GTK backend")
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			height := core.Points(2080)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(theme.appearance)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(map[string]string{
					seenKey: "true", desktopPreferencesKey: `{"Language":"zh-Hans"}`,
					danmakuPreferencesKey: `{"Opacity":80,"Density":60,"Outline":2,"ReserveSubtitles":false,"PromptOnMiss":true}`,
				}))
			app.Press("id:tab-settings")
			app.Press("id:open-phone-danmaku-settings")
			app.WaitFor("id:phone-danmaku-settings", time.Second)
			app.WaitFor("text:弹幕显示区域 1/2", time.Second)
			if nsgotest.Target() == "linux" {
				app.Advance(350 * time.Millisecond)
				app.Screenshot("settings-danmaku-" + theme.name)
			}
			if theme.name != "light" {
				return
			}
			app.Press("id:danmaku-area")
			app.WaitFor("role:option 1/4 屏", time.Second)
			app.Press("role:option 1/4 屏")
			app.WaitFor("text:弹幕显示区域 1/4", time.Second)
			app.Press("id:danmaku-font")
			app.WaitFor("role:option 系统默认 · 常规", time.Second)
			app.Press("role:option 系统默认 · 常规")
			if nsgotest.Target() == "" {
				app.AccessibilityAction("id:danmaku-opacity-thumb", core.ActionDecrement)
			}
			app.Press("id:phone-danmaku-filters")
			app.WaitFor("id:danmaku-keywords", time.Second)
			app.Type("id:danmaku-keywords", "spoiler")
			app.Press("id:danmaku-save-filters")
			app.WaitForGone("id:danmaku-keywords", time.Second)
			app.Press("id:phone-danmaku-blocked-users")
			app.WaitFor("id:danmaku-user-input", time.Second)
			app.Type("id:danmaku-user-input", "fixture-sender")
			app.Press("id:danmaku-save-users")
			app.WaitForGone("id:danmaku-user-input", time.Second)
			app.WaitFor("text:1 人", time.Second)
			prefs := readDanmakuPreferences(app.Storage())
			if prefs.Area != 25 || prefs.Bold || prefs.Keywords != "spoiler" || prefs.BlockedUsers != "fixture-sender" ||
				(nsgotest.Target() == "" && prefs.Opacity != 75) {
				t.Fatalf("danmaku preferences = %+v", prefs)
			}
			if nsgotest.Target() == "" {
				painted := app.FindMounted("id:phone-danmaku-preview-canvas")
				var color core.Color
				for _, op := range painted.Ops {
					if op.Kind == core.DrawFillColor {
						color = op.Color
					}
					if op.Kind == core.DrawFillText && color == core.RGBA(0x7dd3fc, .75) && op.Font.Weight == core.FontWeightNormal {
						return
					}
				}
				t.Fatal("preview did not paint the new opacity and font weight")
			}
		})
	}
}

func TestPlayerPromptsForMissingDanmaku(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses an injected media server and match response")
	}
	entry := source{ID: "fixture", Provider: media.Jellyfin, Address: "https://media.example.test", Name: "Fixture",
		Libraries: []sourceLibrary{{Library: media.Library{ID: "movies"}}}}
	config, _ := json.Marshal([]source{entry})
	transport := dandanTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v2/search/episodes" {
			t.Errorf("unexpected matching route %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"success":true,"animes":[]}`)),
			Header: make(http.Header)}, nil
	})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return appRootWithDandan(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return dandanVideoFixture{fixtureServer: fixtureServer{session: session}, url: "https://media.example.test/video.mp4"}, nil
		}, nil, func(credentials danmaku.DandanCredentials) *danmaku.DandanClient {
			return danmaku.NewDandanClient(credentials, transport)
		})
	}, nsgotest.Offscreen(), nsgotest.Size(1000, 850), nsgotest.Environment(core.Environment{Locale: "en"}),
		nsgotest.Storage(map[string]string{sourcesKey: string(config), danmakuPreferencesKey: `{"PromptOnMiss":true}`}),
		nsgotest.Secrets(map[string]string{"fixture": "fixture-token", dandanCredentialKey: `{"AppID":"fixture","Secret":"fixture-secret"}`}))
	app.WaitFor("id:title-test-film", time.Second)
	app.Press("id:title-test-film")
	app.WaitFor("text:Fixture synopsis", time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	app.WaitFor("id:danmaku-search-title", time.Second)
	app.WaitFor("text:No matching episodes found.", time.Second)
	app.Press("id:close-danmaku-sources")
	app.Advance(350 * time.Millisecond)
	app.WaitForGone("id:danmaku-search-title", time.Second)
}
