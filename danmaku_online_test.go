package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
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

func TestPlayerBindsDandanPlayAndReusesComments(t *testing.T) {
	var searches, comments atomic.Int32
	transport := dandanTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("X-AppId") != "ui-fixture-id" || request.Header.Get("X-Signature") == "" {
			t.Error("the saved credentials did not sign the request")
		}
		var body string
		switch request.URL.Path {
		case "/api/v2/search/episodes":
			searches.Add(1)
			if query := request.URL.Query().Get("anime"); query != "Fixture Film" {
				t.Errorf("unexpected title %q", query)
			}
			body = `{"success":true,"animes":[{"animeId":410,"animeTitle":"Fixture Film","episodes":[{"episodeId":421,"episodeTitle":"The Movie"}]}]}`
		case "/api/v2/comment/421":
			comments.Add(1)
			body = `{"count":1,"comments":[{"p":"0,5,16777215,u","m":"Online fixture comment"}]}`
		default:
			t.Errorf("unexpected request %s", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	entry := source{ID: "fixture", Provider: media.Jellyfin, Address: "https://media.example.test", Name: "Fixture",
		Libraries: []sourceLibrary{{Library: media.Library{ID: "movies"}}}}
	config, _ := json.Marshal([]source{entry})
	video := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.ServeContent(w, request, "fixture.mp4", time.Time{}, bytes.NewReader(nativePlaybackFixture))
	}))
	defer video.Close()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return appRootWithDandan(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return dandanVideoFixture{fixtureServer: fixtureServer{session: session}, url: video.URL + "/fixture.mp4"}, nil
		}, nil, func(credentials danmaku.DandanCredentials) *danmaku.DandanClient {
			return danmaku.NewDandanClient(credentials, transport)
		})
	}, nsgotest.Offscreen(), nsgotest.Idiom(core.IdiomPhone), nsgotest.Size(1000, 850),
		nsgotest.Storage(map[string]string{sourcesKey: string(config)}),
		nsgotest.Secrets(map[string]string{"fixture": "fixture-token"}))
	settingsTab, homeTab := "role:tab Settings", "role:tab Home"
	switch nsgotest.Target() {
	case "linux", "macos", "windows":
		settingsTab, homeTab = "id:tab-settings", "id:tab-home"
	}
	app.Press(settingsTab)
	app.Type("id:dandan-app-id", "ui-fixture-id")
	app.Type("id:dandan-app-secret", "ui-fixture-secret")
	app.Press("id:dandan-save")
	app.WaitFor("id:dandan-remove", time.Second)
	for _, key := range app.Storage().Keys() {
		if content, _ := app.Storage().GetItem(key); strings.Contains(content, "ui-fixture-secret") {
			t.Fatal("credential was written to plain app storage")
		}
	}
	app.Press(homeTab)
	app.WaitFor("id:title-test-film", time.Second)
	app.Press("id:title-test-film")
	app.WaitFor("text:Fixture synopsis", time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	app.Press("id:danmaku-sources")
	app.WaitFor("id:danmaku-tab-source", time.Second)
	app.Press("id:danmaku-tab-source")
	app.WaitFor("id:danmaku-episode-421", time.Second)
	app.Press("id:danmaku-search")
	app.WaitUntil(time.Second, func() bool { return searches.Load() == 2 })
	app.WaitFor("id:danmaku-episode-421", time.Second)
	app.Press("id:danmaku-episode-421")
	app.WaitFor("id:danmaku-canvas", time.Second)
	app.Advance(250 * time.Millisecond)
	app.WaitForGone("id:danmaku-sources-dialog", time.Second)

	key := danmakuBindingKey(route{source: entry, item: media.Item{ID: "test-film"}})
	if value, ok := app.Storage().GetItem(key); !ok || !strings.Contains(value, "421") || strings.Contains(value, "ui-fixture-secret") {
		t.Fatal("the selected episode was not safely bound to the media item")
	}
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:danmaku-canvas", time.Second)
	if searches.Load() != 2 || comments.Load() != 1 {
		t.Fatalf("reopening the player bypassed the binding or cache: searches=%d, comment fetches=%d", searches.Load(), comments.Load())
	}
	app.Press("id:danmaku-sources")
	app.WaitFor("id:danmaku-tab-source", time.Second)
	app.Press("id:danmaku-tab-source")
	app.WaitFor("id:danmaku-unbind", time.Second)
	closeSources := func() {
		app.Press("id:close-danmaku-sources")
		app.Advance(250 * time.Millisecond)
		app.WaitForGone("id:danmaku-sources-dialog", time.Second)
	}
	closeSources()
	app.Press("id:danmaku-sources")
	app.WaitFor("id:danmaku-unbind", time.Second)
	app.Press("id:danmaku-unbind")
	closeSources()
	app.WaitForGone("id:danmaku-canvas", time.Second)
	if _, found := app.Storage().GetItem(key); found {
		t.Fatal("unlinked danmaku binding remained in storage")
	}
}

