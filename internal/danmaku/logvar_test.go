package danmaku

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLogvarTokenAndCacheIsolation(t *testing.T) {
	var requests atomic.Int32
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-AppId") != "" || request.Header.Get("X-Signature") != "" {
			t.Error("Logvar received DandanPlay credentials")
		}
		switch request.URL.Path {
		case "/proxy/api/v2/comment/77", "/proxy/test password?/api/v2/comment/77":
			if request.URL.Query().Get("format") != "json" {
				t.Error("Logvar comments were not requested as JSON")
			}
			requests.Add(1)
			fmt.Fprint(w, `{"comments":[{"p":"1.5,5,16777215,sender","m":"Fixture","cid":1,"t":1500}]}`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(service.Close)
	cache := t.TempDir()
	for _, token := range []string{"", "test password?"} {
		client, err := NewLogvarClient(LogvarConfig{Address: service.URL + "/proxy/", Token: token})
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			comments, err := client.CachedComments(t.Context(), cache, 77)
			if err != nil || len(comments) != 1 || comments[0].Text != "Fixture" {
				t.Fatalf("comments = %d, %v", len(comments), err)
			}
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("two credential sets made %d comment requests", requests.Load())
	}
	files, _ := filepath.Glob(filepath.Join(cache, "logvar-danmu", "*.json.gz"))
	if len(files) != 2 {
		t.Fatalf("got %d cache files", len(files))
	}
	size, err := CachedCommentBytes(cache)
	if err != nil || size <= 0 {
		t.Fatalf("cache bytes = %d, %v", size, err)
	}
	if err := ClearCachedComments(cache); err != nil {
		t.Fatal(err)
	}
	if size, err := CachedCommentBytes(cache); err != nil || size != 0 {
		t.Fatalf("cleared cache bytes = %d, %v", size, err)
	}
	invalid, err := NewLogvarClient(LogvarConfig{Address: service.URL + "/proxy", Token: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalid.Comments(t.Context(), 77); !errors.Is(err, ErrLogvarAuthentication) {
		t.Fatalf("unrecognized password returned %v", err)
	}
}

func TestLogvarRedirectKeepsPasswordOnConfiguredServer(t *testing.T) {
	var requests atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(other.Close)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	client, err := NewLogvarClient(LogvarConfig{Address: service.URL, Token: "local-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	_, err = client.Comments(t.Context(), 1)
	if !errors.Is(err, ErrLogvarRequest) || strings.Contains(err.Error(), "local-test-password") || requests.Load() != 0 {
		t.Fatal("redirect exposed the configured endpoint or password")
	}
	service.Close()
	_, err = client.Comments(t.Context(), 1)
	if !errors.Is(err, ErrLogvarRequest) || strings.Contains(err.Error(), "local-test-password") {
		t.Fatal("network error exposed the configured endpoint or password")
	}
}

func TestLogvarLive(t *testing.T) {
	address := os.Getenv("WAYGU_LOGVAR_LIVE_URL")
	title := os.Getenv("WAYGU_LOGVAR_LIVE_TITLE")
	if address == "" || title == "" {
		t.Skip("requires a locally configured Logvar API and a searchable title")
	}
	client, err := NewLogvarClient(LogvarConfig{Address: address, Token: os.Getenv("WAYGU_LOGVAR_LIVE_TOKEN")})
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := client.Match(t.Context(), FileMatch{Name: title + " S01E01"})
	if err != nil {
		t.Fatalf("live filename match failed: %v", err)
	}
	matches, err := client.Search(t.Context(), title, "1")
	if err != nil || len(matches.Episodes) == 0 {
		t.Fatalf("live search returned %d episode matches, %v", len(matches.Episodes), err)
	}
	comments, err := client.Comments(t.Context(), matches.Episodes[0].ID)
	if err != nil || len(comments) == 0 {
		t.Fatalf("live retrieval returned %d playable comments, %v", len(comments), err)
	}
	t.Logf("Filename match returned %d candidates, search returned %d episodes, decoded %d comments",
		len(automatic.Episodes), len(matches.Episodes), len(comments))
}
