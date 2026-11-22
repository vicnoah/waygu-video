package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

type playbackNetworkFixture struct {
	fixtureServer
	requests chan<- media.PlaybackOptions
	file     string
}

func (f playbackNetworkFixture) Playback(_ context.Context, item media.Item, options media.PlaybackOptions) (media.Stream, error) {
	f.requests <- options
	return media.Stream{ItemID: item.ID, URL: f.file, Method: media.DirectPlay, Duration: 3 * time.Second}, nil
}

func TestPlaybackUsesNetworkQuality(t *testing.T) {
	target := nsgotest.Target()
	want := os.Getenv("WAYGU_TEST_ANDROID_NETWORK")
	if target != "" && (target != "android" || (want != "default" && want != "cellular")) {
		t.Skip("requires the headless host or an Android emulator with a configured network")
	}
	file := filepath.Join(t.TempDir(), "network-fixture.mp4")
	if err := os.WriteFile(file, nativePlaybackFixture, 0600); err != nil {
		t.Fatal(err)
	}
	for _, network := range []struct {
		name     string
		cellular bool
		bitrate  int
		height   int
	}{
		{name: "wifi", bitrate: 20_000_000, height: 1080},
		{name: "cellular", cellular: true, bitrate: 8_000_000, height: 720},
	} {
		if target == "android" && network.cellular != (want == "cellular") {
			continue
		}
		t.Run(network.name, func(t *testing.T) {
			requests := make(chan media.PlaybackOptions, 4)
			var host *core.Host
			if target == "" {
				host = core.NewHost()
				host.Handle(androidCellularHostCall, func(call *core.HostCall) { call.Resolve(network.cellular) })
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
					return playbackNetworkFixture{fixtureServer: fixtureServer{session: session}, requests: requests, file: file}, nil
				}, nil)
			}, nsgotest.Environment(core.Environment{OS: core.OSAndroid, Locale: "zh-Hans"}), nsgotest.Size(390, 780),
				nsgotest.Host(host), nsgotest.Secrets(map[string]string{"source": "fixture-token"}),
				nsgotest.Storage(map[string]string{
					sourcesKey:             `[{"ID":"source","Provider":"jellyfin","Address":"https://example.test","Name":"Fixture","Libraries":[{"ID":"movies"}]}]`,
					bitrateKey:             "20000000",
					playbackPreferencesKey: `{"CellularBitrate":8000000,"WarnOnCellular":true}`,
				}))
			title := "id:title-test-film"
			if target == "android" {
				title = "id:home-hero-details"
			}
			app.WaitFor(title, 3*time.Second)
			app.Press(title)
			app.WaitFor("id:detail-play", 2*time.Second)
			app.Press("id:detail-play")
			if network.cellular {
				app.WaitFor("id:playback-cellular-warning", 2*time.Second)
				if len(requests) != 0 || app.Exists("id:video-player") {
					t.Fatal("playback started before the user answered the mobile data warning")
				}
				if target == "android" {
					app.Advance(300 * time.Millisecond)
					app.Screenshot("playback-cellular-warning")
				}
				app.Press("id:playback-cellular-cancel")
				app.WaitFor("id:detail-play", 2*time.Second)
				if len(requests) != 0 {
					t.Fatal("cancelling the warning opened a media stream")
				}
				app.Press("id:detail-play")
				app.WaitFor("id:playback-cellular-warning", 2*time.Second)
				app.Press("id:playback-cellular-continue")
			}
			app.WaitFor("id:video-player", 2*time.Second)
			if len(requests) != 1 {
				t.Fatalf("expected one request, got %d", len(requests))
			}
			if got := <-requests; got.MaxBitrate != network.bitrate || got.MaxHeight != network.height {
				t.Fatalf("playback requested %d bps at %dp, want %d bps at %dp",
					got.MaxBitrate, got.MaxHeight, network.bitrate, network.height)
			}
		})
	}
}

func TestAndroidDefaultPlaybackNetwork(t *testing.T) {
	want := os.Getenv("WAYGU_TEST_ANDROID_NETWORK")
	if nsgotest.Target() != "android" || (want != "cellular" && want != "default") {
		t.Skip("requires a packaged Android runtime and a configured emulator network")
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		network := core.NewSignal(cx, "checking")
		cx.CallHostTimeout(androidCellularHostCall, nil, 1500*time.Millisecond, func(result any, err error) {
			if cellular, ok := result.(bool); err == nil && ok {
				if cellular {
					network.Set("cellular")
				} else {
					network.Set("default")
				}
			} else {
				if err != nil {
					network.Set(err.Error())
				} else {
					network.Set("network query returned a non-boolean response")
				}
			}
		})
		return core.Text{ContentFunc: network.Get}
	})
	app.WaitFor("text:"+want, 4*time.Second)
}
