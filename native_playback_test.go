package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

//go:embed testdata/native-playback.mp4
var nativePlaybackFixture []byte

func TestNativeVideoDecodes(t *testing.T) {
	if nsgotest.Target() == "" {
		t.Skip("requires a native window target")
	}
	path := filepath.Join(t.TempDir(), "native-playback.mp4")
	if err := os.WriteFile(path, nativePlaybackFixture, 0600); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		status := core.NewSignal(cx, "opening")
		return core.View{Class: "flex-1", Children: []core.Node{
			core.Video{Source: path, Class: "flex-1", Muted: true,
				OnLoad: func(info core.VideoInfo) {
					if info.Size.Width == 160 && info.Size.Height == 90 && info.Duration >= 2900*time.Millisecond {
						status.Set("decoded")
					}
				},
				OnProgress: func(at, _ time.Duration) {
					if status.GetUntracked() == "decoded" && at > 200*time.Millisecond {
						status.Set("playing")
					}
				},
				OnError: func(error) { status.Set("media error") },
			},
			core.Text{ContentFunc: status.Get},
		}}
	})
	app.WaitFor("text:playing", 10*time.Second)
}
