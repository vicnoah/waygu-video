package danmaku

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type dandanTransport func(*http.Request) (*http.Response, error)

func (f dandanTransport) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestDandanSignedMatchingAndCache(t *testing.T) {
	var commentFetches int
	client := NewDandanClient(DandanCredentials{AppID: "fixture-app", Secret: "fixture-secret"}, dandanTransport(func(request *http.Request) (*http.Response, error) {
		headers := make(http.Header)
		code := http.StatusOK
		var body string
		if request.URL.Host == "cdn.fixture.test" {
			if request.Header.Get("X-Signature") != "" || request.Header.Get("X-AppId") != "" || request.Header.Get("X-Timestamp") != "" {
				t.Error("CDN request contained API signing headers")
			}
			body = `{"count":2,"comments":[{"p":"12.34,5,16711680,u","m":"Example"},{"p":"0,7,16777215,u","m":"Unsupported"}]}`
			return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		stamp := request.Header.Get("X-Timestamp")
		if _, err := base64.StdEncoding.DecodeString(request.Header.Get("X-Signature")); err != nil || stamp == "" {
			t.Error("invalid signing headers")
		}
		digest := sha256.Sum256([]byte("fixture-app" + stamp + request.URL.Path + "fixture-secret"))
		if request.Header.Get("X-AppId") != "fixture-app" || request.Header.Get("X-Signature") != base64.StdEncoding.EncodeToString(digest[:]) {
			t.Error("the request was signed with a different path or key")
		}
		switch request.URL.Path {
		case "/api/v2/match":
			var file FileMatch
			err := json.NewDecoder(request.Body).Decode(&file)
			if err != nil || request.Method != http.MethodPost || file.Name != "File name" || file.Hash != "012345" ||
				file.Mode != "hashAndFileName" || file.Size != 42 || file.Duration != 30 {
				t.Errorf("unexpected file match request: %+v, %v", file, err)
			}
			body = `{"success":true,"isMatched":true,"matches":[{"episodeId":9001,"animeId":9,"animeTitle":"作品","episodeTitle":"Episode 2","shift":-0.5}]}`
		case "/api/v2/search/episodes":
			if request.URL.Query().Get("anime") != "作品 + 描述" || request.URL.Query().Get("episode") != "2" {
				t.Error("episode search did not encode its query")
			}
			body = `{"success":true,"hasMore":true,"animes":[{"animeId":9,"animeTitle":"作品","episodes":[{"episodeId":9001,"episodeTitle":"Episode 2"}]}]}`
		case "/api/v2/bangumi/9":
			body = `{"success":true,"bangumi":{"episodes":[{"episodeId":9001,"episodeNumber":"02","seasonId":"a"},{"episodeId":9999,"episodeNumber":"03","seasonId":"b"},{"episodeId":9018,"episodeTitle":"Episode 3","episodeNumber":"03","seasonId":"a"},{"episodeId":9004,"episodeNumber":"S1","seasonId":"a"}]}}`
		case "/api/v2/comment/9001":
			if request.URL.Query().Get("withRelated") != "true" {
				t.Error("related comments were not requested")
			}
			commentFetches++
			code = http.StatusFound
			headers.Set("Location", "https://cdn.fixture.test/comments")
		default:
			t.Errorf("unexpected DandanPlay route: %s", request.URL.Path)
			code = http.StatusNotFound
		}
		return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	ctx := t.Context()
	matched, err := client.Match(ctx, FileMatch{Name: "File name", Hash: "012345", Size: 42, Duration: 30})
	if err != nil || !matched.Exact || len(matched.Episodes) != 1 || matched.Episodes[0].Shift != -.5 {
		t.Fatalf("exact match = %+v, %v", matched, err)
	}
	search, err := client.Search(ctx, "作品 + 描述", "2")
	if err != nil || !search.More || len(search.Episodes) != 1 || search.Episodes[0].Label() != "作品 · Episode 2" {
		t.Fatalf("search = %+v, %v", search, err)
	}
	next, err := client.RelatedEpisode(ctx, matched.Episodes[0], 1)
	if err != nil || next.ID != 9018 || next.AnimeID != 9 {
		t.Fatalf("next episode = %+v, %v", next, err)
	}
	cache := t.TempDir()
	read := func() {
		comments, err := client.CachedComments(ctx, cache, matched.Episodes[0].ID)
		if err != nil || len(comments) != 1 || comments[0] != (Comment{At: 12340 * time.Millisecond, Mode: Top, Color: 0xff0000, Text: "Example", User: "u"}) {
			t.Fatalf("comments = %+v, %v", comments, err)
		}
	}
	read()
	read()
	if commentFetches != 1 {
		t.Fatalf("fresh cache requested comments %d times", commentFetches)
	}
	files, err := filepath.Glob(filepath.Join(cache, "dandanplay", "*.json.gz"))
	if err != nil || len(files) != 1 {
		t.Fatalf("cache files = %v, %v", files, err)
	}
	stale := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(files[0], stale, stale); err != nil {
		t.Fatal(err)
	}
	read()
	if commentFetches != 2 {
		t.Fatalf("expired cache was reused, requests=%d", commentFetches)
	}
}

func TestFingerprintReadsOnlyTheFirst16MiB(t *testing.T) {
	file := strings.Repeat("x", matchBytes) + "Excluded"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/no-range" {
			fmt.Fprint(w, "This response ignores ranges")
			return
		}
		if r.Header.Get("Range") != "bytes=0-16777215" || r.Header.Get("Authorization") != "fixture-header" {
			t.Error("incorrect source range or missing media authorization")
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", matchBytes-1, len(file)))
		w.WriteHeader(http.StatusPartialContent)
		fmt.Fprint(w, file[:matchBytes])
	}))
	defer server.Close()
	got, size, err := Fingerprint(t.Context(), server.URL, http.Header{"Authorization": {"fixture-header"}})
	want := fmt.Sprintf("%x", md5.Sum([]byte(file[:matchBytes])))
	if err != nil || got != want || size != int64(len(file)) {
		t.Fatalf("fingerprint = %s, %d, %v", got, size, err)
	}
	if _, _, err := Fingerprint(t.Context(), server.URL+"/no-range", nil); err == nil {
		t.Fatal("treated a full response as a byte range")
	}
}
