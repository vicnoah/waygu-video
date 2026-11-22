package playback

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"waygu-video/internal/mediahttp"
)

func TestProxyPreservesRangeWithoutUpstreamHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "sample.mp4", time.Time{}, strings.NewReader("0123456789"))
	}))
	defer upstream.Close()
	address, stop, err := Open(mediahttp.Resource{URL: upstream.URL + "/sample.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=3-5")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusPartialContent || string(data) != "345" {
		t.Fatalf("stream: status %d, data %q, %v", response.StatusCode, data, err)
	}
}

func TestProxyPreservesRangeAndAuthenticatesHLSResources(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "fixture-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/media/show/index.m3u8":
			if r.Header.Get("Range") != "" {
				t.Error("the proxy requested a partial playlist")
			}
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
	address, close, err := Open(mediahttp.Resource{URL: upstream.URL + "/media/show/index.m3u8", Headers: http.Header{"Authorization": {"fixture-token"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	base, _ := url.Parse(address)
	manifest, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Header.Set("Range", "bytes=0-")
	response, err := http.DefaultClient.Do(manifest)
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

func TestProxySharesReadLimitAcrossRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer upstream.Close()
	address, stop, err := OpenWithReadLimit(mediahttp.Resource{URL: upstream.URL}, 13)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for _, desired := range []string{"0123456789", "012", ""} {
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if string(data) != desired {
			t.Fatalf("read limit: received %q, want %q", data, desired)
		}
	}
}
