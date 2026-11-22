//go:build linux && !android

package mpv

import (
	"bytes"
	"errors"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/gomono"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/mediahttp"
	"waygu-video/internal/playback"
)

func TestMergeCacheRangesKeepsGaps(t *testing.T) {
	ranges := []CacheRange{
		{Start: 9 * time.Second, End: 12 * time.Second},
		{Start: 2 * time.Second, End: 5 * time.Second},
		{Start: 0, End: 3 * time.Second},
	}
	want := []CacheRange{{Start: 0, End: 5 * time.Second}, {Start: 9 * time.Second, End: 12 * time.Second}}
	if got := mergeCacheRanges(ranges); !slices.Equal(got, want) {
		t.Fatalf("merged cache ranges = %v, want %v", got, want)
	}
}

func TestRenderWithASS(t *testing.T) {
	if os.Getenv("WAYGU_TEST_MPV") != "1" {
		t.Skip("generate the Docker video fixtures and set WAYGU_TEST_MPV=1")
	}
	// Use a solid-color video so every decoded frame has the same background.
	fixture, err := filepath.Abs("../../../integration/servers/media/Movies/Waygu Resume Across Sources (2025)/Waygu Resume Across Sources (2025).mp4")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Open()
	if errors.Is(err, ErrUnavailable) {
		t.Fatal("libmpv is required for this test")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.property("ao", "null"); err != nil {
		t.Fatal(err)
	}
	if err := p.Pause(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Load(fixture); err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 640, 360))
	loaded := false
	until(t, func() bool {
		event := p.Poll()
		if event.Failed {
			t.Fatal("libmpv could not load the fixture")
		}
		loaded = loaded || event.Loaded
		drawn, err := p.Frame(frame, false)
		if err != nil {
			t.Fatal(err)
		}
		return loaded && drawn && p.Status().Width == 160 && frame.RGBAAt(20, 20).R > 0
	})
	if err := p.Volume(.35); err != nil {
		t.Fatal(err)
	}
	if got := p.doubleProperty("volume"); got < 34.9 || got > 35.1 {
		t.Fatalf("libmpv volume = %v, want 35", got)
	}
	if stats := p.Stats(); stats.VideoCodec == "" || stats.FPS <= 0 {
		t.Fatalf("libmpv did not report the fixture's codec and frame rate: %+v", stats)
	}
	if err := p.Seek(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool {
		drawn, err := p.Frame(frame, false)
		if err != nil {
			t.Fatal(err)
		}
		return drawn && p.Status().Position >= 2*time.Second
	})
	baseline := bytes.Clone(frame.Pix)
	comments, err := danmaku.ReadASS(strings.NewReader(`[Events]
Dialogue: 0,0:00:01.00,0:00:05.00,Default,,0,0,0,,{\an8}Waygu fixture comments
`))
	if err != nil || len(comments) != 1 {
		t.Fatalf("ASS fixture import = %v, %v", comments, err)
	}
	settings := danmaku.DefaultSettings()
	track := danmaku.Schedule(comments, settings)
	p.Danmaku(&track)
	until(t, func() bool {
		if _, err := p.Frame(frame, true); err != nil {
			t.Fatal(err)
		}
		row := 55 * frame.Stride
		return !bytes.Equal(baseline[:row], frame.Pix[:row]) && bytes.Equal(baseline[row:], frame.Pix[row:])
	})
	fullOpacity := bytes.Clone(frame.Pix)
	settings.Opacity, settings.FontSize, settings.Outline = .35, 36, 3
	changed := danmaku.Schedule(comments, settings)
	p.Danmaku(&changed)
	until(t, func() bool {
		if _, err := p.Frame(frame, true); err != nil {
			t.Fatal(err)
		}
		return !bytes.Equal(baseline, frame.Pix) && !bytes.Equal(fullOpacity, frame.Pix)
	})
	settings.HideTop = true
	filtered := danmaku.Schedule(comments, settings)
	p.Danmaku(&filtered)
	until(t, func() bool {
		if _, err := p.Frame(frame, true); err != nil {
			t.Fatal(err)
		}
		return bytes.Equal(baseline, frame.Pix)
	})
}

