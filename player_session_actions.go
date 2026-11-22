package main

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

type playerSleepTimer struct {
	delay    time.Duration
	deadline time.Time
}

func (a *application) playerSleep(cx *core.Scope, expired func()) {
	cx.Effect(func() {
		timer := a.sleepTimer.Get()
		if timer.deadline.IsZero() {
			return
		}
		fire := func() {
			if a.sleepTimer.GetUntracked() == timer {
				cx.Batch(func() {
					expired()
					a.sleepTimer.Set(playerSleepTimer{})
				})
			}
		}
		if remaining := timer.deadline.Sub(cx.Now()); remaining > 0 {
			cx.AfterFunc(remaining, fire)
		} else {
			fire()
		}
	})
}

const minSegmentDuration = time.Second

type playbackSegment struct {
	start, end       time.Duration
	hasStart, active bool
}

type segmentLoop struct {
	segment *core.Signal[playbackSegment]
	waiting time.Time
}

func (loop *segmentLoop) clear() {
	loop.waiting = time.Time{}
	loop.segment.Set(playbackSegment{})
}

func (loop *segmentLoop) markStart(at time.Duration) {
	loop.waiting = time.Time{}
	loop.segment.Set(playbackSegment{start: at, hasStart: true})
}

func (loop *segmentLoop) markEnd(at, duration time.Duration) {
	segment := loop.segment.GetUntracked()
	if !segment.hasStart || at-segment.start < minSegmentDuration || at > duration {
		return
	}
	loop.waiting = time.Time{}
	segment.end, segment.active = at, true
	loop.segment.Set(segment)
}

func (loop *segmentLoop) manualSeek(at time.Duration) {
	segment := loop.segment.GetUntracked()
	if segment.active && (at < segment.start || at >= segment.end) {
		loop.clear()
	}
}

// boundary prevents completion and discards stale end-of-segment reports
// while the decoder catches up with a seek to A.
func (loop *segmentLoop) boundary(at, duration time.Duration, now time.Time, seek func(time.Duration)) bool {
	segment := loop.segment.GetUntracked()
	if !segment.active {
		return false
	}
	if segment.end > duration {
		loop.clear()
		return false
	}
	if at < segment.end {
		loop.waiting = time.Time{}
		return false
	}
	if loop.waiting.IsZero() {
		loop.waiting = now
		seek(segment.start)
	} else if now.Sub(loop.waiting) > 3*time.Second {
		loop.clear()
		return false
	}
	return true
}
