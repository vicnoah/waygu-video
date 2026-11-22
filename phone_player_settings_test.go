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

func TestPhonePlaybackAndSubtitleSettingsReview(t *testing.T) {
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(1780)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(variant.appearance)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(map[string]string{seenKey: "true", playbackPreferencesKey: `{"Rate":1}`}))
			capture := func(name string) {
				if nsgotest.Target() != "" {
					app.Advance(450 * time.Millisecond)
					app.Screenshot(name + "-" + variant.name)
				}
			}
			app.Press("id:tab-settings")
			app.Press("id:open-phone-playback-settings")
			app.WaitFor("id:phone-playback-settings", time.Second)
			app.WaitFor("id:phone-streaming-quality >> text:原画", time.Second)
			app.WaitFor("id:phone-cellular-quality >> text:4 Mbps", time.Second)
			capture("settings-playback")
			if !app.Find("id:phone-prefer-direct").Props.Checked {
				t.Fatal("legacy settings should prefer direct play")
			}
			app.Press("id:phone-prefer-direct")
			if !readPlaybackPreferences(app.Storage()).DisableDirectPlay {
				t.Fatal("turning off direct play was not saved")
			}
			app.Press("id:phone-cellular-quality")
			app.WaitFor("role:option 8 Mbps", time.Second)
			app.Press("role:option 8 Mbps")
			app.Press("id:phone-cellular-warning")
			if p := readPlaybackPreferences(app.Storage()); p.CellularBitrate != 8_000_000 || !p.WarnOnCellular {
				t.Fatalf("saved mobile playback settings = %+v", p)
			}
			app.WaitFor("id:phone-streaming-quality >> text:原画", time.Second)
			app.Press("id:phone-cellular-quality")
			app.WaitFor("role:option 原画", time.Second)
			app.Press("role:option 原画")
			if readPlaybackPreferences(app.Storage()).CellularBitrate != 0 {
				t.Fatal("saving original quality on cellular should preserve the explicit zero limit")
			}
			app.Press("id:phone-auto-next")
			if stored, _ := app.Storage().GetItem(autoNextKey); stored != "false" {
				t.Fatalf("auto-next preference = %q", stored)
			}
			if !app.Find("id:phone-skip-intro").Props.Checked {
				t.Fatal("automatic marker skipping should start enabled")
			}
			app.Press("id:phone-skip-intro")
			if !readPlaybackPreferences(app.Storage()).KeepIntroCredits {
				t.Fatal("turning off marker skipping was not saved")
			}
			app.Press("id:phone-default-rate")
			app.WaitFor("role:option 1.5x", time.Second)
			app.Press("role:option 1.5x")
			app.Press("id:phone-seek-forward")
			app.Press("role:option 15 秒")
			if p := readPlaybackPreferences(app.Storage()); p.Rate != 1.5 || p.NextSeconds != 15 || p.BackSeconds != 10 {
				t.Fatalf("saved playback controls = %+v", p)
			}
			app.Press("id:phone-prefer-direct")
			if readPlaybackPreferences(app.Storage()).DisableDirectPlay {
				t.Fatal("turning direct play back on was not saved")
			}
			app.Press("id:back")
			app.Press("id:open-phone-subtitle-settings")
			app.WaitFor("id:phone-subtitle-settings", time.Second)
			capture("settings-subtitles")
			app.Press("id:subtitle-color-2")
			if p := readSubtitlePreferences(app.Storage()); p.Color != subtitleColors[2] {
				t.Fatalf("saved subtitle style = %+v", p)
			}
			app.Press("id:subtitle-custom-color")
			app.WaitFor("id:subtitle-custom-color-dialog", time.Second)
			app.Clear("label:十六进制颜色")
			app.Type("label:十六进制颜色", "2277BB")
			app.Press("id:subtitle-custom-color-done")
			if p := readSubtitlePreferences(app.Storage()); p.Color != 0x2277bb {
				t.Fatalf("saved custom subtitle color = %#x", p.Color)
			}
			app.Press("id:subtitle-language-order")
			app.Press("id:subtitle-language-up-en")
			app.Press("id:subtitle-language-remove-zh-Hant")
			app.Press("id:subtitle-language-add-ja")
			if p := readSubtitlePreferences(app.Storage()); !slices.Equal(p.Languages, []string{"zh-Hans", "en", "ja"}) {
				t.Fatalf("subtitle order = %v", p.Languages)
			}
			capture("settings-subtitle-language-order")
			app.Press("id:back")
			app.WaitFor("id:subtitle-language-order", time.Second)
		})
	}
}

func TestDockerPhonePlayerUsesSettings(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || nsgotest.Target() != "" {
		t.Skip("requires the Docker fixtures and a simulated playback surface")
	}
	for _, provider := range []string{"emby", "jellyfin"} {
		t.Run(provider, func(t *testing.T) { verifyDockerPhonePlayerSettings(t, provider) })
	}
}

