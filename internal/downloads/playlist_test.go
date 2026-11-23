package downloads

import (
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFiniteMP4DownloadPlaylist(t *testing.T) {
	for _, test := range []struct {
		name, mediaList string
		checksum        string
		wantError       bool
	}{
		{name: "complete", mediaList: "#EXT-X-MAP:URI=\"init\"\n#EXTINF:2,\npart\n#EXT-X-ENDLIST",
			checksum: func() string {
				sum := md5.Sum([]byte("video"))
				return base64.StdEncoding.EncodeToString(sum[:])
			}()},
		{name: "truncated", mediaList: "#EXT-X-MAP:URI=\"init\"\n#EXTINF:2,\npart", wantError: true},
		{name: "third-party segment", mediaList: "#EXT-X-MAP:URI=\"init\"\n#EXTINF:2,\nhttps://other.test/part\n#EXT-X-ENDLIST", wantError: true},
		{name: "byte range", mediaList: "#EXT-X-MAP:URI=\"init\"\n#EXTINF:2,\n#EXT-X-BYTERANGE:50@0\npart\n#EXT-X-ENDLIST", wantError: true},
		{name: "checksum mismatch", mediaList: "#EXT-X-MAP:URI=\"init\"\n#EXTINF:2,\npart\n#EXT-X-ENDLIST", checksum: "bad", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("request lost its media-server authorization")
				}
				switch r.URL.Path {
				case "/master.m3u8":
					_, _ = fmt.Fprintln(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nstream/index.m3u8")
				case "/stream/index.m3u8":
					_, _ = fmt.Fprintln(w, "#EXTM3U\n"+test.mediaList)
				case "/stream/init":
					_, _ = fmt.Fprint(w, "init")
				case "/stream/part":
					w.Header().Set("Content-MD5", test.checksum)
					_, _ = fmt.Fprint(w, "video")
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/master.m3u8", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer fixture")
			response, err := openMP4Playlist(server.Client(), req)
			var contents []byte
			if err == nil {
				defer response.Body.Close()
				contents, err = io.ReadAll(response.Body)
			}
			if test.wantError {
				if !errors.Is(err, ErrInvalidFile) {
					t.Fatalf("invalid playlist or segment accepted: %v", err)
				}
			} else if err != nil || len(contents) != 53 || string(contents[:4]) != "init" ||
				string(contents[8:12]) != "free" || string(contents[48:]) != "video" {
				t.Fatalf("downloaded %q: %v", contents, err)
			}
		})
	}
}