type dandanTestTransport func(*http.Request) (*http.Response, error)

func (transport dandanTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type dandanVideoFixture struct {
	fixtureServer
	url string
}

func (fixture dandanVideoFixture) Playback(_ context.Context, item media.Item, _ media.PlaybackOptions) (media.Stream, error) {
	return media.Stream{ItemID: item.ID, Method: media.DirectPlay, Duration: 3 * time.Second, URL: fixture.url}, nil
}

func TestIdentifyDanmakuMatchesOriginalBehindTranscode(t *testing.T) {
	const original = "synthetic source video"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "fixture-media-token" || r.Header.Get("Range") != "bytes=0-16777215" {
			t.Error("fingerprinting did not use the authenticated original file")
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(original)-1, len(original)))
		w.WriteHeader(http.StatusPartialContent)
		fmt.Fprint(w, original)
	}))
	defer server.Close()
	var hash string
	client := danmaku.NewDandanClient(danmaku.DandanCredentials{AppID: "fixture", Secret: "fixture-secret"},
		dandanTestTransport(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/api/v2/match" {
				t.Errorf("unexpected request %s", request.URL.Path)
			}
			var file danmaku.FileMatch
			if err := json.NewDecoder(request.Body).Decode(&file); err != nil {
				t.Fatal(err)
			}
			hash = file.Hash
			if file.Name != "Fixture.S01E02" || file.Duration != 60 ||
				(file.Hash != "" && file.Size != int64(len(original))) {
				t.Errorf("unexpected match metadata: %+v", file)
			}
			body := `{"success":true,"isMatched":true,"matches":[{"episodeId":20,"animeTitle":"Fixture","episodeTitle":"Episode 2"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		}))
	stream := media.Stream{URL: "https://invalid.example.test/transcoded.m3u8", Method: media.Transcode, Duration: time.Minute,
		File: media.SourceFile{URL: server.URL, Name: "Fixture.S01E02.mkv", Headers: http.Header{"Authorization": {"fixture-media-token"}}}}
	selected, options, err := identifyDanmaku(t.Context(), client, stream, "Fixture", "2")
	if err != nil || hash != fmt.Sprintf("%x", md5.Sum([]byte(original))) || selected.ID != 20 || len(options.Episodes) != 0 {
		t.Fatalf("original video did not bind through its fingerprint: %+v, %+v, %v", selected, options, err)
	}
	stream.File.URL = ""
	selected, options, err = identifyDanmaku(t.Context(), client, stream, "Fixture", "2")
	if err != nil || hash != "" || selected.ID != 0 || len(options.Episodes) != 1 || options.Episodes[0].ID != 20 {
		t.Fatalf("filename match was not offered for confirmation: %+v, %+v, %v", selected, options, err)
	}
}
