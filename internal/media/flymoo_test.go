package media

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFlymooReferenceSignature(t *testing.T) {
	now := time.Unix(1700000000, 123456789)
	for _, test := range []struct{ method, address, body, hash string }{
		{http.MethodGet, "http://nas.test/v/api/v1/search/list?q=%E4%B8%89+%E4%BD%93%2B4k&q=1080p&ancestor_guid=lib-1", "", "300b006b55675a97217e75a794a37268"},
		{http.MethodPost, "http://nas.test/api/v1/login", `{"username":"user","password":"password","app_name":"fnmovie"}`, "37d67883c9cd36513bb769b8235b69f7"},
	} {
		endpoint, err := url.Parse(test.address)
		if err != nil {
			t.Fatal(err)
		}
		want := "nonce=256789&timestamp=1700000000123&sign=" + test.hash
		if got := flymooSignature(endpoint, test.method, []byte(test.body), now); got != want {
			t.Errorf("%s signature = %q, want %q", test.method, got, want)
		}
	}
}

// This fixture follows fnmovie server_test.go at de2b3dd, with the same
// request bodies and representative envelopes for the shared operations.
func TestFlymooReferenceProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Trim-Client") != "web" || r.Header.Get("X-Trim-Client-Version") != "631" || r.Header.Get("authx") == "" {
			t.Error("missing fnOS media client headers")
		}
		op := strings.TrimPrefix(r.URL.Path, "/v/api/v1/")
		if op == r.URL.Path {
			t.Errorf("media server prefix lost: %s", r.URL.Path)
		}
		var body map[string]any
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		if op != "login" && op != "sys/config" && op != "sys/version" && r.Header.Get("Authorization") != "reference-token" {
			t.Error("missing raw session token")
		}
		var response string
		switch op {
		case "sys/config":
			response = `{"server_guid":"nas-1","server_name":"Fixture NAS"}`
		case "sys/version":
			response = `{"version":"1.0"}`
		case "login":
			if body["app_name"] != "fnmovie" || body["username"] != "user" || body["password"] != "password" || len(body) != 3 {
				t.Error("login fields differ from the reference")
			}
			response = `{"token":"reference-token"}`
		case "mdb/list":
			response = `[{"guid":"catalog-1","name":"Movies"}]`
		case "item/list":
			if body["page"] != float64(1) || body["page_size"] != float64(20) || body["ancestor_guid"] != "catalog-1" {
				t.Errorf("unexpected item list parameters: %+v", body)
			}
			response = `{"total":2,"list":[{"guid":"item-1","title":"Example","type":"Video","release_date":"2024-01-01","mdb_guid":"catalog-1","is_favorite":true,"watched":true},{"guid":"elsewhere","title":"Other library","mdb_guid":"other"}]}`
		case "search/list":
			if r.URL.Query().Get("q") != "例 子" || r.URL.Query().Get("ancestor_guid") != "catalog-1" {
				t.Error("search query was not trimmed or scoped")
			}
			response = `{"list":[{"guid":"item-1","title":"例 子","mdb_guid":"catalog-1"}]}`
		case "movie/item-1":
			response = `{"guid":"item-1","overview":"Description","poster":[{"path":"show/movie.jpg"}]}`
		case "season/list/show-1":
			response = `[{"guid":"season-2","title":"Season 2","number":2},{"guid":"season-1","title":"Season 1","season_number":1}]`
		case "episode/list/season-1":
			response = `{"list":[{"guid":"episode-2","title":"Episode 2","episode_number":2},{"guid":"episode-1","title":"Episode 1","episode_number":1}]}`
		case "play/list":
			response = `{"items":[{"item_guid":"item-1","duration":125.5,"item":{"guid":"item-1","title":"Example"}}]}`
		case "play/info":
			if body["item_guid"] != "item-1" || len(body) != 1 {
				t.Errorf("unexpected playback info parameters: %+v", body)
			}
			response = `{"media_guid":"media-1","ts":37}`
		case "stream":
			if body["media_guid"] != "media-1" || body["ip"] != "fnmovie-windows" {
				t.Errorf("unexpected stream parameters: %+v", body)
			}
			response = `{"video_stream":{"duration":125.5},"direct_link_qualities":[{"url":"/play/720.m3u8","bitrate":1000},{"url":"/play/1080.m3u8","bitrate":4000}]}`
		case "play/record":
			if body["item_guid"] != "item-1" || body["media_guid"] != "media-1" || body["ts"] != float64(15) || len(body) != 3 {
				t.Errorf("unexpected progress parameters: %+v", body)
			}
			response = `{}`
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"code":0,"data":%s}`, response)
	}))
	defer server.Close()
	ctx := t.Context()
	guest, err := NewClient(Flymoo, server.URL+"/v", Device{}, Session{})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := guest.Probe(ctx); err != nil || info.Name != "Fixture NAS" || info.Version != "1.0" {
		t.Fatalf("probe = %+v, %v", info, err)
	}
	session, err := guest.SignIn(ctx, Credentials{Username: "user", Password: "password"})
	if err != nil || session.Token != "reference-token" {
		t.Fatalf("sign in failed: %v", err)
	}
	client, err := NewClient(Flymoo, server.URL+"/v", Device{}, session)
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := client.Libraries(ctx)
	if err != nil || len(libraries) != 1 || libraries[0].ID != "catalog-1" {
		t.Fatalf("libraries = %+v, %v", libraries, err)
	}
	page, err := client.Catalog(ctx, Query{LibraryID: "catalog-1", Limit: 20})
	if err != nil || page.Total != 2 || len(page.Items) != 1 {
		t.Fatalf("catalog = %+v, %v", page, err)
	}
	item := page.Items[0]
	if item.Kind != Movie || item.Year != 2024 || !item.Favorite || !item.Watched {
		t.Errorf("item fields = %+v", item)
	}
	if matches, err := client.Catalog(ctx, Query{LibraryID: "catalog-1", Search: " 例 子 "}); err != nil || len(matches.Items) != 1 {
		t.Fatalf("search = %+v, %v", matches, err)
	}
	item, err = client.Detail(ctx, item)
	if err != nil || item.Overview != "Description" || item.Title != "Example" || item.Poster != server.URL+"/v/api/v1/sys/img/show/movie.jpg" {
		t.Fatalf("detail = %+v, %v", item, err)
	}
	seasons, err := client.Children(ctx, Item{ID: "show-1", Kind: Series})
	if err != nil || len(seasons) != 2 || seasons[0].ID != "season-1" {
		t.Fatalf("seasons = %+v, %v", seasons, err)
	}
	episodes, err := client.Children(ctx, seasons[0])
	if err != nil || len(episodes) != 2 || episodes[0].ID != "episode-1" {
		t.Fatalf("episodes = %+v, %v", episodes, err)
	}
	previous, err := client.Resume(ctx)
	if err != nil || len(previous) != 1 || previous[0].Position != 37*time.Second {
		t.Fatalf("resume = %+v, %v", previous, err)
	}
	stream, err := client.Playback(ctx, item, PlaybackOptions{})
	if err != nil || stream.URL != server.URL+"/play/1080.m3u8" || stream.SourceID != "media-1" ||
		stream.Duration != 125500*time.Millisecond || stream.Position != 37*time.Second {
		t.Fatalf("unexpected stream descriptor: %v", err)
	}
	if err := client.Report(ctx, PlaybackState{Event: Progress, Stream: stream, Position: 15750 * time.Millisecond}); err != nil {
		t.Fatalf("report: %v", err)
	}
}

func TestFlymooExternalDirectLinkUsesNASRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/play/info":
			fmt.Fprint(w, `{"code":0,"data":{"media_guid":"media-1","ts":52}}`)
		case "/api/v1/stream":
			fmt.Fprint(w, `{"code":0,"data":{"video_stream":{"duration":60},"qualities":[{"url":"https://cloud.test/video.mp4","bitrate":3000}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(Flymoo, server.URL, Device{}, Session{Token: "reference-token"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Playback(t.Context(), Item{ID: "item-1"}, PlaybackOptions{})
	if err != nil || stream.URL != server.URL+"/api/v1/media/range/media-1" || stream.Position != 0 ||
		stream.Headers.Get("Authorization") != "reference-token" {
		t.Fatalf("unexpected NAS fallback: %v", err)
	}
}
