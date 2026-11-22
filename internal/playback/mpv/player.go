// Package mpv loads libmpv on desktop systems and renders video into pixels.
package mpv

import (
	"errors"
	"image"
	"time"

	"waygu-video/internal/danmaku"
)

var ErrUnavailable = errors.New("libmpv is unavailable")

type Player struct {
	backend
}

type Event struct {
	Loaded bool
	Ended  bool
	Failed bool
}

type Status struct {
	Position, Duration       time.Duration
	Width, Height            int
	Paused, Buffering, Ended bool
}

// Open creates a player and its software renderer. A caller must serialize all
// its methods, including Close, on one goroutine.
func Open() (*Player, error) { return open() }

func (p *Player) Close() { p.close() }

func (p *Player) Load(address string) error { return p.command("loadfile", address, "replace") }

func (p *Player) Pause(paused bool) error {
	state := "no"
	if paused {
		state = "yes"
	}
	return p.property("pause", state)
}

func (p *Player) Seek(position time.Duration) error { return p.seek(max(0, position)) }

func (p *Player) Poll() Event { return p.poll() }

func (p *Player) Status() Status { return p.status() }

// Frame updates dst when libmpv has a new video frame or overlay to render.
func (p *Player) Frame(dst *image.RGBA, redraw bool) (bool, error) { return p.frame(dst, redraw) }

// Danmaku changes the independent ASS overlay without altering the subtitle track.
func (p *Player) Danmaku(track *danmaku.Track) { p.danmaku(track) }
