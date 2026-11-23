package main

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerNativeEmbeddedSubtitles(t *testing.T) {
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			testDockerNativeSubtitlePlayback(t, provider, "Waygu Portable Captions Test", media.DirectPlay, "phone-embedded")
		})
	}
}

func TestDockerNativeMatroskaSubtitles(t *testing.T) {
	testDockerNativeSubtitlePlayback(t, "plex", "Waygu Embedded Subtitle Test", media.Transcode, "phone-matroska")
}

func testDockerNativeSubtitlePlayback(t *testing.T, provider, name string, method media.PlayMethod, screenshotPrefix string) {
	t.Helper()
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux" && target != "android") {
		t.Skip("requires the local Docker fixture servers")
	}
	config, secrets := dockerReviewSources(t, false)
	client, movie := reviewFixtureItem(t, config, secrets, provider, name)
	off := -1
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	stream, err := client.Playback(ctx, movie, media.PlaybackOptions{SubtitleIndex: &off})
	cancel()
	if err != nil || stream.Method != method {
		t.Fatalf("%s playback = %s, %v; want %s", name, stream.Method, err, method)
	}
	trackID := func(language string) string {
		i := slices.IndexFunc(stream.Tracks, func(track media.Track) bool {
			return track.ReadableSubtitle() && !track.External && subtitleLanguage(track.Language) == language
		})
		if i < 0 {
			t.Fatalf("missing embedded %s subtitle in %s metadata", language, provider)
		}
		return "id:subtitle-track-" + stream.Tracks[i].ID
	}
	chineseTrack, englishTrack := trackID("zh-Hans"), trackID("en")
	config[subtitlePreferencesKey] = `{"Languages":["zh-Hant","en"],"Mode":"always"}`
	config[playbackPreferencesKey] = `{"DisableProgressRecording":true}`
	window := nsgotest.Size(844, 390)
	if target == "linux" {
		window = nsgotest.Size(390, 837)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, window, nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Offscreen(), nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:tab-search")
	app.Type("id:search-query", name)
	app.Press("id:search-source-" + provider)
	title := "id:title-" + provider + "-" + movie.ID
	app.WaitFor(title, 10*time.Second)
	app.Press(title)
	app.WaitFor("id:detail-play", 10*time.Second)
	app.Press("id:detail-play")
	if target == "linux" {
		app.Resize(844, 427)
	}
	app.WaitFor("id:video-player", 15*time.Second)
	const play = "id:phone-player-play-small"
	revealControls := func() {
		if !app.Exists(play) {
			app.Press("id:phone-player-surface")
		}
		app.WaitFor(play, 3*time.Second)
	}
	revealControls()
	if target == "" {
		app.Play("id:video-player", core.VideoInfo{Duration: 18 * time.Second})
		app.Progress("id:video-player", 3*time.Second, 18*time.Second)
		app.Press(play)
	} else {
		app.WaitFor("id:phone-player-resolution", 10*time.Second)
		revealControls()
		if app.Find(play).Props.Label == "暂停" {
			app.Press(play)
		}
		const rail = "id:phone-player-position-rail"
		width := app.Find(rail).Frame.Size.Width
		app.PressAt(rail, core.Point{X: width / 6, Y: 8})
	}
	const chinese, english = "理髮以後再出發。", "The train leaves at dawn."
	assertCaption := func(text, id string) {
		t.Helper()
		app.WaitUntil(7*time.Second, func() bool {
			nodes := app.Query("id:" + id)
			return len(nodes) == 1 && nodes[0].Text == text
		})
	}
	assertCaption(chinese, "subtitle-caption")
	assertCaption(english, "subtitle-caption-secondary")
	revealControls()
	app.WaitUntil(2*time.Second, func() bool { return app.Find(play).Props.Label == "播放" })
	position := app.Find("id:player-position").Text
	if target != "" {
		app.Screenshot(screenshotPrefix + "-subtitles-" + provider)
	}
	app.Press("id:player-tracks")
	app.WaitUntil(2*time.Second, func() bool { return app.Find(chineseTrack).Props.Checked })
	app.Press(englishTrack)
	assertCaption(english, "subtitle-caption")
	assertCaption(chinese, "subtitle-caption-secondary")
	if target != "" {
		app.Screenshot(screenshotPrefix + "-subtitle-picker-" + provider)
	}
	app.Press("id:subtitles-off")
	app.WaitForGone("id:subtitle-caption", 2*time.Second)
	app.WaitForGone("id:subtitle-caption-secondary", 2*time.Second)
	app.Press("id:close-player-tracks")
	revealControls()
	if app.Find("id:player-position").Text != position || app.Find(play).Props.Label != "播放" {
		t.Fatal("changing embedded subtitles changed the paused playhead")
	}
}
