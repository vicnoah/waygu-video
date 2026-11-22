package main

import (
	"context"
	"image"
	"slices"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"waygu-video/internal/danmaku"
	"waygu-video/internal/playback/mpv"
)

type desktopVideo struct {
	node          core.Node
	active        func() bool
	seek          func(time.Duration)
	comments      func(*danmaku.Track)
	tracks        *core.Signal[[]mpv.Track]
	selectTrack   func(mpv.TrackKind, int64, func(error))
	subtitleDelay func(time.Duration)
}

type mpvCommand struct {
	paused        bool
	seek          time.Duration
	seekID        uint64
	track         *danmaku.Track
	rate          float32
	subtitleDelay time.Duration
}

type mpvTrackChange struct {
	kind mpv.TrackKind
	id   int64
	done func(error)
}

const (
	mpvOpening = iota
	mpvActive
	mpvFallback
)

func mpvVideo(cx *core.Scope, native core.Video) desktopVideo {
	mode := core.NewSignal(cx, mpvOpening)
	pixels := core.NewSignal[*image.RGBA](cx, nil)
	tracks := core.NewSignalFunc[[]mpv.Track](cx, nil, slices.Equal)
	requests := make(chan mpvTrackChange, 2)
	stopped := make(chan struct{})
	var desired atomic.Pointer[mpvCommand]
	cmd := mpvCommand{}
	desired.Store(&cmd)
	update := func(change func(*mpvCommand)) {
		copy := *desired.Load()
		change(&copy)
		desired.Store(&copy)
	}
	cx.Effect(func() {
		paused := native.PausedFunc()
		speed := native.RateFunc()
		update(func(c *mpvCommand) { c.paused, c.rate = paused, speed })
	})
	viewContext := cx.Context()
	go func() {
		playMPV(viewContext, cx, native, &desired, mode, pixels, tracks, requests)
		close(stopped)
		cx.Post(func() {
			tracks.Set(nil)
			for len(requests) > 0 {
				(<-requests).done(mpv.ErrUnavailable)
			}
		})
	}()
	node := core.Dynamic(func(cx *core.Scope) core.Node {
		switch mode.Get() {
		case mpvFallback:
			return native
		case mpvActive:
			return core.Image{Class: "absolute inset-0", ResizeMode: native.ResizeMode, StyleFunc: func() core.Style {
				if native.ResizeModeFunc != nil {
					return core.Style{ObjectFit: native.ResizeModeFunc()}
				}
				return core.Style{}
			}, TestID: native.TestID,
				SourceFunc: func() core.ImageSource { return core.ImageSource{Pixels: pixels.Get()} }}
		default:
			return nil
		}
	})
	return desktopVideo{node: node, active: func() bool { return mode.Get() == mpvActive },
		seek:     func(at time.Duration) { update(func(c *mpvCommand) { c.seek, c.seekID = at, c.seekID+1 }) },
		comments: func(track *danmaku.Track) { update(func(c *mpvCommand) { c.track = track }) },
		tracks:   tracks, subtitleDelay: func(delay time.Duration) {
			update(func(c *mpvCommand) { c.subtitleDelay = delay })
		},
		selectTrack: func(kind mpv.TrackKind, id int64, done func(error)) {
			select {
			case <-stopped:
				done(mpv.ErrUnavailable)
				return
			default:
			}
			select {
			case requests <- mpvTrackChange{kind: kind, id: id, done: done}:
			default:
				done(mpv.ErrUnavailable)
			}
		},
	}
}

func playMPV(ctx context.Context, cx *core.Scope, events core.Video, desired *atomic.Pointer[mpvCommand],
	mode *core.Signal[int], frames *core.Signal[*image.RGBA], tracks *core.Signal[[]mpv.Track], requests <-chan mpvTrackChange) {
	post := func(fn func()) {
		cx.Post(func() {
			if ctx.Err() == nil {
				fn()
			}
		})
	}
	player, err := mpv.Open()
	if err != nil {
		post(func() { mode.Set(mpvFallback) })
		return
	}
	defer player.Close()
	if ctx.Err() != nil {
		return
	}
	if err := player.Load(events.Source); err != nil {
		post(func() { mode.Set(mpvFallback) })
		return
	}
	post(func() { mode.Set(mpvActive) })

	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	var applied mpvCommand
	var scratch *image.RGBA
	var pendingFrame atomic.Bool
	var lastUpdate time.Time
	wasEnded, wasBuffering := false, false
	ready := false
	redraw := false
	for {
		select {
		case <-ctx.Done():
			return
		case change := <-requests:
			err := mpv.ErrUnavailable
			var current []mpv.Track
			if ready {
				err = player.SelectTrack(change.kind, change.id)
				current = player.Tracks()
				redraw = redraw || (err == nil && change.kind == mpv.SubtitleTrack)
			}
			post(func() {
				if err == nil {
					tracks.Set(current)
				}
				change.done(err)
			})
		case now := <-ticker.C:
			command := desired.Load()
			if command.paused != applied.paused {
				_ = player.Pause(command.paused)
			}
			if command.seekID != applied.seekID {
				_ = player.Seek(command.seek)
			}
			if command.rate > 0 && command.rate != applied.rate {
				_ = player.Speed(command.rate)
			}
			if command.track != applied.track {
				player.Danmaku(command.track)
			}
			if command.subtitleDelay != applied.subtitleDelay {
				redraw = player.SubtitleDelay(command.subtitleDelay) == nil || redraw
			}
			applied = *command
			event, status := player.Poll(), player.Status()
			if event.Failed {
				if !ready {
					post(func() { mode.Set(mpvFallback) })
				} else {
					post(func() { events.OnError(mediaDecodeError{}) })
				}
				return
			}
			if event.Loaded && !ready {
				ready = true
				current := player.Tracks()
				post(func() {
					tracks.Set(current)
					events.OnLoad(core.VideoInfo{Duration: status.Duration, RateChangeable: true,
						Size: core.Size{Width: core.Points(status.Width), Height: core.Points(status.Height)}})
				})
			}
			if !ready {
				continue
			}
			if status.Buffering != wasBuffering {
				wasBuffering = status.Buffering
				post(func() { events.OnBuffer(status.Buffering) })
			}
			ended := event.Ended || status.Ended
			if ended && !wasEnded {
				post(events.OnEnd)
			}
			wasEnded = ended
			if now.Sub(lastUpdate) >= 250*time.Millisecond && !ended {
				lastUpdate = now
				post(func() { events.OnProgress(status.Position, status.Duration) })
			}
			if status.Width <= 0 || status.Height <= 0 || pendingFrame.Load() {
				continue
			}
			box := fittedFrame(status.Width, status.Height)
			resize := scratch == nil || scratch.Bounds() != box
			if resize {
				scratch = image.NewRGBA(box)
			}
			drawn, err := player.Frame(scratch, resize || redraw)
			redraw = false
			if err != nil {
				post(func() { events.OnError(err) })
				return
			}
			if drawn {
				frame := image.NewRGBA(box)
				copy(frame.Pix, scratch.Pix)
				pendingFrame.Store(true)
				post(func() { frames.Set(frame); pendingFrame.Store(false) })
			}
		}
	}
}

type mediaDecodeError struct{}

func (mediaDecodeError) Error() string { return "libmpv could not open the video" }

func fittedFrame(width, height int) image.Rectangle {
	const limitWidth, limitHeight = 960, 540
	if width > limitWidth {
		height, width = max(1, height*limitWidth/width), limitWidth
	}
	if height > limitHeight {
		width, height = max(1, width*limitHeight/height), limitHeight
	}
	return image.Rect(0, 0, width, height)
}
