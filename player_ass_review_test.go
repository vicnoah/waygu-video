package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDockerASSDanmakuReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media servers")
	}
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("native comparison uses Linux GTK")
	}
	if target == "linux" {
		if _, err := exec.LookPath("xdotool"); err != nil || os.Getenv("DISPLAY") == "" {
			t.Skip("the native file picker test needs xdotool and an X11 display")
		}
	}
	const comments = `[Script Info]
ScriptType: v4.00+
PlayResX: 1280
PlayResY: 720
[V4+ Styles]
Format: Name, PrimaryColour, Alignment
Style: Default,&H00FFFFFF,8
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.10,0:00:04.10,Default,,0,0,0,,{\an8}ASS 顶部弹幕
Dialogue: 0,0:00:00.20,0:00:08.20,Default,,0,0,0,,{\move(1280,160,-200,160)\c&HFFDD99&}向左滚动
Dialogue: 0,0:00:00.25,0:00:08.25,Default,,0,0,0,,{\move(-200,240,1280,240)\c&H99DDFF&}向右滚动
Dialogue: 0,0:00:00.30,0:00:04.30,Default,,0,0,0,,{\an2}ASS 底部弹幕
`
	providers := []string{"emby", "jellyfin", "plex"}
	if target == "linux" {
		providers = providers[:1]
	}
	config, secrets := homeReviewSources(t)
	config[playbackPreferencesKey] = `{"KeepIntroCredits":true}`
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			_, movie := reviewFixtureItem(t, config, secrets, provider, "Waygu Track Test")
			portrait, landscape := core.Points(850), core.Points(390)
			if target == "linux" {
				portrait += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(core.AppearanceLight)
				return Root(cx)
			}, nsgotest.Size(390, portrait), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.Press("id:tab-search")
			app.Type("id:search-query", movie.Title)
			app.Press("id:search-source-" + provider)
			card := "id:title-" + provider + "-" + movie.ID
			app.WaitFor(card, 8*time.Second)
			app.Press(card)
			const play = "id:detail-play"
			app.WaitFor(play, 8*time.Second)
			if app.Exists("role:button /继续播放/") {
				app.LongPress(play)
				app.WaitFor("id:detail-from-start", 3*time.Second)
				app.Press("id:detail-from-start")
			} else {
				app.Press(play)
			}
			app.Resize(844, landscape)
			const video = "id:video-player"
			app.WaitFor(video, 8*time.Second)
			fixture := comments
			if target == "" {
				app.Play(video, core.VideoInfo{Duration: 24 * time.Second, Size: core.Size{Width: 320, Height: 180}})
				app.Progress(video, 2*time.Second, 24*time.Second)
				app.PauseVideo(video, core.VideoPauseControls)
			} else {
				const position = "id:player-position"
				app.WaitUntil(10*time.Second, func() bool {
					return app.Exists(position) && app.Find(position).Text >= "00:00:01"
				})
				app.Press("id:toggle-play")
				at, err := strconv.Atoi(strings.TrimPrefix(app.Find(position).Text, "00:00:"))
				if err != nil || at <= 0 || at >= 24 {
					t.Fatalf("unexpected native playback position: %q", app.Find(position).Text)
				}
				// Anchor the comments near the paused frame even when the fixture
				// server still has a resume point from another playback test.
				offset := max(0, at-2)
				fixture = strings.NewReplacer(
					"0:00:00.", fmt.Sprintf("0:00:%02d.", offset),
					"0:00:04.", fmt.Sprintf("0:00:%02d.", offset+4),
					"0:00:08.", fmt.Sprintf("0:00:%02d.", offset+8),
				).Replace(fixture)
			}
			app.Press("id:danmaku-sources")
			app.Press("id:danmaku-tab-source")
			app.WaitFor("text:未导入", time.Second)
			app.Press("id:danmaku-manual-match")
			app.WaitUntil(time.Second, func() bool { return len(app.QueryMounted("id:danmaku-search-title")) == 1 })
			app.Press("id:danmaku-manual-match")
			app.WaitUntil(time.Second, func() bool { return len(app.QueryMounted("id:danmaku-search-title")) == 0 })
			if target == "linux" {
				app.Screenshot("phone-danmaku-ass-source-empty")
			}

			pick := func(trigger, data string) {
				if target == "linux" {
					filename := filepath.Join(t.TempDir(), "review.ass")
					if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
					app.Press(trigger)
					chooseLinuxASSFile(t, filename)
					return
				}
				app.PickFiles = func(picker nsgotest.FilePick) []core.File {
					if !slices.Equal(picker.Types, []string{".xml", ".json", ".ass"}) {
						t.Errorf("danmaku file types = %v", picker.Types)
					}
					return []core.File{{Name: "review.ASS", Opener: func() (io.ReadCloser, error) {
						return io.NopCloser(strings.NewReader(data)), nil
					}}}
				}
				app.Press(trigger)
			}
			pick("id:danmaku-open-local-file", fixture)
			app.WaitFor("text:本地 ASS 文件", 3*time.Second)
			if count := app.Find("id:danmaku-local-count").Text; count != "4" {
				t.Fatalf("ASS comment count = %q", count)
			}
			const canvas = "id:danmaku-canvas"
			app.WaitFor(canvas, 3*time.Second)
			if target == "linux" {
				app.Screenshot("phone-danmaku-ass-imported")
			}
			app.Press("id:danmaku-toggle-local")
			app.WaitForGone(canvas, 3*time.Second)
			app.Press("id:danmaku-toggle-local")
			app.WaitFor(canvas, 3*time.Second)
			app.Press("id:close-danmaku-sources")
			app.WaitForGone("id:danmaku-sources-dialog", 3*time.Second)
			if target == "" {
				app.Progress(video, 2*time.Second, 24*time.Second)
				app.WaitFor("text:00:00:02", time.Second)
			}
			expectLane := func(wantTop core.Points) {
				t.Helper()
				app.WaitUntil(time.Second, func() bool {
					picture := app.Find("id:phone-player-picture").Frame
					wantFont := 17 * picture.Size.Height / 390
					return slices.ContainsFunc(app.FindMounted(canvas).Ops, func(op core.DrawOp) bool {
						top := picture.Origin.Y + core.Points(op.Y) - op.Font.Size
						return op.Text == "ASS 顶部弹幕" && math.Abs(float64(top-wantTop)) < 1 &&
							math.Abs(float64(op.Font.Size-wantFont)) < 1
					})
				})
			}
			expectLane(64)
			if target == "linux" {
				app.Screenshot("phone-danmaku-ass-playback")
			}
			if provider == "emby" {
				for _, ratio := range []string{"crop", "2-35-1"} {
					app.Press("id:phone-player-fit")
					app.Press("id:phone-player-ratio-" + ratio)
					app.WaitForGone("id:phone-player-ratio-panel", 3*time.Second)
					expectLane(64)
					if target == "linux" && ratio == "crop" {
						app.Screenshot("phone-danmaku-ass-fill")
					}
				}
				if target == "" {
					app.SetEnvironment(core.Environment{Locale: "zh-Hans", SafeArea: core.Edges{Top: 12}})
					expectLane(76)
				}
			}
			app.Press("id:danmaku-sources")
			pick("id:danmaku-import-file", "[Events]\nDialogue: malformed\n")
			app.WaitFor("text:无法从所选文件读取弹幕。", 3*time.Second)
			if app.Find("id:danmaku-local-count").Text != "4" {
				t.Fatal("invalid file disturbed the existing track")
			}
		})
	}
}

func chooseLinuxASSFile(t *testing.T, filename string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	xdotool := func(args ...string) string {
		t.Helper()
		output, err := exec.CommandContext(ctx, "xdotool", args...).CombinedOutput()
		if err != nil {
			windows, _ := exec.CommandContext(t.Context(), "xwininfo", "-root", "-tree").CombinedOutput()
			t.Fatalf("xdotool %v: %v: %s\n%s", args, err, output, windows)
		}
		return strings.TrimSpace(string(output))
	}
	window := xdotool("search", "--sync", "--all", "--onlyvisible", "--pid", strconv.Itoa(os.Getpid()), "--name", "^导入弹幕文件$")
	xdotool("windowfocus", "--sync", window, "key", "--clearmodifiers", "ctrl+l", "sleep", "0.3", "key", "ctrl+a")
	xdotool("type", "--clearmodifiers", filename)
	// GtkFileChooser can append a completion while xdotool types a directory.
	xdotool("key", "--clearmodifiers", "shift+End", "Delete")
	xdotool("sleep", "0.5", "key", "--clearmodifiers", "Return")
}
