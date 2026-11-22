package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	var source string
	if nsgotest.Target() == "android" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, "native-playback.mp4", time.Time{}, bytes.NewReader(nativePlaybackFixture))
		}))
		t.Cleanup(server.Close)
		source = server.URL + "/native-playback.mp4"
	} else {
		source = filepath.Join(t.TempDir(), "native-playback.mp4")
		if err := os.WriteFile(source, nativePlaybackFixture, 0600); err != nil {
			t.Fatal(err)
		}
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		status := core.NewSignal(cx, "opening")
		return core.View{Class: "flex-1", Children: []core.Node{
			core.Video{Source: source, Class: "flex-1", Muted: true, ProgressInterval: 100 * time.Millisecond,
				OnLoadStart: func() { status.Set("loading media") },
				OnLoad: func(info core.VideoInfo) {
					if info.Size.Width == 160 && info.Size.Height == 90 && info.Duration >= 2900*time.Millisecond {
						status.Set("decoded")
						return
					}
					status.Set(fmt.Sprintf("unexpected media properties: %gx%g, %v", info.Size.Width, info.Size.Height, info.Duration))
				},
				OnProgress: func(at, _ time.Duration) {
					if status.GetUntracked() == "decoded" && at > 200*time.Millisecond {
						status.Set("playing")
					}
				},
				OnError: func(err error) { status.Set("media error: " + err.Error()) },
			},
			core.Text{ContentFunc: status.Get},
		}}
	})
	app.WaitFor("text:playing", 10*time.Second)
}
