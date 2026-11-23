package playback

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"waygu-video/internal/mediahttp"
)

type subtitleRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip subtitleRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return roundTrip(r)
}

func subtitleResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Request: r, Body: io.NopCloser(strings.NewReader(body))}
}

func TestOnlineSubtitleSearchAndDownload(t *testing.T) {
	const size = 3 * 65536
	media := make([]byte, size)
	for pos := 0; pos < len(media); pos += 8 {
		binary.LittleEndian.PutUint64(media[pos:], 1)
	}
	var rangeRequests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "media-only" || r.Header.Get("Range") == "" {
			t.Error("the hash request must use the media authorization and an exact range")
			w.WriteHeader(403)
			return
		}
		rangeRequests.Add(1)
		http.ServeContent(w, r, "movie.mp4", time.Time{}, bytes.NewReader(media))
	}))
	defer origin.Close()
	credentials := OpenSubtitleCredentials{Key: "provider-only", Username: "account", Password: "example"}
	var searchHits, shooterHits, downloads int
	services := SubtitleServices{
		Shooter: true, Open: credentials, HTTP: &http.Client{Transport: subtitleRoundTrip(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("media token leaked to an online subtitle service")
			}
			host, path := r.URL.Host, r.URL.Path
			if host == "api.opensubtitles.com" || host == "vip-api.opensubtitles.com" {
				if r.Header.Get("Api-Key") != credentials.Key || r.Header.Get("User-Agent") == "" {
					t.Error("missing OpenSubtitles request headers")
				}
			} else if r.Header.Get("Api-Key") != "" || r.Header.Get("Authorization") != "" {
				t.Error("OpenSubtitles credentials leaked to a download or another provider")
			}
			status := http.StatusOK
			var content string
			switch {
			case host == "api.opensubtitles.com" && path == "/api/v1/subtitles":
				searchHits++
				q := r.URL.Query()
				if q.Get("languages") != "en,zh-cn" {
					t.Errorf("language query = %v", q)
				}
				if hash := q.Get("moviehash"); hash != "" {
					if hash != "0000000000034000" {
						t.Errorf("moviehash = %q", hash)
					}
					content = `{"data":[]}`
				} else {
					if q.Get("parent_imdb_id") != "1234" || q.Get("type") != "episode" ||
						q.Get("season_number") != "2" || q.Get("episode_number") != "3" {
						t.Errorf("episode fallback query = %v", q)
					}
					content = `{"data":[{"attributes":{"language":"en","download_count":9,"files":[{"file_id":37,"file_name":"Series.S02E03.srt"}]}}]}`
				}
			case host == "www.shooter.cn":
				shooterHits++
				if err := r.ParseForm(); err != nil || r.PostForm.Get("pathinfo") != "movie.mp4" ||
					len(strings.Split(r.PostForm.Get("filehash"), ";")) != 4 {
					t.Errorf("Shooter form = %v, %v", r.PostForm, err)
				}
				switch r.PostForm.Get("lang") {
				case "Chn":
					content = `[{"Desc":"中文","Delay":400,"Files":[{"Ext":"srt","Link":"http://file.shooter.cn/movie.srt"},{"Ext":"ass","Link":"https://file.shooter.cn/movie.ass"}]}]`
				case "Eng":
					content = "\xff"
				default:
					t.Errorf("unexpected Shooter language %q", r.PostForm.Get("lang"))
				}
			case host == "api.opensubtitles.com" && path == "/api/v1/login":
				content = `{"base_url":"vip-api.opensubtitles.com","token":"bearer"}`
			case host == "vip-api.opensubtitles.com" && path == "/api/v1/download":
				if r.Header.Get("Authorization") != "Bearer bearer" {
					t.Error("missing authenticated download token")
				}
				var request struct {
					ID     int    `json:"file_id"`
					Format string `json:"sub_format"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.ID != 37 || request.Format != "srt" {
					t.Errorf("download = %+v, %v", request, err)
				}
				content = `{"link":"https://www.opensubtitles.com/download/fixture"}`
			case host == "www.opensubtitles.com" || host == "file.shooter.cn":
				downloads++
				content = "1\n00:00:01,000 --> 00:00:03,000\nOnline subtitle fixture\n"
			default:
				t.Errorf("unexpected service call %s", r.URL)
				status = 404
			}
			return subtitleResponse(r, status, content), nil
		})},
	}
	query := OnlineSubtitleQuery{
		Title: "Series", SeriesIMDb: "tt0001234", Season: 2, Episode: 3, Languages: []string{"zh-Hans", "en"},
		FileName: "media/movie.mp4", FileSize: size,
		Original: mediahttp.Resource{URL: origin.URL, Headers: http.Header{"X-Plex-Token": {"media-only"}}},
	}
	found, err := services.Search(t.Context(), query)
	if err != nil || len(found) != 2 || found[0].Provider != Shooter || !found[0].Exact ||
		found[0].Delay != 400*time.Millisecond || found[1].Provider != OpenSubtitles || found[1].Exact {
		t.Fatalf("search = %+v, %v", found, err)
	}
	for _, match := range found {
		text, err := services.Download(t.Context(), match)
		if err != nil || !bytes.Contains(text, []byte("Online subtitle fixture")) {
			t.Fatalf("subtitle download = %q, %v", text, err)
		}
	}
	if got := rangeRequests.Load(); got != 6 || searchHits != 2 || shooterHits != 2 || downloads != 2 {
		t.Fatalf("ranges=%d, OpenSubtitles searches=%d, Shooter=%d, downloads=%d", got, searchHits, shooterHits, downloads)
	}
}

func TestOnlineSubtitleDownloadRejectsUnexpectedHost(t *testing.T) {
	var requests int
	services := SubtitleServices{Open: OpenSubtitleCredentials{Key: "fixture"},
		HTTP: &http.Client{Transport: subtitleRoundTrip(func(r *http.Request) (*http.Response, error) {
			requests++
			return subtitleResponse(r, 200, `{"link":"https://opensubtitles.com.example/secret"}`), nil
		})}}
	if _, err := services.Download(t.Context(), OnlineSubtitle{Provider: OpenSubtitles, id: 1}); err != ErrSubtitleService || requests != 1 {
		t.Fatalf("unexpected link: read %d requests, error %v", requests, err)
	}
}
