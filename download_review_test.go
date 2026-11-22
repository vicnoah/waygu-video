package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerOfflineDownloadReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	for i, entry := range sources {
		t.Run(entry.ID, func(t *testing.T) {
			client, movie := reviewFixtureItem(t, config, secrets, entry.ID, "Waygu Resume Test")
			base, err := url.Parse(entry.Address)
			if err != nil {
				t.Fatal(err)
			}
			var offline, slow atomic.Bool
			var attempts atomic.Int32
			forward := httputil.NewSingleHostReverseProxy(base)
			rewrite := forward.Director
			forward.Director = func(request *http.Request) {
				rewrite(request)
				request.Host = base.Host
				request.Header["X-Forwarded-For"] = nil
			}
			forward.ModifyResponse = func(response *http.Response) error {
				if slow.Load() && response.Request.Header.Get("Range") != "" &&
					strings.HasPrefix(response.Header.Get("Content-Type"), "video/") {
					response.Body = pacedFixtureBody{ReadCloser: response.Body, ctx: response.Request.Context()}
				}
				return nil
			}
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if offline.Load() {
					attempts.Add(1)
					http.Error(w, "fixture disconnected", http.StatusServiceUnavailable)
					return
				}
				forward.ServeHTTP(w, r)
			}))
			t.Cleanup(gateway.Close)
			entry.Address = gateway.URL
			id := downloadID(mediaAccountKey(entry), movie.ID)
			encoded, err := json.Marshal([]source{entry})
			if err != nil {
				t.Fatal(err)
			}
			state := map[string]string{sourcesKey: string(encoded)}
			var dataDir string
			start := func(t *testing.T, storage map[string]string, previousData string) *nsgotest.App {
				t.Helper()
				height := core.Points(1380)
				if nsgotest.Target() == "linux" {
					height += 37
				}
				return nsgotest.Start(t, func(cx *core.Scope) core.Node {
					cx.SetSubtreeAppearance(reviewAppearances[i%len(reviewAppearances)].appearance)
					dataDir = cx.DataDir()
					if previousData != "" {
						if err := os.CopyFS(dataDir, os.DirFS(previousData)); err != nil {
							t.Fatal("copy data for restart:", err)
						}
					}
					return Root(cx)
				}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
					nsgotest.Idiom(core.IdiomPhone), nsgotest.Storage(storage), nsgotest.Secrets(secrets))
			}

			app := start(t, state, "")
			waitForHomeCatalogTitle(app, "id:title-"+movie.ID)
			app.Press("id:title-" + movie.ID)
			app.WaitFor("id:detail-download", 5*time.Second)
			app.Press("id:detail-download")
			app.WaitUntil(20*time.Second, func() bool {
				if app.Exists("text:下载失败") {
					queue, _ := app.Storage().GetItem(downloadQueueKey)
					var entries []downloadEntry
					_ = json.Unmarshal([]byte(queue), &entries)
					if len(entries) > 0 {
						t.Fatalf("download failed at %d/%d bytes: %s", entries[0].Transferred, entries[0].Total, entries[0].Failure)
					}
					t.Fatal("download failed")
				}
				return app.Exists("role:button 已下载")
			})
			app.Press("id:back")
			app.Press("id:tab-settings")
			app.Press("id:settings-downloads")
			app.WaitFor("id:download-play-"+id, 5*time.Second)
			if nsgotest.Target() != "" && i < 2 {
				_, other := reviewFixtureItem(t, config, secrets, entry.ID, "Waygu Track Test")
				otherID := downloadID(mediaAccountKey(entry), other.ID)
				app.Press("id:back")
				app.Press("id:tab-home")
				app.WaitFor("id:title-"+other.ID, 5*time.Second)
				app.Press("id:title-" + other.ID)
				slow.Store(true)
				app.WaitFor("id:detail-download", 5*time.Second)
				app.Press("id:detail-download")
				app.Press("id:back")
				app.Press("id:tab-settings")
				app.Press("id:settings-downloads")
				app.WaitFor("id:download-pause-"+otherID, time.Second)
				app.WaitUntil(4*time.Second, func() bool {
					text := app.Find("id:download-state-" + otherID).Text
					return strings.Contains(text, "/") && !strings.Contains(text, "0 B /")
				})
				app.Screenshot("settings-storage-" + reviewAppearances[i].name)
				app.Press("id:download-pause-" + otherID)
				app.WaitFor("id:download-resume-"+otherID, 3*time.Second)
				slow.Store(false)
				app.Press("id:download-resume-" + otherID)
				app.WaitFor("id:download-play-"+otherID, 10*time.Second)
			}
			raw, exists := app.Storage().GetItem(downloadQueueKey)
			if !exists || strings.Contains(raw, gateway.URL) || (secrets[entry.ID] != "" && strings.Contains(raw, secrets[entry.ID])) {
				t.Fatal("stored queue contains transient media addresses or credentials")
			}
			state[downloadQueueKey] = raw
			// Open a fresh app after denying every request to the media provider.
			offline.Store(true)
			app = start(t, state, dataDir)
			app.WaitFor("id:tab-library", time.Second)
			app.Press("id:tab-library")
			app.WaitFor("id:download-badge-"+movie.ID, 5*time.Second)
			app.Press("id:title-" + movie.ID)
			app.WaitFor("id:detail-play", 5*time.Second)
			app.Press("id:detail-play")
			video := app.WaitFor("id:video-player", 5*time.Second)
			app.WaitFor("text:离线播放", 5*time.Second)
			probe, err := exec.CommandContext(t.Context(), "ffprobe", "-v", "error", "-select_streams", "v:0",
				"-show_entries", "stream=height", "-of", "csv=p=0", video.Video.Source).Output()
			if err != nil || strings.TrimSpace(string(probe)) != "90" {
				t.Fatalf("downloaded file does not decode at the expected height: %q, %v", probe, err)
			}
			const checkpoint = 96 * time.Second
			if nsgotest.Target() == "" {
				app.Play("id:video-player", core.VideoInfo{Duration: 6 * time.Minute})
				app.Progress("id:video-player", checkpoint, 6*time.Minute)
			} else {
				app.WaitForGone("text:正在加载…", 10*time.Second)
				before := app.Find("id:player-position").Text
				app.WaitUntil(8*time.Second, func() bool { return app.Find("id:player-position").Text != before })
			}
			requests := attempts.Load()
			app.Advance(350 * time.Millisecond)
			if attempts.Load() != requests {
				t.Fatal("offline playback attempted to contact its media server")
			}
			app.Press("id:close-player")
			if nsgotest.Target() == "" {
				key := (pendingProgress{Account: mediaAccountKey(entry), Update: media.OfflineProgress{ItemID: movie.ID}}).key()
				if _, exists := app.Storage().GetItem(key); !exists {
					t.Fatal("offline playback did not queue its progress")
				}
				offline.Store(false)
				app.Advance(2 * time.Second)
				app.WaitUntil(8*time.Second, func() bool {
					_, exists := app.Storage().GetItem(key)
					return !exists
				})
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				detail, err := client.Detail(ctx, movie)
				if err != nil || detail.Position != checkpoint {
					t.Fatalf("media server position = %s, want %s: %v", detail.Position, checkpoint, err)
				}
			}
			app.Press("id:back")
			app.Press("id:tab-settings")
			app.Press("id:settings-downloads")
			app.Press("id:download-remove-" + id)
			app.Press("id:confirm-remove-download-" + id)
			app.WaitForGone("id:download-row-"+id, 5*time.Second)
		})
	}
}

type pacedFixtureBody struct {
	io.ReadCloser
	ctx context.Context
}

func (body pacedFixtureBody) Read(chunk []byte) (int, error) {
	select {
	case <-body.ctx.Done():
		return 0, body.ctx.Err()
	case <-time.After(400 * time.Millisecond):
		return body.ReadCloser.Read(chunk[:min(len(chunk), 64<<10)])
	}
}
