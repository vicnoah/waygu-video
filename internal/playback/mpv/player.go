// Package mpv loads libmpv on desktop systems and renders video into pixels.
package mpv

import (
	"errors"
	"image"
	"strconv"
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

type Stats struct {
	Buffered              time.Duration
	CacheRanges           []CacheRange
	FPS                   float64
	Bitrate, DownloadRate int64
	DroppedFrames         int64
	VideoCodec, Hardware  string
	AudioOutput           string
}

// CacheRange is a seekable interval in the current demuxer cache.
type CacheRange struct {
	Start, End time.Duration
}

type TrackKind string

const (
	AudioTrack    TrackKind = "audio"
	SubtitleTrack TrackKind = "sub"
)

type Track struct {
	ID                     int64
	Kind                   TrackKind
	Title, Language, Codec string
	Selected, Forced       bool
	Default                bool
	External               bool
}

// Open creates a player and its software renderer. A caller must serialize all
// its methods, including Close, on one goroutine.
func Open() (*Player, error) { return open() }

// Available reports whether the desktop client API can be loaded.
func Available() bool { return available() }

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

func (p *Player) Volume(volume float32) error {
	return p.property("volume", strconv.FormatFloat(float64(100*max(0, min(1, volume))), 'f', 2, 32))
}

func (p *Player) Stats() Stats { return p.stats() }

func (p *Player) Speed(rate float32) error {
	return p.property("speed", strconv.FormatFloat(float64(rate), 'f', -1, 32))
}

// Tracks returns the audio and subtitle tracks the decoder can select.
func (p *Player) Tracks() []Track { return p.tracks() }

// SelectTrack changes one track without loading a new file. Subtitle ID zero disables subtitles.
func (p *Player) SelectTrack(kind TrackKind, id int64) error {
	switch {
	case kind == AudioTrack && id > 0:
		return p.property("aid", strconv.FormatInt(id, 10))
	case kind == SubtitleTrack && id > 0:
		if p.intProperty("secondary-sid") == id {
			if err := p.SecondarySubtitle(0); err != nil {
				return err
			}
		}
		return p.property("sid", strconv.FormatInt(id, 10))
	case kind == SubtitleTrack && id == 0:
		return p.property("sid", "no")
	default:
		return errors.New("invalid playback track")
	}
}

func (p *Player) SubtitleDelay(delay time.Duration) error {
	value := strconv.FormatFloat(delay.Seconds(), 'f', 3, 64)
	// Older libmpv versions use sub-delay for both decoders.
	_ = p.property("secondary-sub-delay", value)
	return p.property("sub-delay", value)
}

func (p *Player) ShowNativeSubtitle(visible bool) error {
	return p.property("sub-visibility", optionFlag(visible))
}

func (p *Player) SecondarySubtitle(id int64) error {
	if id <= 0 {
		return p.property("secondary-sid", "no")
	}
	if err := p.property("secondary-sub-visibility", "no"); err != nil {
		return err
	}
	return p.property("secondary-sid", strconv.FormatInt(id, 10))
}

// SubtitleText returns the current cue with formatting stripped, including when hidden.
func (p *Player) SubtitleText(secondary bool) string {
	if secondary {
		return p.stringProperty("secondary-sub-text")
	}
	return p.stringProperty("sub-text")
}

func (p *Player) EmbeddedSubtitleFonts(enabled bool) error {
	return p.property("embeddedfonts", optionFlag(enabled))
}

func optionFlag(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func (p *Player) Poll() Event { return p.poll() }

func (p *Player) Status() Status { return p.status() }

// Frame updates dst when libmpv has a new video frame or overlay to render.
func (p *Player) Frame(dst *image.RGBA, redraw bool) (bool, error) { return p.frame(dst, redraw) }

// Danmaku changes the independent ASS overlay without altering the subtitle track.
func (p *Player) Danmaku(track *danmaku.Track) { p.danmaku(track) }