func TestSwitchEmbeddedTracksDuringAuthenticatedPlayback(t *testing.T) {
	if os.Getenv("WAYGU_TEST_MPV") != "1" {
		t.Skip("generate the Docker video fixtures and set WAYGU_TEST_MPV=1")
	}
	const fixture = "../../../integration/servers/media/Movies/Waygu Track Test (2025)/Waygu Track Test (2025).mp4"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.ServeFile(w, r, fixture)
	}))
	defer upstream.Close()
	address, closeProxy, err := playback.Open(mediahttp.Resource{URL: upstream.URL + "/movie", Headers: http.Header{"Authorization": {"Bearer fixture"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	p, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.property("ao", "null"); err != nil {
		t.Fatal(err)
	}
	if err := p.Pause(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Load(address); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool {
		event := p.Poll()
		if event.Failed {
			t.Fatal("libmpv could not open the authenticated stream")
		}
		return event.Loaded
	})
	until(t, func() bool {
		for _, cached := range p.Stats().CacheRanges {
			if cached.Start == 0 && cached.End > 8*time.Second {
				return true
			}
		}
		return false
	})
	var original, alternate, caption Track
	for _, track := range p.Tracks() {
		switch {
		case track.Kind == AudioTrack && track.Language == "eng":
			original = track
		case track.Kind == AudioTrack && track.Language == "jpn":
			alternate = track
		case track.Kind == SubtitleTrack && track.Language == "eng":
			caption = track
		}
	}
	if !original.Selected || alternate.ID == 0 || alternate.Selected || caption.ID == 0 || caption.Selected {
		t.Fatalf("unexpected track list: %+v", p.Tracks())
	}
	// Position the video before the first subtitle packet. Some containers
	// don't supply a cue that began before an already-paused frame.
	const target = 500 * time.Millisecond
	if err := p.Seek(target); err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 320, 180))
	until(t, func() bool {
		drawn, err := p.Frame(frame, false)
		if err != nil {
			t.Fatal(err)
		}
		return drawn && p.Status().Position == target
	})
	before := p.Status().Position
	if err := p.SelectTrack(AudioTrack, alternate.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.SelectTrack(SubtitleTrack, caption.ID); err != nil {
		t.Fatal(err)
	}
	for _, track := range p.Tracks() {
		if track.Selected != (track.Kind == AudioTrack && track.ID == alternate.ID ||
			track.Kind == SubtitleTrack && track.ID == caption.ID) {
			t.Fatalf("unexpected track selection: %+v", track)
		}
	}
	after := p.Status()
	if !after.Paused || after.Position != before {
		t.Fatalf("track switch changed paused playback from %v to %v", before, after)
	}
	if err := p.Pause(false); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool {
		if _, err := p.Frame(frame, false); err != nil {
			t.Fatal(err)
		}
		return p.stringProperty("sub-text") == "Waygu track selection fixture"
	})
	if err := p.Pause(true); err != nil {
		t.Fatal(err)
	}
	comments := danmaku.Schedule([]danmaku.Comment{
		{At: 500 * time.Millisecond, Mode: danmaku.Top, Color: danmaku.White, Text: "Independent comments"},
	}, danmaku.DefaultSettings())
	p.Danmaku(&comments)
	if _, err := p.Frame(frame, true); err != nil {
		t.Fatal(err)
	}
	withCaption := bytes.Clone(frame.Pix)
	if err := p.SubtitleDelay(2 * time.Second); err != nil || p.timeProperty("sub-delay") != 2*time.Second {
		t.Fatalf("subtitle delay was not applied: %v", err)
	}
	if err := p.SelectTrack(SubtitleTrack, 0); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool {
		if _, err := p.Frame(frame, true); err != nil {
			t.Fatal(err)
		}
		split := 80 * frame.Stride
		return bytes.Equal(withCaption[:split], frame.Pix[:split]) && !bytes.Equal(withCaption[split:], frame.Pix[split:])
	})
}

func TestTwoEmbeddedCaptionsAndAttachedFonts(t *testing.T) {
	if os.Getenv("WAYGU_TEST_MPV") != "1" {
		t.Skip("generate the Docker video fixtures and set WAYGU_TEST_MPV=1")
	}
	const fixture = "../../../integration/servers/media/Movies/Waygu Embedded Subtitle Test (2025)/Waygu Embedded Subtitle Test (2025).mkv"
	output := t.TempDir()
	font := filepath.Join(output, "Go-Mono.ttf")
	if err := os.WriteFile(font, gomono.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	movie := filepath.Join(output, "attached-font.mkv")
	if log, err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-loglevel", "error", "-i", fixture, "-map", "0",
		"-c", "copy", "-attach", font, "-metadata:s:t:0", "mimetype=application/x-truetype-font", movie).CombinedOutput(); err != nil {
		t.Fatalf("attach the fixture font: %v\n%s", err, log)
	}
	p, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(p.property("ao", "null"))
	must(p.Load(movie))
	until(t, func() bool { return p.Poll().Loaded })
	must(p.SelectTrack(SubtitleTrack, 1))
	must(p.ShowNativeSubtitle(false))
	must(p.SecondarySubtitle(2))
	frame := image.NewRGBA(image.Rect(0, 0, 640, 360))
	until(t, func() bool {
		_, err := p.Frame(frame, false)
		must(err)
		return p.SubtitleText(false) == "理发以后再出发。" && p.SubtitleText(true) == "The train leaves at dawn."
	})
	for _, track := range p.Tracks() {
		if track.Kind == SubtitleTrack && track.Selected != (track.ID == 1) {
			t.Fatalf("the companion track was exposed as the primary selection: %+v", track)
		}
	}
	baseline := bytes.Clone(frame.Pix)
	must(p.SubtitleDelay(8 * time.Second))
	until(t, func() bool { return p.SubtitleText(false) == "" && p.SubtitleText(true) == "" })
	must(p.SubtitleDelay(0))
	must(p.SelectTrack(SubtitleTrack, 2))
	until(t, func() bool { return p.SubtitleText(false) == "The train leaves at dawn." })
	if p.intProperty("secondary-sid") == 2 {
		t.Fatal("promoting the companion left it selected twice")
	}
	must(p.SelectTrack(SubtitleTrack, 3))
	must(p.ShowNativeSubtitle(true))
	until(t, func() bool {
		_, err := p.Frame(frame, true)
		must(err)
		return p.SubtitleText(false) == "Rótulo ASS de prueba" && !bytes.Equal(baseline, frame.Pix)
	})
	attached := bytes.Clone(frame.Pix)
	must(p.EmbeddedSubtitleFonts(false))
	until(t, func() bool {
		_, err := p.Frame(frame, true)
		must(err)
		return !bytes.Equal(baseline, frame.Pix) && !bytes.Equal(attached, frame.Pix)
	})
}

func until(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for libmpv output")
}
