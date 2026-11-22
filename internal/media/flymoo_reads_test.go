package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/downloads"
	"waygu-video/internal/mediahttp"
	"waygu-video/internal/playback"
)

func TestFlymooSignedMediaReads(t *testing.T) {
	clip := bytes.Repeat([]byte("fixture clip "), 30<<10)
	const token = "fixture-token"
	var externalReads, resumeReads atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		externalReads.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer external.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/relay/v/api/v1/play/info":
			_, _ = io.WriteString(w, `{"code":0,"data":{"media_guid":"source/1","file_name":"fixture.mp4"}}`)
			return
		case "/relay/v/api/v1/stream":
			_, _ = io.WriteString(w, `{"code":0,"data":{"video_stream":{"duration":24},"direct_link_qualities":[{"url":"/relay/v/index.m3u8?ticket=z","bitrate":5000}]}}`)
			return
		}
		if r.Header.Get("Authorization") != token || r.Header.Get("X-Trim-Client") != "web" ||
			r.Header.Get("X-Trim-Client-Version") != "631" || !validFlymooReadSignature(r) {
			t.Errorf("missing or incorrect %s signature for %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.EscapedPath() {
		case "/relay/v/api/v1/media/range/source%2F1":
			http.Redirect(w, r, "/relay/v/fixture%20clip.mp4?z=1&a=%E4%B8%89+%2B&a=2", http.StatusTemporaryRedirect)
		case "/relay/v/fixture%20clip.mp4":
			w.Header().Set("ETag", `"fixture-1"`)
			if r.Header.Get("If-Range") == `"fixture-1"` && r.Header.Get("Range") == "bytes=131072-" {
				resumeReads.Add(1)
			}
			http.ServeContent(w, r, "fixture.mp4", time.Time{}, bytes.NewReader(clip))
		case "/relay/v/index.m3u8":
			http.Redirect(w, r, "/relay/v/results/master.m3u8?ticket=renewed", http.StatusTemporaryRedirect)
		case "/relay/v/results/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:24,\nsegment.ts?ticket=ok&seq=2\n")
		case "/relay/v/results/segment.ts":
			_, _ = io.WriteString(w, "sample transport stream")
		case "/relay/v/poster.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "sample poster")
		case "/relay/v/another-origin":
			http.Redirect(w, r, external.URL, http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	adapter, err := NewClient(Flymoo, upstream.URL+"/relay/v", Device{}, Session{Token: token})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := adapter.Playback(t.Context(), Item{ID: "movie"}, PlaybackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("range and head across same-origin redirect", func(t *testing.T) {
		address, closeProxy, err := playback.Open(stream.Original())
		if err != nil {
			t.Fatal(err)
		}
		defer closeProxy()
		if got := readFlymooFixture(t, address, http.MethodHead, "", http.StatusOK); len(got) != 0 {
			t.Fatal("HEAD returned a body")
		}
		if got := readFlymooFixture(t, address, http.MethodGet, "bytes=64000-64031", http.StatusPartialContent); !bytes.Equal(got, clip[64000:64032]) {
			t.Fatal("nonzero range did not reach the decoder")
		}
	})
	t.Run("renderer playlist and segment", func(t *testing.T) {
		address, closeProxy, err := playback.OpenForRenderer(stream.Resource(), netip.MustParseAddr("127.0.0.1"), "application/vnd.apple.mpegurl")
		if err != nil {
			t.Fatal(err)
		}
		defer closeProxy()
		playlist := readFlymooFixture(t, address, http.MethodGet, "", http.StatusOK)
		lines := strings.Split(strings.TrimSpace(string(playlist)), "\n")
		if len(lines) != 3 {
			t.Fatalf("unexpected playlist: %q", playlist)
		}
		link, err := url.Parse(lines[2])
		if err != nil {
			t.Fatal(err)
		}
		proxy, _ := url.Parse(address)
		if link.IsAbs() || link.RawQuery != "" || !strings.HasPrefix(link.Path, "/") {
			t.Fatalf("renderer received an upstream URL: %s", link)
		}
		if got := readFlymooFixture(t, proxy.ResolveReference(link).String(), http.MethodGet, "", http.StatusOK); string(got) != "sample transport stream" {
			t.Fatalf("renderer returned %q", got)
		}
	})
	t.Run("resumed download and danmaku fingerprint", func(t *testing.T) {
		id := strings.Repeat("a", 64)
		store := &downloads.Store{Dir: t.TempDir()}
		input := downloads.Source{Resource: stream.Original(), Name: stream.File.Name, MediaID: stream.SourceID}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		_, err := store.Transfer(ctx, id, input, downloads.Limits{}, func(count, _ int64) {
			if count > 0 {
				cancel()
			}
		})
		if !errors.Is(err, context.Canceled) || store.Bytes(id) != 128<<10 {
			t.Fatalf("paused Flymoo transfer: %v, %d bytes", err, store.Bytes(id))
		}
		file, err := store.Transfer(t.Context(), id, input, downloads.Limits{}, nil)
		if err != nil || file.SHA256 != fmt.Sprintf("%x", sha256.Sum256(clip)) || resumeReads.Load() != 1 {
			t.Fatalf("Flymoo resumed transfer: %v, %d resumed requests", err, resumeReads.Load())
		}
		hash, size, err := danmaku.Fingerprint(t.Context(), stream.Original())
		if err != nil || size != int64(len(clip)) || hash != md5String(clip) {
			t.Fatalf("Flymoo signed fingerprint: %s, %d bytes, %v", hash, size, err)
		}
	})
	t.Run("artwork and credential boundary", func(t *testing.T) {
		artwork, err := playback.OpenArtwork(mediahttp.Resource{URL: upstream.URL, Authorize: ArtworkAuthorization(adapter)})
		if err != nil {
			t.Fatal(err)
		}
		defer artwork.Close()
		path := artwork.Resolve(upstream.URL + "/relay/v/poster.jpg?size=small")
		if got := readFlymooFixture(t, path, http.MethodGet, "", http.StatusOK); string(got) != "sample poster" {
			t.Fatalf("artwork returned %q", got)
		}
		readFlymooFixture(t, artwork.Resolve(upstream.URL+"/relay/v/another-origin"), http.MethodGet, "", http.StatusBadGateway)
		if externalReads.Load() != 0 || artwork.Resolve(external.URL) != external.URL {
			t.Fatal("an external URL gained access to the signed proxy")
		}
	})
}

func TestFlymooReferenceArtworkPaths(t *testing.T) {
	for _, entry := range []struct {
		base, link, want string
	}{
		{"https://nas.test/relay/v", "/relay/v/poster%20file.jpg?size=small", "https://nas.test/relay/v/poster%20file.jpg?size=small"},
		{"https://nas.test/relay/v", "/api/v1/sys/img/poster.jpg", "https://nas.test/relay/v/api/v1/sys/img/poster.jpg"},
		{"https://nas.test/relay/v", "sys/img/poster.jpg", "https://nas.test/relay/v/api/v1/sys/img/poster.jpg"},
		{"https://nas.test/relay/v", "show/poster.jpg", "https://nas.test/relay/v/api/v1/sys/img/show/poster.jpg"},
		{"https://nas.test", "/poster.jpg", "https://nas.test/poster.jpg"},
		{"https://nas.test", "//cdn.test/poster.jpg", "https://cdn.test/poster.jpg"},
	} {
		client, err := NewClient(Flymoo, entry.base, Device{}, Session{})
		if err != nil {
			t.Fatal(err)
		}
		if got := client.(*flymooClient).artwork(map[string]any{"poster": entry.link}, "poster"); got != entry.want {
			t.Errorf("artwork %q at %q = %q, want %q", entry.link, entry.base, got, entry.want)
		}
	}
}

func validFlymooReadSignature(r *http.Request) bool {
	parts, err := url.ParseQuery(r.Header.Get("authx"))
	if err != nil || len(parts) != 3 {
		return false
	}
	nonce, nonceErr := strconv.Atoi(parts.Get("nonce"))
	millis, timestampErr := strconv.ParseInt(parts.Get("timestamp"), 10, 64)
	if nonceErr != nil || timestampErr != nil || nonce < 100000 || nonce > 999999 ||
		time.Since(time.UnixMilli(millis)).Abs() >= time.Minute {
		return false
	}
	var input []byte
	if r.Method == http.MethodGet {
		query, _ := url.QueryUnescape(r.URL.Query().Encode())
		input = []byte(query)
	}
	// Use the wire values independently of the request author's time source.
	text := fmt.Sprintf("NDzZTVxnRKP8Z0jXg1VAMonaG8akvh_%s_%d_%d_%s_16CCEB3D-AB42-077D-36A1-F355324E4237",
		r.URL.EscapedPath(), nonce, millis, md5String(input))
	return parts.Get("sign") == md5String([]byte(text))
}

func readFlymooFixture(t *testing.T, address, method, byteRange string, status int) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("%s %s returned %s, want %d", method, address, response.Status, status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
