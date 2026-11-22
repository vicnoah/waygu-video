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

type tvPlayerPreviewFixture struct {
	media.Client
	media.VersionClient
	frame []byte
}

func (f tvPlayerPreviewFixture) SeekPreviews(context.Context, media.Item, string) (media.SeekPreview, error) {
	return func(context.Context, time.Duration) ([]byte, error) { return f.frame, nil }, nil
}

func TestDockerTVPlayerReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("the desktop capture harness requires GTK")
	}

	config, secrets := homeReviewSources(t)
	client, item := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	single, err := json.Marshal(sources[:1])
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(single)
	config[danmakuPreferencesKey] = `{"Opacity":80}`
	config[danmakuBindingKey(route{source: sources[0], item: item})] = `{"episodeId":840150933}`
	secrets[dandanCredentialKey] = `{"AppID":"review-app","Secret":"review-secret"}`
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	movie, err := client.Detail(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, movie.Backdrop, nil)
	if err != nil {
		t.Fatal("the test fixture has no backdrop")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal("request the fixture backdrop:", err)
	}
	thumbnail, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || len(thumbnail) == 0 {
		t.Fatalf("read fixture backdrop: HTTP %d, %v", response.StatusCode, err)
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
			if req.URL.Path != "/api/v2/comment/840150933" {
				return nil, fmt.Errorf("unexpected danmaku fixture path %s", req.URL.Path)
			}
			body := `{"count":4,"comments":[
{"p":"0.1,5,16777215,u","m":"肥牛视频播放测试"},{"p":"0.2,5,16764006,u","m":"前方高能"},
{"p":"0.3,1,10079231,u","m":"弹幕与画面同步"},{"p":"0.4,1,16751001,u","m":"下一段开始了"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}))
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomTV)
		cx.SetSubtreeAppearance(core.AppearanceLight)
		return appRootWithDandan(cx, newMedia, nil, newDandan)
	}, nsgotest.Size(1920, 1080), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.WaitFor("id:home-hero-details", 8*time.Second)
	app.Press("id:home-hero-details")
	app.WaitFor("id:detail-play", 5*time.Second)
	if app.Exists("id:detail-from-start") {
		app.Press("id:detail-from-start")
	} else {
		app.Press("id:detail-play")
	}
	app.WaitFor("id:tv-player", 10*time.Second)
	app.WaitFor("id:video-player", 5*time.Second)
	if nsgotest.Target() == "" {
		video := app.FindMounted("id:video-player")
		app.Report(nsgotest.Event{Kind: nsgotest.EventLoad, ID: video.ID, Duration: 24 * time.Second,
			Size: core.Size{Width: 320, Height: 180}, RateChangeable: true})
		app.Progress("id:video-player", 2*time.Second, 24*time.Second)
	}
	app.WaitFor("id:danmaku-canvas", 5*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		return app.Exists("id:player-position") && app.Find("id:player-position").Text != "00:00"
	})
	app.Focus("id:toggle-play")
	app.Key("enter")
	if label := app.Find("id:toggle-play").Props.Label; label != "播放" {
		t.Fatalf("remote Select did not pause playback: %q", label)
	}
	app.Key("down")
	app.WaitFor("id:tv-player-quick-settings", time.Second)
	app.Key("down")
	app.WaitFor("id:tv-player-remote-hints", time.Second)
	if got := app.Find("id:tv-player").Frame.Size.Height; got != 1080 {
		t.Fatalf("player client height = %v, want 1080", got)
	}
	opacity := app.Find("id:tv-player-opacity-value").Text
	if opacity != "80%" {
		t.Fatalf("initial opacity is %q", opacity)
	}
	if width := app.Find("id:tv-player-quick-settings").Frame.Size.Width; width != 560 {
		t.Fatalf("quick settings width = %v, want 560", width)
	}
	if got := app.Find("id:tv-player-progress").Frame.Size.Width; got != 1224 {
		t.Fatalf("timeline width = %v, want 1224", got)
	}
	if nsgotest.Target() != "" {
		app.Screenshot("app-tv-player-quick-settings")
	}
	app.Key("left")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:tv-player-opacity-value").Text == "70%" })
	if got := readDanmakuPreferences(app.Storage()).Opacity; got != 70 {
		t.Fatalf("remote opacity step was not saved, got %d", got)
	}
	app.Key("right")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:tv-player-opacity-value").Text == "80%" })
	app.Key("escape")
	app.WaitForGone("id:tv-player-quick-settings", time.Second)

	if nsgotest.Target() != "" {
		app.Screenshot("app-tv-player-osd")
	}
	app.Key("right")
	previewTime := app.WaitFor("id:tv-player-seek-time", time.Second).Text
	if previewTime == app.Find("id:player-position").Text {
		t.Fatalf("the preview time %q replaced the current playback position", previewTime)
	}
	app.WaitFor("id:tv-player-seek-thumbnail", time.Second)
	if nsgotest.Target() != "" {
		app.Screenshot("app-tv-player-seek")
	}
	app.Key("down")
	app.WaitFor("id:tv-player-quick-settings", time.Second)
	app.Focus("id:tv-player-opacity")
	if nsgotest.Target() != "" {
		app.Screenshot("app-tv-player-scrub-settings")
	}
	app.FastForward(tvSeekDelay + 100*time.Millisecond)
	app.WaitForGone("id:tv-player-seek-time", time.Second)
	if nsgotest.Target() == "" && !slices.Contains(app.FindMounted("id:video-player").Seeks, 12*time.Second) {
		t.Fatalf("right did not seek ten seconds: %v", app.FindMounted("id:video-player").Seeks)
	}
	app.Key("escape")
	app.WaitForGone("id:tv-player-quick-settings", time.Second)

	app.Key("up")
	app.WaitFor("id:player-tracks-dialog", 3*time.Second)
	if nsgotest.Target() != "" {
		app.Screenshot("app-tv-player-tracks")
	}
	app.Press("id:close-player-tracks")
	app.WaitForGone("id:player-tracks-dialog", 3*time.Second)
	app.Focus("id:toggle-play")
	app.Key("enter")
	app.FastForward(3200 * time.Millisecond)
	app.WaitForGone("id:tv-player-osd", 3*time.Second)

	app.Key("down")
	app.WaitFor("id:tv-player-quick-settings", 3*time.Second)
	if nsgotest.Target() == "" {
		app.Press("id:tv-player-speed")
		app.WaitFor("id:playback-speed-1.5", 3*time.Second)
		app.Press("id:playback-speed-1.5")
		app.Press("id:close-player-tracks")
		if got := app.Find("id:tv-player-speed-value").Text; got != "1.5x" {
			t.Fatalf("quick settings show %q after changing playback speed", got)
		}
	}
	app.Press("id:tv-player-more-settings")
	app.WaitFor("id:settings-add-source", 3*time.Second)
	app.Press("id:back")
	app.WaitFor("id:tv-player", 5*time.Second)
	app.Key("escape")
	app.WaitFor("id:tv-detail-hero", 5*time.Second)
}
