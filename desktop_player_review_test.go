package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
)

func TestDockerDesktopPlayerReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("desktop design capture runs on Linux GTK")
	}
	useMPV := os.Getenv("WAYGU_TEST_DESKTOP_MPV") == "1" && nsgotest.Target() != ""
	config, secrets := homeReviewSources(t)
	entry, client, series, seasons := reviewSeriesFixture(t, config, secrets, "emby")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	first, episodes := seriesFixtureEpisodes(t, ctx, client, seasons, 1, 5)
	_, second := seriesFixtureEpisodes(t, ctx, client, seasons, 2, 2)
	episode := episodes[1]
	catalog, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(catalog)
	config[mpvKey] = fmt.Sprint(useMPV)
	config[autoNextKey] = "false"
	config[danmakuPreferencesKey] = `{"Opacity":80,"Bottom":false}`
	config[danmakuBindingKey(route{source: entry, item: episode})] = `{"episodeId":840150952}`
	secrets[dandanCredentialKey] = `{"AppID":"review-app","Secret":"review-secret"}`
	_, poster := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	poster, err = client.Detail(ctx, poster)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, poster.Backdrop, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	thumbnail, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || len(thumbnail) == 0 {
		t.Fatalf("read preview still: HTTP %d, %v", response.StatusCode, err)
	}
	newMedia := func(provider media.Provider, address string, device media.Device, session media.Session) (media.Client, error) {
		client, err := media.NewClient(provider, address, device, session)
		if err != nil {
			return nil, err
		}
		return tvPlayerPreviewFixture{Client: client, VersionClient: client.(media.VersionClient), frame: thumbnail}, nil
	}
	newDandan := func(credentials danmaku.DandanCredentials) *danmaku.DandanClient {
		return danmaku.NewDandanClient(credentials, dandanTestTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/v2/comment/840150952" {
				return nil, fmt.Errorf("unexpected danmaku request %q", req.URL.Path)
			}
			const data = `{"count":6,"comments":[
{"p":"0.1,5,16777215,u","m":"这段配乐很好听"},{"p":"0.5,1,16777215,u","m":"这里的细节很自然"},
{"p":"1.1,1,16764006,u","m":"前方高能"},{"p":"2.3,1,8240124,u","m":"灯光有层次"},
{"p":"4.8,5,16777215,u","m":"又看了一遍"},{"p":"9.2,1,16777215,u","m":"下一幕开始了"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(data)), Header: make(http.Header)}, nil
		}))
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomDesktop)
		cx.SetSubtreeAppearance(core.AppearanceLight)
		return appRootWithDandan(cx, newMedia, nil, newDandan)
	}, nsgotest.Size(1440, 1000), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:desktop-library-series")
	card := "id:title-" + series.ID
	app.WaitFor(card, 10*time.Second)
	app.Press(card)
	season := "id:child-" + first.ID
	app.WaitFor(season, 5*time.Second)
	app.Press(season)
	row := "id:child-" + episode.ID
	app.WaitFor(row, 5*time.Second)
	app.Press(row)
	app.WaitFor("id:desktop-player", 10*time.Second)
	app.WaitFor("id:video-player", 5*time.Second)
	app.Resize(1440, 810)
	if nsgotest.Target() == "" {
		id := app.FindMounted("id:video-player").ID
		app.Report(nsgotest.Event{Kind: nsgotest.EventLoad, ID: id, Duration: 24 * time.Second,
			Size: core.Size{Width: 640, Height: 360}, RateChangeable: true})
		app.Progress("id:video-player", 2*time.Second, 24*time.Second)
	}
	app.WaitUntil(12*time.Second, func() bool { return app.Find("id:player-position").Text != "00:00" })
	app.Key("space")
	app.WaitUntil(4*time.Second, func() bool { return app.Find("id:toggle-play").Props.Label == "播放" })
	app.WaitFor("id:desktop-episode-"+episode.ID, 5*time.Second)
	panel, pane := "id:desktop-player-sidebar", "id:desktop-player-video-pane"
	if width := app.Find(panel).Frame.Size.Width; width != 320 {
		t.Fatalf("sidebar width = %v, want 320", width)
	}
	if width := app.Find(pane).Frame.Size.Width; width != 1120 {
		t.Fatalf("playing area width = %v, want 1120", width)
	}
	if !app.Find("id:desktop-episode-" + episode.ID).Props.Selected {
		t.Fatal("the current episode is not selected")
	}
	capture := func(label string) {
		if nsgotest.Target() != "" {
			app.Screenshot("app-desktop-player-" + label)
		}
	}
	const timeline = "id:desktop-player-position-rail"
	if useMPV {
		buffer := app.WaitFor("id:desktop-player-buffered", 6*time.Second)
		if width := app.Find(timeline).Frame.Size.Width; buffer.Frame.Size.Width != width || buffer.Frame.Size.Height != 6 {
			t.Fatalf("buffer range track = %+v, want %v × 6", buffer.Frame, width)
		}
		app.WaitUntil(3*time.Second, func() bool {
			return slices.ContainsFunc(app.FindMounted("id:desktop-player-buffered").Ops, func(op core.DrawOp) bool {
				return op.W > 20 && op.H == 6
			})
		})
		capture("buffered")
	}
	if nsgotest.Target() != "" {
		app.PressAt(timeline, core.Point{X: 17, Y: 8})
		app.WaitUntil(5*time.Second, func() bool {
			at := app.Find("id:player-position").Text
			return strings.HasPrefix(at, "00:0")
		})
		app.Advance(350 * time.Millisecond)
	}
	app.HoverAt(timeline, core.Point{X: 648, Y: 8})
	app.WaitFor("id:desktop-player-seek-time", 4*time.Second)
	app.WaitFor("id:desktop-player-seek-thumbnail", 4*time.Second)
	capture("episodes")
	app.HoverOut()

	app.Press("id:desktop-player-tab-danmaku")
	app.WaitFor("id:desktop-danmaku-row-0", 5*time.Second)
	capture("danmaku")
	app.Press("id:desktop-danmaku-filter-scroll")
	app.WaitUntil(2*time.Second, func() bool { return !readDanmakuPreferences(app.Storage()).Scrolling })
	app.Press("id:desktop-danmaku-filter-scroll")
	app.Press("id:desktop-danmaku-palette")
	app.Press("role:menuitem 琥珀色")
	app.Press("role:menuitem 顶部")
	app.Type("id:desktop-danmaku-draft", "本地弹幕测试")
	app.Key("enter")
	app.WaitFor("text:本地弹幕测试", 5*time.Second)
	notesKey := "danmaku-local-notes-v1:" + danmakuItemKey(route{source: entry, item: episode})
	raw, _ := app.Storage().GetItem(notesKey)
	var notes []danmaku.Comment
	if err := json.Unmarshal([]byte(raw), &notes); err != nil || len(notes) != 1 ||
		notes[0].Mode != danmaku.Top || notes[0].Color != 0xfcd34d {
		t.Fatalf("saved note = %+v, %v", notes, err)
	}
	app.Press("id:desktop-player-tab-info")
	app.WaitFor("id:desktop-player-info", 3*time.Second)
	if useMPV {
		app.WaitFor("text:/内核.*libmpv/", 5*time.Second)
		app.WaitFor("text:丢帧", 3*time.Second)
	}
	capture("info")
	app.Key("tab")
	app.WaitForGone(panel, 4*time.Second)
	if width := app.Find(pane).Frame.Size.Width; width != 1440 {
		t.Fatalf("collapsed playback area width = %v, want 1440", width)
	}
	app.Key("tab")
	app.WaitFor(panel, 4*time.Second)
	app.Key("d")
	if app.Find("id:toggle-danmaku").Props.Selected {
		t.Fatal("D did not turn off danmaku")
	}
	app.Key("d")
	app.Key("]")
	app.WaitFor("id:desktop-player-speed >> text:1.25x", 3*time.Second)
	app.Key("up")
	if nsgotest.Target() == "" {
		app.Progress("id:video-player", 2*time.Second, 24*time.Second)
	}
	app.Key("right")
	if nsgotest.Target() == "" {
		video := app.FindMounted("id:video-player")
		if video.Video.Volume < .79 || video.Video.Volume > .81 {
			t.Fatalf("Up did not change volume: %v", video.Video.Volume)
		}
		if !slices.Contains(video.Seeks, 12*time.Second) {
			t.Fatalf("Right did not seek from two to twelve seconds: %v", video.Seeks)
		}
		windowRequests := len(app.Window)
		app.Key("f")
		if len(app.Window) != windowRequests+1 || app.Window[len(app.Window)-1].Action != nsgotest.WindowToggleFullScreen {
			t.Fatal("F did not request full screen")
		}
		app.SetEnvironment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans", FullScreen: true})
		app.Key("escape")
		if len(app.Window) != windowRequests+2 || app.Window[len(app.Window)-1].Action != nsgotest.WindowToggleFullScreen {
			t.Fatal("Escape did not request an exit from full screen")
		}
		app.SetEnvironment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"})
	}

	app.Press("id:desktop-player-tab-episodes")
	app.WaitFor("id:desktop-player-season", 5*time.Second)
	app.Press("id:desktop-player-season")
	app.WaitFor("role:menuitem 第 2 季", 5*time.Second)
	capture("season-menu")
	app.Press("role:menuitem 第 2 季")
	app.WaitFor("id:desktop-episode-"+second[0].ID, 5*time.Second)
	app.WaitFor("id:desktop-player-season >> text:第 2 季", 5*time.Second)
	capture("season-2")
	app.Press("id:desktop-episode-" + second[0].ID)
	app.WaitFor("text:第 2 季 · 第 1 集", 10*time.Second)
	app.WaitFor("id:desktop-player-next", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool { return app.Enabled("id:desktop-player-next") })
	app.Key("n")
	app.WaitFor("text:第 2 季 · 第 2 集", 10*time.Second)
	app.Press("id:close-player")
	app.WaitForGone("id:desktop-player", 5*time.Second)
}
