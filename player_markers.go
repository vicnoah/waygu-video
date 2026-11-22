package main

import (
	"context"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

type markerSource struct {
	version, id string
	duration    time.Duration
}

func (a *application) playbackMarkers(cx *core.Scope, current route, client media.Client, stream func() media.Stream, enabled func() bool) func() []media.PlaybackMarker {
	provider, _ := client.(media.MarkerClient)
	audioCapable := current.item.Kind == media.Episode && current.item.Episode > 0 && current.item.SeriesID != ""
	if provider == nil && !audioCapable {
		return func() []media.PlaybackMarker { return nil }
	}
	source := core.NewMemo(cx, func() markerSource { return sourceForMarkers(stream()) })
	type result struct {
		source  markerSource
		markers []media.PlaybackMarker
	}
	resource := core.NewResource(cx, func() (markerSource, bool) { return source.Get(), enabled() },
		func(ctx context.Context, key markerSource, _ core.ResourceInfo) (result, error) {
			var markers []media.PlaybackMarker
			if provider != nil {
				ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				markers, _ = provider.PlaybackMarkers(ctx, current.item, media.Stream{SourceID: key.id, Duration: key.duration})
			}
			return result{key, markers}, nil
		})
	known := func() (result, bool) {
		loaded, ready := resource.Get()
		return loaded, ready && loaded.source == source.Get() && enabled()
	}
	detected := core.NewResource(cx, func() (audioMarkerRequest, bool) {
		loaded, ready := known()
		if !ready || !audioCapable {
			return audioMarkerRequest{}, false
		}
		var missing []media.MarkerKind
		for _, kind := range []media.MarkerKind{media.MarkerIntro, media.MarkerCredits} {
			if !slices.ContainsFunc(loaded.markers, func(marker media.PlaybackMarker) bool { return marker.Kind == kind }) {
				missing = append(missing, kind)
			}
		}
		if len(missing) == 0 {
			return audioMarkerRequest{}, false
		}
		return audioMarkerRequest{loaded.source, core.Untracked(cx, stream), missing, a.parentalRules()}, true
	}, func(ctx context.Context, request audioMarkerRequest, _ core.ResourceInfo) (result, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return result{request.key, a.inferredMarkers(ctx, current, client, request)}, nil
	})
	combinedMarkers := core.NewMemoFunc(cx, func() []media.PlaybackMarker {
		loaded, ready := known()
		if !ready {
			return nil
		}
		found, analyzed := detected.Get()
		if !analyzed || found.source != loaded.source {
			return loaded.markers
		}
		combined := slices.Clone(loaded.markers)
		for _, marker := range found.markers {
			if !slices.ContainsFunc(loaded.markers, func(known media.PlaybackMarker) bool { return known.Kind == marker.Kind }) {
				combined = append(combined, marker)
			}
		}
		return combined
	}, slices.Equal)
	return combinedMarkers.Get
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
