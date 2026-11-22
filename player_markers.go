package main

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

type markerSource struct {
	version, id string
	duration    time.Duration
}

func playbackMarkers(cx *core.Scope, item media.Item, client media.Client, stream func() media.Stream, enabled func() bool) func() []media.PlaybackMarker {
	provider, supported := client.(media.MarkerClient)
	if !supported {
		return func() []media.PlaybackMarker { return nil }
	}
	source := core.NewMemo(cx, func() markerSource {
		active := stream()
		return markerSource{active.VersionID(), active.SourceID, active.Duration}
	})
	type result struct {
		source  markerSource
		markers []media.PlaybackMarker
	}
	resource := core.NewResource(cx, func() (markerSource, bool) { return source.Get(), enabled() },
		func(ctx context.Context, key markerSource, _ core.ResourceInfo) (result, error) {
			ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			markers, err := provider.PlaybackMarkers(ctx, item, media.Stream{SourceID: key.id, Duration: key.duration})
			return result{key, markers}, err
		})
	return func() []media.PlaybackMarker {
		loaded, ready := resource.Get()
		if ready && loaded.source == source.Get() && enabled() {
			return loaded.markers
		}
		return nil
	}
}

type markerSkip struct {
	skipped             map[media.PlaybackMarker]bool
	manual              time.Duration
	target              time.Duration
	ignoreProgressUntil time.Time
}

func (s *markerSkip) manualSeek(at time.Duration) {
	s.manual = at
	s.ignoreProgressUntil = time.Time{}
}

func (s *markerSkip) pending(at time.Duration, now time.Time) bool {
	if s.ignoreProgressUntil.IsZero() {
		return false
	}
	if at < s.target-250*time.Millisecond && now.Before(s.ignoreProgressUntil) {
		return true
	}
	s.ignoreProgressUntil = time.Time{}
	return false
}

func (s *markerSkip) next(markers []media.PlaybackMarker, at, duration time.Duration, now time.Time) (target time.Duration, finish bool) {
	for _, marker := range markers {
		if at < marker.Start || at >= marker.End || s.skipped[marker] ||
			(s.manual >= marker.Start && s.manual < marker.End) {
			continue
		}
		target = min(marker.End, duration)
		if target-at <= 250*time.Millisecond {
			continue
		}
		finish = marker.Kind == media.MarkerCredits && duration-target <= 250*time.Millisecond
		if !finish && target == duration {
			continue
		}
		if s.skipped == nil {
			s.skipped = make(map[media.PlaybackMarker]bool)
		}
		s.skipped[marker] = true
		s.target = target
		s.ignoreProgressUntil = now.Add(2 * time.Second)
		return target, finish
	}
	return 0, false
}
