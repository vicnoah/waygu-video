package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

//go:embed testdata/background-audio.mp4
var backgroundAudioFixture []byte

func TestAndroidBackgroundAudioPolicy(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("requires simulated window lifecycle and launcher replies")
	}
	for _, scenario := range []struct {
		name           string
		enabled, grant bool
	}{
		{name: "off"},
		{name: "foreground service active", enabled: true, grant: true},
		{name: "foreground service denied", enabled: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			host := core.NewHost()
			starts, stops := make(chan string, 4), make(chan string, 4)
			host.Handle(androidCellularHostCall, func(call *core.HostCall) { call.Resolve(false) })
			host.Handle(androidBackgroundSupport, func(call *core.HostCall) { call.Resolve(true) })
			host.Handle(androidBackgroundStart, func(call *core.HostCall) {
				fields, _ := call.Data.(map[string]any)
				token, _ := fields["token"].(string)
				starts <- token
				call.Resolve(scenario.grant)
			})
			host.Handle(androidBackgroundStop, func(call *core.HostCall) {
				token, _ := call.Data.(string)
				stops <- token
				call.Resolve(true)
			})
			env := core.Environment{OS: core.OSAndroid, Lifecycle: core.LifecycleActive}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
					return fixtureServer{session: session}, nil
				}, nil)
			}, nsgotest.Idiom(core.IdiomPhone), nsgotest.Environment(env), nsgotest.Size(390, 800),
				nsgotest.Host(host), nsgotest.Secrets(map[string]string{"source": "fixture-token"}),
				nsgotest.Storage(map[string]string{
					sourcesKey: `[{"ID":"source","Provider":"jellyfin","Address":"https://example.test","Name":"Fixture","Libraries":[{"ID":"movies"}]}]`,
					bitrateKey: "4000000",
				}))
			app.Press("id:tab-settings")
			app.Press("id:open-phone-playback-settings")
			const setting = "id:phone-background-audio"
			app.WaitFor(setting, time.Second)
			if app.Find(setting).Props.Checked {
				t.Fatal("background audio should initially be off")
			}
			if scenario.enabled {
				app.Scroll("id:phone-playback-settings", 1200)
				app.Press(setting)
				if !readPlaybackPreferences(app.Storage()).BackgroundAudio {
					t.Fatal("background audio was not saved")
				}
			}
			app.Press("id:back")
			app.Press("id:tab-home")
			app.WaitFor("id:title-test-film", time.Second)
			app.Press("id:title-test-film")
			app.WaitFor("id:detail-play", time.Second)
			app.Press("id:detail-play")
			const video = "id:video-player"
			app.WaitFor(video, 3*time.Second)
			app.Play(video, core.VideoInfo{Duration: time.Minute})
			var token string
			if scenario.enabled {
				app.WaitUntil(2*time.Second, func() bool { return len(starts) == 1 })
				token = <-starts
				if token == "" {
					t.Fatal("the foreground service did not receive a playback identifier")
				}
			}
			env.Lifecycle = core.LifecycleBackground
			app.SetEnvironment(env)
			if got := app.FindMounted(video).Video.Paused; got == scenario.grant {
				t.Fatalf("paused in background = %t, foreground service active = %t", got, scenario.grant)
			}
			if scenario.grant {
				host.Send(androidBackgroundPause, "old-session")
				app.Advance(200 * time.Millisecond)
				if app.FindMounted(video).Video.Paused {
					t.Fatal("a stale media notification paused the current video")
				}
				host.Send(androidBackgroundPause, token)
				app.WaitUntil(2*time.Second, func() bool { return app.FindMounted(video).Video.Paused })
			}
			env.Lifecycle = core.LifecycleActive
			app.SetEnvironment(env)
			if !app.FindMounted(video).Video.Paused {
				t.Fatal("returning to the foreground resumed a paused video")
			}
			if scenario.enabled {
				app.WaitUntil(2*time.Second, func() bool { return len(stops) == 1 })
				if got := <-stops; got != token {
					t.Fatal("the foreground service was released for the wrong video")
				}
			} else if len(starts) != 0 {
				t.Fatal("background audio started with its preference off")
			}
		})
	}
}

func TestAndroidBackgroundAudioNative(t *testing.T) {
	mode := os.Getenv("WAYGU_TEST_ANDROID_BACKGROUND_AUDIO")
	if nsgotest.Target() != "android" || (mode != "on" && mode != "off") {
		t.Skip("requires the Android media service and the external lifecycle driver")
	}
	var background atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "background-audio.mp4", time.Time{}, bytes.NewReader(backgroundAudioFixture))
	}))
	t.Cleanup(server.Close)
	requests := make(chan media.PlaybackOptions, 4)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.Effect(func() { background.Store(cx.Env().Lifecycle() == core.LifecycleBackground) })
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return playbackNetworkFixture{fixtureServer: fixtureServer{session: session}, requests: requests,
				file: server.URL + "/background-audio.mp4"}, nil
		}, nil)
	}, nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Secrets(map[string]string{"source": "fixture-token"}),
		nsgotest.Storage(map[string]string{
			sourcesKey: `[{"ID":"source","Provider":"jellyfin","Address":"https://example.test","Name":"Fixture","Libraries":[{"ID":"movies"}]}]`,
		}))
	app.Press("id:tab-settings")
	app.Press("id:open-phone-playback-settings")
	app.Scroll("id:phone-playback-settings", 1200)
	const setting = "id:phone-background-audio"
	app.WaitFor(setting, 3*time.Second)
	if mode == "on" {
		app.Press(setting)
	}
	app.Screenshot("android-background-audio-" + mode)
	app.Press("id:back")
	app.Press("id:tab-home")
	app.WaitFor("id:home-hero-details", 5*time.Second)
	app.Press("id:home-hero-details")
	app.WaitFor("id:detail-play", 5*time.Second)
	app.Press("id:detail-play")
	const video, position = "id:video-player", "id:player-position"
	app.WaitFor(video, 8*time.Second)
	app.WaitUntil(8*time.Second, func() bool { return app.Find(position).Text != "00:00:00" })
	videoID := app.FindMounted(video).ID
	fmt.Println("WAYGU_AUDIO_READY")
	app.WaitUntil(12*time.Second, background.Load)
	if mode == "on" {
		at := app.Find(position).Text
		app.WaitUntil(5*time.Second, func() bool {
			return !app.FindMounted(video).Video.Paused && app.Find(position).Text != at
		})
		fmt.Println("WAYGU_AUDIO_RUNNING")
	}
	app.WaitUntil(10*time.Second, func() bool { return app.FindMounted(video).Video.Paused })
	fmt.Println("WAYGU_AUDIO_PAUSED")
	app.WaitUntil(12*time.Second, func() bool { return !background.Load() })
	if app.FindMounted(video).ID != videoID {
		t.Fatal("returning to the foreground replaced the video")
	}
	if !app.FindMounted(video).Video.Paused {
		t.Fatal("returning to the foreground resumed a paused video")
	}
	if app.Find(position).Text == "00:00:00" {
		t.Fatal("returning to the foreground reset the playback position")
	}
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", 4*time.Second)
}
