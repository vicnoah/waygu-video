package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDockerTranscodedDownload(t *testing.T) {
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux" && target != "android") {
		t.Skip("requires the Docker fixtures and a local video decoder")
	}
	config, secrets := dockerReviewSources(t, false)
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	for index, entry := range sources {
		t.Run(entry.ID, func(t *testing.T) {
			client, movie := reviewFixtureItem(t, config, secrets, entry.ID, "Waygu Download Test")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			movie, err := client.Detail(ctx, movie)
			cancel()
			if err != nil || movie.Duration != 12*time.Second {
				t.Fatalf("read the 12-second download fixture: %s, %v", movie.Duration, err)
			}
			address, err := url.Parse(entry.Address)
			if err != nil {
				t.Fatal(err)
			}
			forward := httputil.NewSingleHostReverseProxy(address)
			direct := forward.Director
			forward.Director = func(request *http.Request) {
				direct(request)
				request.Host = address.Host
				request.Header["X-Forwarded-For"] = nil
			}
			var disconnected atomic.Bool
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if disconnected.Load() {
					http.Error(w, "fixture disconnected", http.StatusServiceUnavailable)
					return
				}
				forward.ServeHTTP(w, r)
			}))
			t.Cleanup(gateway.Close)
			entry.Address = gateway.URL
			encoded, err := json.Marshal([]source{entry})
			if err != nil {
				t.Fatal(err)
			}
			state := map[string]string{sourcesKey: string(encoded), playbackPreferencesKey: `{"DisableProgressRecording":true}`}
			if target == "android" {
				state[downloadWiFiKey] = "false"
			}
			theme := reviewAppearances[index%len(reviewAppearances)]
			start := func(state map[string]string, previousDir string, dataDir *string) *nsgotest.App {
				return nsgotest.Start(t, func(cx *core.Scope) core.Node {
					cx.SetSubtreeAppearance(theme.appearance)
					*dataDir = cx.DataDir()
					if previousDir != "" {
						if err := os.CopyFS(*dataDir, os.DirFS(previousDir)); err != nil {
							t.Fatal(err)
						}
					}
					return Root(cx)
				}, nsgotest.Size(390, 1417), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
					nsgotest.Idiom(core.IdiomPhone), nsgotest.Storage(state), nsgotest.Secrets(secrets))
			}
			var dir string
			app := start(state, "", &dir)
			openSettings := func() {
				app.Press("id:tab-settings")
				app.WaitFor("id:phone-settings-home", 5*time.Second)
				if target == "android" {
					app.Scroll("id:phone-settings-home", 440)
				}
				app.Press("id:settings-downloads")
			}
			openSettings()
			app.WaitFor("id:download-quality", 5*time.Second)
			app.Press("id:download-quality")
			app.Press("text:1080p · 10 Mbps")
			if value, _ := app.Storage().GetItem(downloadQualityKey); value != "1080" {
				t.Fatalf("default quality = %q", value)
			}
			app.Press("id:back")
			app.Press("id:tab-search")
			app.Type("id:search-query", movie.Title)
			app.Press("id:search-submit")
			app.WaitFor("id:title-"+movie.ID, 8*time.Second)
			app.Press("id:title-" + movie.ID)
			app.WaitFor("id:detail-download", 8*time.Second)
			app.Press("id:detail-download")
			app.WaitFor("id:detail-download-quality-1080", 5*time.Second)
			if !app.Find("id:detail-download-quality-1080").Props.Checked {
				t.Fatal("the download dialog did not select the saved default")
			}
			app.Press("id:detail-download-quality-720")
			if !app.Find("id:detail-download-quality-720").Props.Checked {
				t.Fatal("could not override the download quality")
			}
			if target == "linux" {
				app.Screenshot("download-quality-" + theme.name + "-" + entry.ID)
			}
			app.Press("id:detail-download-confirm")
			app.FastForward(350 * time.Millisecond)
			app.WaitForGone("id:detail-download-quality", 3*time.Second)
			app.WaitUntil(35*time.Second, func() bool {
				if app.Exists("text:下载失败") {
					raw, _ := app.Storage().GetItem(downloadQueueKey)
					var entries []downloadEntry
					_ = json.Unmarshal([]byte(raw), &entries)
					if len(entries) > 0 {
						t.Fatalf("transcoded download failed after %d bytes: %s", entries[0].Transferred, entries[0].Failure)
					}
					t.Fatal("transcoded download failed")
				}
				return app.Exists("role:button 已下载")
			})
			if value, _ := app.Storage().GetItem(downloadQualityKey); value != "1080" {
				t.Fatalf("single download changed the default quality to %q", value)
			}
			app.Press("id:back")
			openSettings()
			id := downloadID(mediaAccountKey(entry), movie.ID)
			label := app.WaitFor("id:download-state-"+id, 5*time.Second).Text
			if !strings.Contains(label, "720p") {
				t.Fatalf("completed row reads %q", label)
			}
			if target == "linux" {
				app.Screenshot("download-complete-" + theme.name + "-" + entry.ID)
			}
			raw, _ := app.Storage().GetItem(downloadQueueKey)
			if strings.Contains(raw, gateway.URL) || (secrets[entry.ID] != "" && strings.Contains(raw, secrets[entry.ID])) {
				t.Fatal("the download queue retained network credentials")
			}
			state[downloadQueueKey] = raw
			state[downloadQualityKey] = "1080"
			disconnected.Store(true)
			if target == "android" {
				app.Press("id:download-play-" + id)
			} else {
				var reopenedDir string
				app = start(state, dir, &reopenedDir)
				app.Press("id:tab-library")
				app.WaitFor("id:download-badge-"+movie.ID, 8*time.Second)
				app.Press("id:title-" + movie.ID)
				app.WaitFor("id:detail-play", 5*time.Second)
				app.Press("id:detail-play")
			}
			app.WaitFor("text:离线播放", 5*time.Second)
			video := app.WaitFor("id:video-player", 5*time.Second)
			if target != "android" {
				probeTranscodedDownload(t, video.Video.Source, 720, movie.Duration)
			}
			if target == "linux" {
				before := app.Find("id:player-position").Text
				app.WaitUntil(8*time.Second, func() bool { return app.Find("id:player-position").Text != before })
			}
			if target == "android" {
				position := func() string { return app.Find("id:player-position").Text }
				app.WaitUntil(8*time.Second, func() bool { return position() != "00:00:00" })
				if label := app.Find("id:phone-player-resolution").Text; label != "720p" {
					t.Fatalf("offline picture label = %q, want 720p", label)
				}
				if label := app.Find("id:phone-player-duration").Text; label != "00:00:12" {
					t.Fatalf("offline duration label = %q, want 00:00:12", label)
				}
				if position() >= "00:00:12" {
					app.Press("id:toggle-play")
				}
				const rail = "id:phone-player-position-rail"
				width := app.Find(rail).Frame.Size.Width
				app.DragAt(rail, core.Point{X: width / 4, Y: 8}, core.Point{X: width * 0.40})
				app.WaitUntil(6*time.Second, func() bool { return position() >= "00:00:09" })
				if app.Exists("text:播放器无法打开这个视频。") {
					t.Fatal("the decoder reported an error after seeking")
				}
				app.Screenshot("download-offline-android-" + entry.ID)
			}
			app.Press("id:close-player")
		})
	}
}

