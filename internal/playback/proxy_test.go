package playback

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyPreservesRangeAndAuthenticatesHLSResources(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "fixture-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/media/show/index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MAP:URI=\"/media/header.mp4\"\n#EXTINF:4,\nsegment.m4s?quality=1\n")
		case "/media/show/segment.m4s":
			if r.Header.Get("Range") != "bytes=3-5" || r.URL.Query().Get("quality") != "1" {
				t.Error("segment did not preserve its range and query")
			}
			w.Header().Set("Content-Range", "bytes 3-5/10")
			w.WriteHeader(http.StatusPartialContent)
			fmt.Fprint(w, "345")
		case "/media/header.mp4":
			fmt.Fprint(w, "header")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	address, close, err := Open(upstream.URL+"/media/show/index.m3u8", http.Header{"Authorization": {"fixture-token"}})
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	base, _ := url.Parse(address)
	response, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	playlist, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || strings.Contains(string(playlist), "fixture-token") {
		t.Fatalf("playlist: status %d, %v", response.StatusCode, err)
	}
	var segment string
	for line := range strings.SplitSeq(string(playlist), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			segment = line
		}
	}
	if !strings.HasPrefix(segment, "/") {
		t.Fatalf("local playlist produced no segment: %s", playlist)
	}
	request, err := http.NewRequest(http.MethodGet, base.ResolveReference(&url.URL{Path: segment}).String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=3-5")
	segmentResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(segmentResponse.Body)
	segmentResponse.Body.Close()
	if err != nil || segmentResponse.StatusCode != http.StatusPartialContent || string(data) != "345" {
		t.Fatalf("segment: status %d, data %q, %v", segmentResponse.StatusCode, data, err)
	}
}
