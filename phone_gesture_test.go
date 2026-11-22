package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPhoneGesturePreferencesAffectPlayback(t *testing.T) {
	target := nsgotest.Target()
	if target != "" && target != "android" {
		t.Skip("requires Android or the simulated phone player")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "gestures.mp4", time.Time{}, bytes.NewReader(nativePlaybackFixture))
	}))
	t.Cleanup(server.Close)
	requests := make(chan media.PlaybackOptions, 4)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return playbackNetworkFixture{fixtureServer: fixtureServer{session: session}, requests: requests,
				file: server.URL + "/gestures.mp4"}, nil
		}, nil)
	}, nsgotest.Idiom(core.IdiomPhone), nsgotest.Size(390, 800),
		nsgotest.Environment(core.Environment{Locale: "zh-Hans"}), nsgotest.Secrets(map[string]string{"source": "fixture-token"}),
		nsgotest.Storage(map[string]string{
			sourcesKey: `[{"ID":"source","Provider":"jellyfin","Address":"https://example.test","Name":"Fixture","Libraries":[{"ID":"movies"}]}]`,
		}))
	app.Press("id:tab-settings")
	app.Press("id:open-phone-playback-settings")
	app.Scroll("id:phone-playback-settings", 960)
	app.Press("id:phone-playback-gestures")
	app.WaitFor("id:phone-playback-gestures-dialog", 3*time.Second)
	if target != "" {
		app.Advance(450 * time.Millisecond)
		app.Screenshot("phone-gesture-settings-android")
	}
	app.Press("id:phone-gesture-brightness")
	app.Press("id:phone-gesture-seek")
	app.Press("id:phone-gestures-done")
	app.Advance(350 * time.Millisecond)
	app.WaitForGone("id:phone-playback-gestures-dialog", 3*time.Second)
	app.WaitFor("id:phone-playback-gestures >> text:音量", time.Second)
	app.Press("id:back")
	app.Press("id:tab-home")
	title := "id:title-test-film"
	if target == "android" {
		title = "id:home-hero-details"
	}
	app.WaitFor(title, 5*time.Second)
	app.Press(title)
	app.WaitFor("id:detail-play", 5*time.Second)
	app.Press("id:detail-play")
	if target == "" {
		app.Resize(844, 390)
	}

	const surface, video, rail = "id:phone-player-surface", "id:video-player", "id:phone-player-position-rail"
	app.WaitFor(surface, 10*time.Second)
	app.WaitFor(video, 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		size := app.Find(surface).Frame.Size
		return size.Width > size.Height
	})
	if target == "" {
		app.Play(video, core.VideoInfo{Duration: 3 * time.Second, RateChangeable: true})
	} else {
		app.WaitForGone("text:正在加载…", 8*time.Second)
	}
	app.Press("id:toggle-play")
	size := app.Find(surface).Frame.Size
	left := core.Point{X: size.Width * .25, Y: size.Height * .33}
	right := core.Point{X: size.Width * .75, Y: left.Y}
	down := core.Point{Y: size.Height * .23}
	app.DragAt(surface, left, down)
	if app.Exists("id:phone-player-brightness") || app.Exists("id:phone-player-dimming") {
		t.Fatal("left drag changed the picture after disabling that gesture")
	}
	before := app.Find("id:player-position").Text
	app.DragAt(surface, left, core.Point{X: 125})
	if after := app.Find("id:player-position").Text; after != before {
		t.Fatalf("disabled horizontal drag changed the position from %s to %s", before, after)
	}
	app.DragAt(surface, right, down)
	app.WaitFor("id:phone-player-volume", time.Second)
	width := app.Find(rail).Frame.Size.Width
	start, end := width*.15, width*.85
	if before != "00:00:00" {
		start, end = end, start
	}
	app.DragAt(rail, core.Point{X: start, Y: 8}, core.Point{X: end - start})
	app.WaitUntil(2*time.Second, func() bool {
		return app.Find("id:player-position").Text != before
	})
	if target != "" {
		app.Screenshot("phone-gesture-playback")
	}
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", 5*time.Second)
}