func verifyDockerPhonePlayerSettings(t *testing.T, provider string) {
	t.Helper()
	config, secrets := homeReviewSources(t)
	client, item := reviewFixtureItem(t, config, secrets, provider, "Waygu Track Test")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tracks, err := client.Playback(ctx, item, media.PlaybackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	expectedTrack := ""
	for _, track := range tracks.Tracks {
		if track.Type == "audio" {
			t.Logf("fixture track %s language=%s title=%s default=%v selected=%v", track.ID, track.Language, track.Title, track.Default, track.Selected)
			if subtitleLanguage(track.Language) == "ja" {
				expectedTrack = track.ID
			}
		}
	}
	if expectedTrack == "" {
		t.Fatal("fixture is missing Japanese audio")
	}
	movie := item.ID
	config[playbackPreferencesKey] = `{"BackSeconds":5,"NextSeconds":15,"Rate":1.5,"Picture":"crop","KeepIntroCredits":true}`
	config[subtitlePreferencesKey] = `{"Languages":["zh-Hant","en"],"Audio":"ja","Mode":"always","Color":16638023,"Scale":125,"Position":12,"Background":"solid"}`
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return Root(cx)
	}, nsgotest.Size(390, 800), nsgotest.Offscreen(), nsgotest.Storage(config), nsgotest.Secrets(secrets))
	app.Press("id:tab-search")
	app.Type("id:search-query", "Waygu Track Test")
	app.Press("id:search-source-" + provider)
	app.WaitFor("id:title-"+provider+"-"+movie, 10*time.Second)
	app.Press("id:title-" + provider + "-" + movie)
	app.WaitFor("id:detail-play", 10*time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", 15*time.Second)
	video := app.FindMounted("id:video-player").Video
	if video.Rate != 1.5 || video.ResizeMode != core.Cover {
		t.Fatalf("initial player config = %gx, %v", video.Rate, video.ResizeMode)
	}
	app.Play("id:video-player", core.VideoInfo{Duration: 24 * time.Second, RateChangeable: true})
	app.Progress("id:video-player", 2*time.Second, 24*time.Second)
	caption := app.WaitFor("text:Waygu track selection fixture", 10*time.Second)
	if caption.Props.TestID != "subtitle-caption" || caption.TextStyle.Font.Size != 25 {
		t.Fatalf("rendered subtitle = %s, size %v", caption.Props.TestID, caption.TextStyle.Font.Size)
	}
	app.Press("id:player-seek-forward")
	app.Press("id:player-seek-back")
	seeks := app.FindMounted("id:video-player").Seeks
	if len(seeks) < 2 || !slices.Equal(seeks[len(seeks)-2:], []time.Duration{17 * time.Second, 12 * time.Second}) {
		t.Fatalf("configured seek requests = %v", seeks)
	}
	app.Press("id:player-tracks")
	app.Press("id:track-tab-audio")
	node := app.WaitFor("id:audio-track-"+expectedTrack, time.Second)
	if !node.Props.Checked {
		t.Fatalf("the preferred Japanese audio track %s was not selected", expectedTrack)
	}
}

func TestAutomaticSubtitleUsesOrderedLanguagesAndAudioPolicy(t *testing.T) {
	prefs := subtitlePreferences{Languages: []string{"zh-Hant", "zh-Hans", "en"}, Mode: "foreign", Audio: "ja"}
	tracks := []media.Track{
		{ID: "1", Type: "audio", Language: "eng", Selected: true},
		{ID: "2", Type: "audio", Language: "jpn"},
		{ID: "3", Type: "subtitle", Language: "en", URL: "/en"},
		{ID: "4", Type: "subtitle", Language: "chs", URL: "/chs"},
		{ID: "5", Type: "subtitle", Language: "zh-TW", URL: "/cht"},
	}
	if match := automaticSubtitle(tracks, prefs); match.ID != "5" {
		t.Fatalf("ordered selection = %+v", match)
	}
	if index := preferredAudio(tracks, prefs.Audio); index == nil || *index != 2 {
		t.Fatalf("preferred audio index = %v", index)
	}
	tracks[0].Selected, tracks[1].Selected = false, true
	if match := automaticSubtitle(tracks, prefs); match.ID != "" {
		t.Fatalf("foreign audio subtitles selected for matching audio: %+v", match)
	}
	if index := preferredAudio(tracks, prefs.Audio); index != nil {
		t.Fatalf("duplicated active audio selection: %v", index)
	}
	prefs.Mode = "off"
	if match := automaticSubtitle(tracks, prefs); match.ID != "" {
		t.Fatalf("subtitles selected while disabled: %+v", match)
	}
}
