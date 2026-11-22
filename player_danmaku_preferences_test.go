package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestPlayerDanmakuChangesApplyAndPersist(t *testing.T) {
	entry := source{ID: "danmaku", Provider: media.Jellyfin, Address: "https://media.example.test", Name: "Fixture",
		Libraries: []sourceLibrary{{Library: media.Library{ID: "movies"}}}}
	config, _ := json.Marshal([]source{entry})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return dandanVideoFixture{fixtureServer: fixtureServer{session: session}, url: "https://media.example.test/video.mp4"}, nil
		}, nil)
	}, nsgotest.Offscreen(), nsgotest.Size(1000, 850),
		nsgotest.Storage(map[string]string{sourcesKey: string(config)}),
		nsgotest.Secrets(map[string]string{"danmaku": "fixture-token"}))
	app.PickFiles = func(nsgotest.FilePick) []core.File {
		return []core.File{{Name: "fixture.xml", Opener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(`<i>
				<d p="0,5,25,16777215">Hello</d>
				<d p="0.1,5,25,16777215">Hello</d>
				<d p="0,5,25,16777215">Spoiler</d>
				<d p="0,5,25,16777215">42</d>
				<d p="0,5,25,16711680">Colored</d>
			</i>`)), nil
		}}}
	}
	app.WaitFor("id:title-test-film", time.Second)
	app.Press("id:title-test-film")
	app.WaitFor("text:Fixture synopsis", time.Second)
	play := func() {
		app.Press("id:detail-play")
		app.WaitFor("id:video-player", time.Second)
		app.Play("id:video-player", core.VideoInfo{Duration: 3 * time.Second, Size: core.Size{Width: 1280, Height: 720}})
		app.PauseVideo("id:video-player", core.VideoPauseControls)
	}
	play()
	app.Press("id:load-danmaku")
	app.WaitFor("id:danmaku-canvas", time.Second)
	hasComment := func(text string) bool {
		for _, operation := range app.FindMounted("id:danmaku-canvas").Ops {
			if operation.Text == text {
				return true
			}
		}
		return false
	}
	app.WaitUntil(time.Second, func() bool { return hasComment("Hello ×2") && hasComment("Spoiler") && hasComment("Colored") })
	app.Press("id:danmaku-sources")
	app.Press("id:danmaku-later")
	app.Find("text:Time offset +0.5 s")
	app.WaitUntil(time.Second, func() bool { return !hasComment("Hello ×2") })
	app.Progress("id:video-player", time.Second, 3*time.Second)
	app.WaitUntil(time.Second, func() bool { return hasComment("Hello ×2") && hasComment("42") })
	var firstFontSize core.Points
	for _, operation := range app.FindMounted("id:danmaku-canvas").Ops {
		if operation.Text == "Hello ×2" {
			firstFontSize = operation.Font.Size
			break
		}
	}
	app.AccessibilityAction("id:danmaku-opacity-thumb", core.ActionDecrement)
	app.AccessibilityAction("id:danmaku-font-size-thumb", core.ActionIncrement)
	app.WaitUntil(time.Second, func() bool {
		var sizeChanged, opacityChanged bool
		for _, operation := range app.FindMounted("id:danmaku-canvas").Ops {
			sizeChanged = sizeChanged || (operation.Text == "Hello ×2" && operation.Font.Size > firstFontSize)
			opacityChanged = opacityChanged || operation.Color == core.RGBA(0xffffff, .85)
		}
		return sizeChanged && opacityChanged
	})
	app.Press("id:danmaku-colored")
	app.WaitUntil(time.Second, func() bool { return !hasComment("Colored") })
	app.Press("id:danmaku-tab-filters")
	app.Type("id:danmaku-keywords", "spoiler")
	app.Type("id:danmaku-patterns", "[")
	app.Press("id:danmaku-save-filters")
	app.WaitFor("text:Invalid regular expression [", time.Second)
	if !hasComment("Spoiler") {
		t.Fatal("an invalid regular expression applied part of the draft")
	}
	app.Clear("id:danmaku-patterns")
	app.Type("id:danmaku-patterns", `^\d+$`)
	app.Press("id:danmaku-save-filters")
	app.WaitUntil(time.Second, func() bool { return hasComment("Hello ×2") && !hasComment("Spoiler") && !hasComment("42") })
	app.Press("id:danmaku-deduplicate")
	app.WaitUntil(time.Second, func() bool { return hasComment("Hello") && !hasComment("Hello ×2") })

	data, _ := app.Storage().GetItem(danmakuPreferencesKey)
	var prefs danmakuPreferences
	if err := json.Unmarshal([]byte(data), &prefs); err != nil || prefs.Keywords != "spoiler" ||
		prefs.Patterns != `^\d+$` || prefs.Colored || prefs.Deduplicate || prefs.Opacity != 85 || prefs.FontScale != 105 {
		t.Fatalf("saved preferences = %+v, %v", prefs, err)
	}
	key := "danmaku-offset-v1:" + danmakuItemKey(route{source: entry, item: media.Item{ID: "test-film"}})
	if value, _ := app.Storage().GetItem(key); value != "500" {
		t.Fatalf("saved time offset = %q", value)
	}
	app.Press("id:close-danmaku-sources")
	app.Advance(250 * time.Millisecond)
	app.WaitForGone("id:danmaku-sources-dialog", time.Second)
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", time.Second)
	play()
	app.Press("id:danmaku-sources")
	app.WaitFor("text:Time offset +0.5 s", time.Second)
	app.Press("id:danmaku-reset-offset")
	if _, found := app.Storage().GetItem(key); found {
		t.Fatal("reset left a saved offset for this item")
	}
}