func probeTranscodedDownload(t *testing.T, path string, expectedHeight int, expectedDuration time.Duration) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "ffprobe", "-v", "error",
		"-show_entries", "stream=codec_name,height:format=duration", "-of", "json", path)
	raw, err := command.Output()
	if err != nil {
		t.Fatal("probe transcoded copy:", err)
	}
	var measured struct {
		Streams []struct {
			Codec  string `json:"codec_name"`
			Height int
		}
		Format struct{ Duration string }
	}
	if err := json.Unmarshal(raw, &measured); err != nil || len(measured.Streams) != 2 ||
		measured.Streams[0].Codec != "h264" || measured.Streams[0].Height != expectedHeight || measured.Streams[1].Codec != "aac" {
		t.Fatalf("transcoded copy did not contain the expected picture and audio: %s (%v)", raw, err)
	}
	seconds, err := strconv.ParseFloat(measured.Format.Duration, 64)
	if err != nil || seconds < expectedDuration.Seconds()-0.25 || seconds > expectedDuration.Seconds()+0.25 {
		t.Fatalf("transcoded duration = %v, expected %s", seconds, expectedDuration)
	}
	decode := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-ss",
		strconv.FormatFloat(seconds-1, 'f', 2, 64), "-i", path,
		"-t", "0.5", "-map", "0:v:0", "-map", "0:a:0", "-f", "null", "-")
	if output, err := decode.CombinedOutput(); err != nil {
		t.Fatalf("decode near the end of the transcoded copy: %v (%s)", err, output)
	}
}
