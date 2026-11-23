package main

import (
	"fmt"
	"io"
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

	"waygu-video/internal/danmaku"
	"waygu-video/internal/downloads"
	"waygu-video/internal/media"
)

func TestMovieDanmakuCountsFollowBindings(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("runs the pairing and request assertions in the headless harness")
	}
	current := route{source: movieDesignFixtureSource(), item: media.Item{ID: "test-film"}}
	for _, layout := range []struct {
		name  string
		idiom core.Idiom
		width core.Points
	}{{"desktop", core.IdiomDesktop, 1440}, {"phone", core.IdiomPhone, 390}} {
		t.Run(layout.name, func(t *testing.T) {
			var dandanRequests, logvarRequests atomic.Int32
			logvar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/fixture-password/api/v2/comment/124":
					logvarRequests.Add(1)
					fmt.Fprint(w, fixtureComments(2))
				case "/fixture-password/api/v2/search/episodes":
					fmt.Fprint(w, `{"success":true,"animes":[{"animeId":2,"animeTitle":"Fixture Film","episodes":[{"episodeId":125,"episodeTitle":"Empty"}]}]}`)
				case "/fixture-password/api/v2/comment/125":
					logvarRequests.Add(1)
					fmt.Fprint(w, fixtureComments(0))
				default:
					t.Errorf("unexpected Logvar request: %s", req.URL.Path)
					http.NotFound(w, req)
				}
			}))
			defer logvar.Close()
			dandan := func(credentials danmaku.DandanCredentials) *danmaku.DandanClient {
				return danmaku.NewDandanClient(credentials, dandanTestTransport(func(req *http.Request) (*http.Response, error) {
					var body string
					switch req.URL.Path {
					case "/api/v2/comment/123":
						dandanRequests.Add(1)
						body = fixtureComments(1)
					case "/api/v2/search/episodes":
						body = `{"success":true,"animes":[{"animeId":1,"animeTitle":"Fixture Film","episodes":[{"episodeId":126,"episodeTitle":"New match"}]}]}`
					case "/api/v2/comment/126":
						dandanRequests.Add(1)
						body = fixtureComments(4)
					default:
						t.Errorf("unexpected DandanPlay request: %s", req.URL.Path)
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				}))
			}
			custom := danmaku.LogvarConfig{Address: logvar.URL, Token: "fixture-password"}
			dandanKey, logvarKey := danmakuBindingKey(current), logvarBindingKey(current, custom)
			config := movieDesignFixtureConfig(t, 0)
			config[dandanKey] = `{"episodeId":123}`
			config[logvarKey] = `{"episodeId":124}`
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, layout.idiom)
				return appRootWithDandan(cx, movieDesignFixtureClient, nil, dandan)
			}, nsgotest.Idiom(layout.idiom), nsgotest.Size(layout.width, 1500),
				nsgotest.Environment(core.Environment{Locale: "zh-Hans"}), nsgotest.Storage(config),
				nsgotest.Secrets(map[string]string{"capture": "fixture-token", logvarConfigKey: downloadFixtureJSON(t, custom),
					dandanCredentialKey: downloadFixtureJSON(t, danmaku.DandanCredentials{AppID: "fixture-id", Secret: "fixture-secret"})}))
			open := func() {
				app.Press("id:tab-library")
				app.WaitFor("id:title-test-film", 5*time.Second)
				app.Press("id:title-test-film")
			}
			verify := func(dandan, custom int) {
				t.Helper()
				if layout.idiom == core.IdiomPhone {
					app.WaitFor("text:弹幕 "+fmt.Sprint(dandan+max(custom, 0)), 5*time.Second)
					return
				}
				app.WaitFor(fmt.Sprintf("text:已匹配 %d 条 · 弹弹play", dandan), 5*time.Second)
				if custom >= 0 {
					app.WaitFor(fmt.Sprintf("text:已匹配 %d 条 · Logvar", custom), 5*time.Second)
				}
			}
			open()
			verify(1, 2)
			match := func(provider string, id int) {
				t.Helper()
				app.Press("id:detail-danmaku")
				app.WaitFor("id:"+provider+"-search", 3*time.Second)
				app.Press("id:" + provider + "-search")
				button := fmt.Sprintf("id:%s-episode-%d", provider, id)
				app.WaitFor(button, 3*time.Second)
				app.Press(button)
				app.FastForward(350 * time.Millisecond)
				app.WaitForGone("id:detail-danmaku-dialog", 3*time.Second)
			}
			match("danmaku", 126)
			verify(4, 2)
			match("logvar", 125)
			verify(4, 0)
			app.Press("id:detail-danmaku")
			app.WaitFor("id:logvar-unbind", 3*time.Second)
			app.Press("id:logvar-unbind")
			app.Press("id:detail-close-danmaku")
			app.FastForward(350 * time.Millisecond)
			app.WaitForGone("id:detail-danmaku-dialog", 3*time.Second)
			verify(4, -1)
			if _, found := app.Storage().GetItem(logvarKey); found || app.Exists("text:已匹配 0 条 · Logvar") {
				t.Fatal("the removed Logvar binding or its count survived")
			}
			raw, _ := app.Storage().GetItem(dandanKey)
			if bound := decodeDanmakuBinding(raw); bound.ID != 126 || bound.Count == nil || *bound.Count != 4 {
				t.Fatal("the refreshed count was not stored with the new DandanPlay binding")
			}
			app.Press("id:back")
			open()
			verify(4, -1)
			if dandanRequests.Load() != 2 || logvarRequests.Load() != 2 {
				t.Fatalf("comment fetches = DandanPlay %d, Logvar %d, want two each", dandanRequests.Load(), logvarRequests.Load())
			}
		})
	}
}

func fixtureComments(count int) string {
	body := strings.Repeat(`{"p":"1,1,16777215,fixture","m":"Example"},`, count)
	return `{"count":99,"comments":[` + body + `{"p":"invalid","m":""}]}`
}

func TestMovieDanmakuCountFromDownload(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("requires the local track fixture")
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		dir := filepath.Join(cx.DataDir(), "downloads")
		id := strings.Repeat("a", 64)
		if err := os.MkdirAll(filepath.Join(dir, id), 0700); err != nil {
			t.Fatal(err)
		}
		tracks := downloads.Tracks{Danmaku: &downloads.CommentTrack{Comments: []danmaku.Comment{
			{Text: "Saved one"}, {Text: "Saved two"},
		}}}
		if err := os.WriteFile(filepath.Join(dir, id, "tracks.json"), []byte(downloadFixtureJSON(t, tracks)), 0600); err != nil {
			t.Fatal(err)
		}
		a := &application{downloads: &downloadQueue{store: downloads.Store{Dir: dir}}}
		item := media.Item{ID: "saved", Title: "Saved movie", Kind: media.Movie}
		comments := a.movieDanmaku(cx, route{item: item, downloadID: id})
		return core.Dynamic(func(cx *core.Scope) core.Node {
			info := comments()
			return core.View{Children: []core.Node{
				a.phoneMovieHero(cx, item, media.Version{}, info, func() {}, nil),
				desktopMovieDanmaku(cx, info, nil),
			}}
		})
	}, nsgotest.Size(390, 800), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))
	app.WaitFor("text:弹幕 2", 3*time.Second)
	app.WaitFor("text:已保存 2 条", 3*time.Second)
	if app.Exists("id:detail-danmaku") {
		t.Fatal("the archived movie offered a remote match action")
	}
}
