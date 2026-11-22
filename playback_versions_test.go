package main

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPlaybackVersionSelectionAndRememberedFallback(t *testing.T) {
	requests := make(chan media.PlaybackOptions, 8)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return versionFixtureServer{fixtureServer{session: session}, requests}, nil
		}, nil)
	}, nsgotest.Size(820, 900), nsgotest.Offscreen(),
		nsgotest.Storage(map[string]string{sourcesKey: `[{"ID":"example","Provider":"jellyfin","Address":"https://example.test",
			"Name":"Example","Libraries":[{"ID":"movies"}]}]`}),
		nsgotest.Secrets(map[string]string{"example": "fixture-token"}))

	app.WaitFor("id:title-test-film", time.Second)
	app.Press("role:tab Settings")
	app.Press("id:open-phone-playback-settings")
	app.Scroll("id:phone-playback-settings", 1400)
	app.Press("id:phone-playback-version-policy")
	app.Press("role:option Remember the last version")
	if policy, _ := app.Storage().GetItem(versionPolicyKey); policy != string(media.VersionRemember) {
		t.Fatal("preferred playback version was not saved")
	}
	app.Press("id:back")
	app.Press("role:tab Home")
	app.WaitFor("id:title-test-film", time.Second)
	app.Press("id:title-test-film")
	app.WaitFor("text:Fixture synopsis", time.Second)
	app.WaitFor("id:playback-version", time.Second)
	app.Press("id:playback-version")
	app.Press("id:playback-version-1")

	expectRequest := func(id string) {
		t.Helper()
		select {
		case options := <-requests:
			if options.SourceID != id || options.VersionPolicy != media.VersionRemember {
				t.Fatalf("playback request for %q with %q, want %q with remembered versions", options.SourceID, options.VersionPolicy, id)
			}
		case <-time.After(time.Second):
			t.Fatal("no playback request")
		}
	}
	load := func(filename string) {
		t.Helper()
		view := app.WaitFor("id:video-player", time.Second)
		if view.Video.Source != "https://example.test/"+filename+".mp4" {
			t.Fatalf("unexpected video URL for %s", filename)
		}
		app.Play("id:video-player", core.VideoInfo{Duration: time.Minute})
	}
	current := route{source: source{ID: "example"}, item: media.Item{ID: "test-film"}}
	key := versionHistoryKey(current)
	app.Press("id:detail-play")
	expectRequest("small")
	load("small")
	if remembered, _ := app.Storage().GetItem(key); remembered != "small" {
		t.Fatal("played version was not remembered")
	}

	app.Press("id:close-player")
	app.WaitFor("text:Fixture synopsis", time.Second)
	app.Press("id:detail-play")
	expectRequest("small")
	load("small")
	app.Press("id:close-player")
	app.WaitFor("text:Fixture synopsis", time.Second)
	if err := app.Storage().SetItem(key, "removed"); err != nil {
		t.Fatal(err)
	}
	app.Press("id:detail-play")
	expectRequest("removed")
	expectRequest("")
	load("large")
	if remembered, _ := app.Storage().GetItem(key); remembered != "large" {
		t.Fatal("a successful fallback did not replace the stale preference")
	}
}

type versionFixtureServer struct {
	fixtureServer
	requests chan media.PlaybackOptions
}

func (versionFixtureServer) Versions(context.Context, media.Item) ([]media.Version, error) {
	return []media.Version{{ID: "small", Height: 720}, {ID: "large", Height: 1080}}, nil
}

func (f versionFixtureServer) Playback(_ context.Context, item media.Item, options media.PlaybackOptions) (media.Stream, error) {
	f.requests <- options
	id := options.SourceID
	if id == "" {
		id = "large"
	}
	if id != "small" && id != "large" {
		return media.Stream{}, media.ErrNoStream
	}
	return media.Stream{ItemID: item.ID, SourceID: id, Method: media.DirectPlay,
		URL: "https://example.test/" + id + ".mp4", Duration: time.Minute}, nil
}
