//go:build linux && !android

package mpv

import (
	"bytes"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"waygu-video/internal/danmaku"
)

func TestRenderWithASS(t *testing.T) {
	if os.Getenv("WAYGU_TEST_MPV") != "1" {
		t.Skip("generate the Docker video fixtures and set WAYGU_TEST_MPV=1")
	}
	fixture, err := filepath.Abs("../../../integration/servers/media/Movies/Waygu Test Movie (2025)/Waygu Test Movie (2025).mp4")
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
		return loaded && drawn && p.Status().Width == 640 && frame.RGBAAt(20, 20).R > 0
	})
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
	track := danmaku.Schedule([]danmaku.Comment{
		{At: time.Second, Mode: danmaku.Top, Color: danmaku.White, Text: "Waygu fixture comments"},
	}, danmaku.DefaultSettings())
	p.Danmaku(&track)
	until(t, func() bool {
		if _, err := p.Frame(frame, true); err != nil {
			t.Fatal(err)
		}
		row := 55 * frame.Stride
		return !bytes.Equal(baseline[:row], frame.Pix[:row]) && bytes.Equal(baseline[row:], frame.Pix[row:])
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
