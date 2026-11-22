package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
)

func TestDockerPhonePlayerDesignReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media servers")
	}
	config, secrets := homeReviewSources(t)
	client, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	stream, err := client.Playback(ctx, movie, media.PlaybackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	english := slices.IndexFunc(stream.Tracks, func(track media.Track) bool {
		return track.Type == "subtitle" && track.External && track.URL != ""
	})
	if english < 0 {
		t.Fatal("the Docker fixture has no external caption track")
	}
	subtitle := stream.Tracks[english].ID
	// Native test hosts use their real file picker. Load the same comments
	// through a saved DandanPlay match and a local transport in that host.
	localFixture := nsgotest.Target() == ""
	var dandanFactory danmakuFactory
	visibleSource := "local"
	if !localFixture {
		var sources []source
		if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
			t.Fatal(err)
		}
		var emby source
		for _, entry := range sources {
			if entry.ID == "emby" {
				emby = entry
				break
			}
		}
		if emby.ID == "" {
			t.Fatal("the capture has no Emby source")
		}
		config[danmakuBindingKey(route{source: emby, item: movie})] = `{"episodeId":840150933}`
		secrets[dandanCredentialKey] = `{"AppID":"review-app","Secret":"review-secret"}`
		transport := dandanTestTransport(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/api/v2/comment/840150933" {
				return nil, fmt.Errorf("unexpected danmaku fixture path %s", request.URL.Path)
			}
			body := `{"count":4,"comments":[
{"p":"0.1,5,16777215,u","m":"前方高能"},{"p":"0.2,5,16764006,u","m":"肥牛视频播放测试"},
{"p":"0.3,5,10079231,u","m":"本地弹幕与画面同步"},{"p":"0.4,5,16751001,u","m":"下一段开始了"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		dandanFactory = func(credentials danmaku.DandanCredentials) *danmaku.DandanClient {
			return danmaku.NewDandanClient(credentials, transport)
		}
		visibleSource = "remote"
	}
	portrait, landscape := core.Points(800), core.Points(390)
	if nsgotest.Target() == "linux" {
		portrait += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		cx.SetSubtreeAppearance(core.AppearanceLight)
		if dandanFactory != nil {
			return appRootWithDandan(cx, media.NewClient, nil, dandanFactory)
		}
		return Root(cx)
	}, nsgotest.Size(390, portrait), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.PickFiles = func(nsgotest.FilePick) []core.File {
		return []core.File{{Name: "player-review.xml", Opener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(`<i>
<d p="0.1,5,25,16777215,0,0,0,1">前方高能</d><d p="0.2,5,25,16764006,0,0,0,2">肥牛视频播放测试</d>
<d p="0.3,5,25,10079231,0,0,0,3">本地弹幕与画面同步</d><d p="0.4,5,25,16751001,0,0,0,4">下一段开始了</d>
</i>`)), nil
		}}}
	}
	app.Press("id:tab-search")
	app.Type("id:search-query", "Waygu Track Test")
	app.Press("id:search-source-emby")
	app.WaitFor("id:title-emby-"+movie.ID, 10*time.Second)
	app.Press("id:title-emby-" + movie.ID)
	app.WaitFor("id:detail-play", 10*time.Second)
	app.Press("id:detail-play")
	app.Resize(844, landscape)
	app.WaitFor("id:phone-player", 10*time.Second)
	app.WaitFor("id:video-player", 10*time.Second)
	if nsgotest.Target() == "" {
		app.Report(nsgotest.Event{Kind: nsgotest.EventLoad, ID: app.FindMounted("id:video-player").ID,
			Duration: 24 * time.Second, Size: core.Size{Width: 320, Height: 180}, RateChangeable: true})
		app.Progress("id:video-player", 2*time.Second, 24*time.Second)
	}
	app.WaitUntil(12*time.Second, func() bool {
		return app.Find("id:player-position").Text != "00:00:00"
	})
	app.WaitFor("id:phone-player-resolution", time.Second)
	if localFixture {
		app.Press("id:load-danmaku")
	}
	app.WaitFor("id:danmaku-canvas", 3*time.Second)
	capture := func(name string) {
		if nsgotest.Target() != "" {
			app.Advance(300 * time.Millisecond)
			app.Screenshot(name)
		}
	}
	app.Press("id:toggle-play")
	app.WaitFor("id:phone-player-chapter-ticks", 6*time.Second)
	capture("player")
	app.Press("id:phone-player-lock")
	app.WaitForGone("id:player-tracks", time.Second)
	app.WaitFor("id:phone-player-unlock", time.Second)
	capture("player-locked")
	app.Press("id:phone-player-unlock")
	app.WaitFor("id:player-tracks", time.Second)
	video := app.Find("id:video-player").ID
	for _, choice := range []struct {
		id    string
		mode  core.ResizeMode
		ratio float64
	}{
		{"crop", core.Cover, 0}, {"stretch", core.Stretch, 0}, {"16-9", core.Stretch, 16.0 / 9},
		{"4-3", core.Stretch, 4.0 / 3}, {"2-35-1", core.Stretch, 2.35}, {"original", core.Contain, 0},
	} {
		app.Press("id:phone-player-fit")
		app.WaitFor("id:phone-player-ratio-panel", time.Second)
		if choice.id == "4-3" {
			capture("player-ratios")
		}
		app.Press("id:phone-player-ratio-" + choice.id)
		app.WaitForGone("id:phone-player-ratio-panel", time.Second)
		picture := app.Find("id:phone-player-picture").Frame.Size
		want := choice.ratio
		if want == 0 {
			want = 844.0 / 390
		}
		if got := float64(picture.Width / picture.Height); math.Abs(got-want) > .01 {
			t.Fatalf("frame for %s: %vx%v, want ratio %v", choice.id, picture.Width, picture.Height, want)
		}
		if nsgotest.Target() == "" && app.FindMounted("id:video-player").Video.ResizeMode != choice.mode {
			t.Fatalf("video did not receive the %s fit", choice.id)
		}
		if app.Find("id:video-player").ID != video {
			t.Fatal("choosing a picture ratio remounted the video")
		}
		if choice.id == "4-3" {
			capture("player-picture-4-3")
		}
	}
	rateAvailable := app.Enabled("id:phone-player-rate")
	if rateAvailable {
		app.Press("id:phone-player-rate")
		app.WaitFor("id:phone-player-rate >> text:1.25x", time.Second)
	} else {
		t.Log("native media backend reported that rate control is unavailable")
	}
	app.Press("id:player-tracks")
	panel := app.WaitFor("id:player-tracks-dialog", time.Second)
	if panel.Frame.Size.Width != 360 {
		t.Fatalf("track panel width = %v", panel.Frame.Size.Width)
	}
	app.Press("id:subtitle-track-" + subtitle)
	app.WaitUntil(4*time.Second, func() bool { return app.Find("id:subtitle-track-" + subtitle).Props.Checked })
	capture("player-tracks-panel-subs")
	app.Press("id:player-subtitle-style")
	app.WaitFor("id:subtitle-font-size", time.Second)
	app.Press("id:player-subtitle-style-back")
	app.WaitFor("id:track-tab-audio", time.Second)
	app.Press("id:track-tab-audio")
	capture("player-tracks-panel-audio")
	app.Press("id:track-tab-quality")
	for _, preset := range []struct{ id, label string }{
		{"id:player-bitrate-20000000", "1080p · 20 Mbps"},
		{"id:player-bitrate-8000000", "720p · 8 Mbps"},
		{"id:player-bitrate-4000000", "480p · 4 Mbps"},
	} {
		option := app.WaitFor(preset.id, time.Second)
		if option.Props.Label != preset.label || option.Frame.Size.Height != 40 {
			t.Fatalf("quality option = %q at %v pt, want %q at 40 pt", option.Props.Label, option.Frame.Size.Height, preset.label)
		}
	}
	if original := app.Find("id:player-bitrate-0"); !original.Props.Checked || original.Frame.Size.Height != 48 {
		t.Fatal("the original quality should start selected in the 48 pt row")
	}
	app.WaitFor("text:原画 · "+strconv.Itoa(stream.Rendition.Height)+"p", time.Second)
	capture("player-tracks-panel-quality")
	app.Press("id:track-tab-speed")
	if rateAvailable {
		app.Press("id:playback-speed-1")
	}
	capture("player-tracks-panel-speed")
	app.Press("id:close-player-tracks")
	app.WaitForGone("id:player-tracks-dialog", time.Second)
	app.Press("id:danmaku-sources")
	app.WaitFor("id:danmaku-display-enabled", time.Second)
	capture("player-danmaku-panel-show")
	app.Press("id:danmaku-tab-source")
	app.WaitFor("id:danmaku-import-file", time.Second)
	capture("player-danmaku-panel-source")
	app.Press("id:danmaku-toggle-" + visibleSource)
	app.WaitForGone("id:danmaku-canvas", 3*time.Second)
	app.Press("id:danmaku-toggle-" + visibleSource)
	app.WaitFor("id:danmaku-canvas", 3*time.Second)
	app.Press("id:danmaku-tab-filters")
	app.Press("id:danmaku-edit-filters")
	app.Type("id:danmaku-keywords", "剧透")
	app.Type("id:danmaku-patterns", `^\d+$`)
	app.Press("id:danmaku-save-filters")
	app.WaitFor("label:移除 剧透", time.Second)
	capture("player-danmaku-panel-block")
	app.Press("label:移除 剧透")
	if stored := readDanmakuPreferences(app.Storage()); stored.Keywords != "" || stored.Patterns != `^\d+$` {
		t.Fatalf("saved filters = %+v", stored)
	}
	app.Press("id:close-danmaku-sources")
	app.WaitForGone("id:danmaku-sources-dialog", time.Second)
	app.Press("id:phone-player-info")
	app.WaitFor("id:phone-player-info-panel", time.Second)
	capture("player-more")
	app.Press("id:phone-player-action-info")
	app.WaitFor("text:播放信息", time.Second)
	app.Press("id:phone-player-more-back")
	app.Press("id:phone-player-action-chapters")
	app.WaitFor("id:phone-player-chapter-2", time.Second)
	capture("player-chapters")
	app.Press("id:phone-player-chapter-2")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:13" })
	choosePhonePlayerAction(app, "chapters")
	app.Press("id:phone-player-chapter-1")
	choosePhonePlayerAction(app, "loop")
	app.Press("id:phone-player-loop-start")
	if app.Enabled("id:phone-player-loop-end") {
		t.Fatal("a segment can end at its own start")
	}
	app.Press("id:phone-player-more-back")
	app.Press("id:phone-player-action-chapters")
	app.Press("id:phone-player-chapter-2")
	choosePhonePlayerAction(app, "loop")
	app.Press("id:phone-player-loop-end")
	app.WaitFor("id:phone-player-loop-range", time.Second)
	capture("player-loop")
	app.Press("id:phone-player-info-close")
	app.WaitFor("id:phone-player-loop-ticks", time.Second)
	app.Press("id:toggle-play")
	if localFixture {
		app.Progress("id:video-player", 14*time.Second, 24*time.Second)
		seeks := app.FindMounted("id:video-player").Seeks
		if seeks[len(seeks)-1] != 4320*time.Millisecond {
			t.Fatalf("A–B boundary did not seek to A: %v", seeks)
		}
		app.Progress("id:video-player", 14*time.Second, 24*time.Second)
		if len(app.FindMounted("id:video-player").Seeks) != len(seeks) {
			t.Fatal("stale progress started a duplicate loop seek")
		}
		app.Progress("id:video-player", 24*time.Second, 24*time.Second)
		if app.FindMounted("id:video-player").Video.Paused {
			t.Fatal("the decoder's end event stopped an active segment")
		}
	}
	app.WaitUntil(2*time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:04" })
	if !localFixture {
		app.WaitUntil(3*time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:05" })
	}
	app.Press("id:toggle-play")
	choosePhonePlayerAction(app, "sleep")
	app.Press("id:phone-player-sleep-30")
	choosePhonePlayerAction(app, "sleep")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:phone-player-sleep-30").Props.Checked })
	capture("player-sleep")
	if localFixture {
		app.Press("id:phone-player-sleep-0")
	} else {
		app.Press("id:phone-player-info-close")
		app.Press("id:toggle-play")
		app.WaitUntil(time.Second, func() bool { return app.Find("id:toggle-play").Props.Label == "暂停" })
		app.FastForward(30 * time.Minute)
		app.WaitUntil(time.Second, func() bool { return app.Find("id:toggle-play").Props.Label == "播放" })
		choosePhonePlayerAction(app, "sleep")
		if !app.Find("id:phone-player-sleep-0").Props.Checked {
			t.Fatal("the expired timer was still selected")
		}
		app.Press("id:phone-player-info-close")
	}
	choosePhonePlayerAction(app, "chapters")
	app.Press("id:phone-player-chapter-3")
	app.WaitForGone("id:phone-player-loop-ticks", time.Second)
	app.Press("id:player-seek-forward")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:24" })
	if localFixture {
		choosePhonePlayerAction(app, "chapters")
		app.Press("id:phone-player-chapter-1")
		choosePhonePlayerAction(app, "loop")
		app.Press("id:phone-player-loop-start")
		app.Press("id:phone-player-info-close")
		app.Progress("id:video-player", 24*time.Second, 24*time.Second)
		choosePhonePlayerAction(app, "loop")
		app.Press("id:phone-player-loop-end")
		app.Press("id:phone-player-info-close")
		app.Press("id:toggle-play")
		app.WaitUntil(time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:04" })
		app.WaitFor("id:phone-player-loop-ticks", time.Second)
	}
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", time.Second)
}

func choosePhonePlayerAction(app *nsgotest.App, action string) {
	app.Press("id:phone-player-info")
	app.WaitFor("id:phone-player-info-panel", time.Second)
	app.Press("id:phone-player-action-" + action)
}

func TestDockerPhonePlayerGesturesAndPreview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media servers")
	}
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("held pointer capture uses the Linux test host")
	}
	config, secrets := homeReviewSources(t)
	_, movie := reviewFixtureItem(t, config, secrets, "jellyfin", "Waygu Track Test")
	initialHeight := core.Points(800)
	if target == "linux" {
		initialHeight += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, initialHeight), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:tab-search")
	app.Type("id:search-query", "Waygu Track Test")
	app.Press("id:search-source-jellyfin")
	app.WaitFor("id:title-jellyfin-"+movie.ID, 10*time.Second)
	app.Press("id:title-jellyfin-" + movie.ID)
	app.WaitFor("id:detail-play", 10*time.Second)
	app.Press("id:detail-play")
	app.Resize(844, 390)
	const surface = "id:phone-player-surface"
	const rail = "id:phone-player-position-rail"
	const video = "id:video-player"
	app.WaitFor(surface, 10*time.Second)
	app.WaitFor(video, 10*time.Second)
	if target == "" {
		app.Report(nsgotest.Event{Kind: nsgotest.EventLoad, ID: app.FindMounted(video).ID,
			Duration: 24 * time.Second, Size: core.Size{Width: 320, Height: 180}, RateChangeable: true})
	}
	app.WaitFor("id:phone-player-chapter-ticks", 6*time.Second)
	if target == "linux" {
		app.WaitUntil(10*time.Second, func() bool { return app.Find("id:player-position").Text != "00:00:00" })
	}
	app.Press("id:toggle-play")

	left, right := core.Point{X: 190, Y: 110}, core.Point{X: 660, Y: 110}
	down := core.Point{Y: 108}
	app.DragAt(surface, left, down)
	app.WaitFor("id:phone-player-brightness", time.Second)
	app.WaitFor("id:phone-player-dimming", time.Second)
	app.DragAt(surface, right, down)
	app.WaitFor("id:phone-player-volume", time.Second)
	if target == "" {
		if volume := app.FindMounted(video).Video.Volume; volume < .71 || volume > .74 {
			t.Fatalf("right drag video volume = %v", volume)
		}
	}

	choosePhonePlayerAction(app, "chapters")
	app.Press("id:phone-player-chapter-1")
	app.WaitUntil(time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:04" })
	app.DragAt(surface, left, core.Point{X: 24})
	app.WaitUntil(time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:10" })

	timeline := app.Find(rail).Frame.Size.Width
	start, middle := core.Point{X: timeline / 2, Y: 8}, core.Point{X: timeline * .63, Y: 8}
	nativePointer := target == "linux" && os.Getenv("WAYGU_REVIEW_X11_POINTER") == "1"
	switch {
	case target == "":
		finger := app.Touch(rail, start)
		finger.Move(middle)
		app.WaitFor("id:phone-player-seek-thumbnail", 6*time.Second)
		app.WaitFor("id:phone-player-seek-time", time.Second)
		finger.Lift()
	case nativePointer:
		phoneReviewPointer(t, app.Find(rail), start, middle, func() {
			app.WaitFor("id:phone-player-seek-thumbnail", 6*time.Second)
			app.WaitFor("id:phone-player-seek-time", time.Second)
			app.Screenshot("player-seek-thumbnail")
		})
	default:
		app.DragAt(rail, start, core.Point{X: middle.X - start.X})
	}
	app.WaitForGone("id:phone-player-seek-thumbnail", time.Second)
	app.WaitUntil(time.Second, func() bool { return app.Find("id:player-position").Text == "00:00:15" })
	if target == "" {
		if seeks := app.FindMounted(video).Seeks; !slices.Contains(seeks, 4320*time.Millisecond) || !slices.Contains(seeks, 15*time.Second) {
			t.Fatalf("decoder did not receive chapter and timeline seeks: %v", seeks)
		}
		finger := app.Touch(rail, start)
		finger.Move(core.Point{X: timeline * .8, Y: 8})
		app.WaitFor("id:phone-player-seek-time", time.Second)
		finger.Cancel()
		app.WaitForGone("id:phone-player-seek-time", time.Second)
		if app.Find("id:player-position").Text != "00:00:15" {
			t.Fatal("cancelled preview moved playback")
		}
	}

	app.Press("id:phone-player-lock")
	app.DragAt(surface, right, down)
	app.WaitForGone("id:phone-player-volume", time.Second)
	app.Press("id:phone-player-unlock")
	app.PressAt(surface, left)
	app.WaitForGone("id:toggle-play", time.Second)
	app.PressAt(surface, left)
	app.WaitFor("id:toggle-play", time.Second)
	if target == "" && app.FindMounted(video).Video.Paused {
		t.Fatal("double tap did not resume playback")
	}
	if rateAvailable := app.Enabled("id:phone-player-rate"); rateAvailable {
		app.Press("id:phone-player-rate")
		app.WaitFor("id:phone-player-rate >> text:1.25x", time.Second)
		if target == "" {
			finger := app.Touch(surface, left)
			app.Advance(550 * time.Millisecond)
			app.WaitFor("id:phone-player-boost", time.Second)
			if rate := app.FindMounted(video).Video.Rate; rate != 2 {
				t.Fatalf("held playback rate = %v", rate)
			}
			finger.Cancel()
		} else if nativePointer {
			phoneReviewPointer(t, app.Find(surface), left, left, func() {
				app.Advance(550 * time.Millisecond)
				app.WaitFor("id:phone-player-boost", time.Second)
				app.Screenshot("player-hold-speed")
			})
		} else {
			app.PressAt(surface, left)
			app.WaitForGone("id:toggle-play", time.Second)
			app.LongPress(surface)
			app.PressAt(surface, left)
		}
		app.WaitForGone("id:phone-player-boost", time.Second)
		app.WaitFor("id:phone-player-rate >> text:1.25x", time.Second)
		if target == "" {
			if rate := app.FindMounted(video).Video.Rate; rate != 1.25 {
				t.Fatalf("restored playback rate = %v", rate)
			}
		}
	} else if target == "" {
		t.Fatal("fixture reported rate control but the action is disabled")
	}
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", time.Second)
}

// The GTK test host sends a complete drag at once. X11 input lets the
// capture observe the running player while its pointer is still down.
func phoneReviewPointer(t *testing.T, view *nsgotest.View, from, to core.Point, during func()) {
	t.Helper()
	xdotool := func(args ...string) string {
		t.Helper()
		output, err := exec.CommandContext(t.Context(), "xdotool", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("xdotool %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	windows := strings.Fields(xdotool("search", "--onlyvisible", "--pid", strconv.Itoa(os.Getpid())))
	if len(windows) == 0 {
		t.Fatal("the GTK test window is not visible on X11")
	}
	window := windows[len(windows)-1]
	for node := view; node != nil; node = node.Parent {
		from.X += node.Frame.Origin.X
		from.Y += node.Frame.Origin.Y
		to.X += node.Frame.Origin.X
		to.Y += node.Frame.Origin.Y
	}
	move := func(point core.Point) {
		xdotool("mousemove", "--window", window, strconv.Itoa(int(point.X)), strconv.Itoa(int(point.Y)))
	}
	xdotool("windowfocus", "--sync", window)
	move(from)
	xdotool("mousedown", "1")
	defer xdotool("mouseup", "1")
	if to != from {
		move(to)
	}
	during()
}
