package main

import (
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
	"waygu-video/internal/playback/mpv"
)

func TestDecoderTracksAndLocalCaptionsAreExclusive(t *testing.T) {
	type selection struct {
		kind mpv.TrackKind
		id   int64
	}
	var changes []selection
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		tracks := core.NewSignalFunc(cx, []mpv.Track{
			{ID: 1, Kind: mpv.AudioTrack, Language: "eng", Selected: true},
			{ID: 2, Kind: mpv.AudioTrack, Language: "jpn"},
			{ID: 1, Kind: mpv.SubtitleTrack, Codec: "mov_text", Language: "eng"},
		}, slices.Equal)
		decoder := &desktopVideo{tracks: tracks, subtitleDelay: func(time.Duration) {}}
		decoder.selectTrack = func(kind mpv.TrackKind, id int64, done func(error)) {
			changes = append(changes, selection{kind, id})
			next := slices.Clone(tracks.GetUntracked())
			for i := range next {
				if next[i].Kind == kind {
					next[i].Selected = next[i].ID == id
				}
			}
			tracks.Set(next)
			done(nil)
		}
		ui := playerTracks(cx, func() media.Stream {
			return media.Stream{Tracks: []media.Track{{ID: "99", Type: "audio"}, {ID: "77", Type: "subtitle", URL: "https://example.test/embedded.vtt"}}}
		}, func() *desktopVideo { return decoder }, nil, "", func() subtitlePreferences { return readSubtitlePreferences(cx.Storage()) }, core.NewSignal(cx, 2*time.Second),
			core.NewSignal(cx, float32(1)), core.NewSignal(cx, true), core.NewSignal(cx, false), core.NewSignal(cx, false),
			func(media.Track) { t.Error("decoder audio selection went to the server") }, nil, nil, nil)
		cx.OnKey("a", ui.cycleAudio)
		cx.OnKey("s", ui.cycleSubtitle)
		return core.View{Class: "flex-1", Children: []core.Node{ui.button, ui.dialog, ui.captions}}
	}, nsgotest.Size(1000, 680))
	app.PickFiles = func(nsgotest.FilePick) []core.File {
		return []core.File{{Name: "fixture.srt", Opener: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("1\n00:00:01,000 --> 00:00:04,000\nLocal subtitle fixture\n")), nil
		}}}
	}

	app.Press("id:player-tracks")
	app.WaitForGone("id:subtitle-track-77", time.Second)
	app.Press("id:decoder-subtitle-1")
	app.Press("id:open-subtitle-file")
	app.WaitFor("text:Local subtitle fixture", time.Second)
	app.Press("id:decoder-subtitle-1")
	app.WaitForGone("id:subtitle-caption", time.Second)
	app.Press("id:subtitles-off")
	app.Press("id:track-tab-audio")
	app.WaitForGone("id:audio-track-99", time.Second)
	app.Press("id:audio-track-2")
	app.Press("id:close-player-tracks")
	app.Key("a")
	app.Key("s")

	want := []selection{{mpv.SubtitleTrack, 1}, {mpv.SubtitleTrack, 0}, {mpv.SubtitleTrack, 1},
		{mpv.SubtitleTrack, 0}, {mpv.AudioTrack, 2}, {mpv.AudioTrack, 1}, {mpv.SubtitleTrack, 1}}
	if !slices.Equal(changes, want) {
		t.Fatalf("decoder requests = %v, want %v", changes, want)
	}
}
