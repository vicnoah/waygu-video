package media

import (
	"cmp"
	"context"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"
)

type MarkerKind uint8

const (
	MarkerIntro MarkerKind = iota + 1
	MarkerCredits
)

type PlaybackMarker struct {
	Kind       MarkerKind
	Start, End time.Duration
}

// MarkerClient resolves ranges on the cut selected for playback.
type MarkerClient interface {
	PlaybackMarkers(context.Context, Item, Stream) ([]PlaybackMarker, error)
}

func (m *mediaBrowser) PlaybackMarkers(ctx context.Context, item Item, stream Stream) ([]PlaybackMarker, error) {
	var markers []PlaybackMarker
	if m.kind == Jellyfin {
		id := stream.SourceID
		if id == "" {
			id = item.ID
		}
		var result struct {
			Items []struct {
				Type                 string
				StartTicks, EndTicks int64
			}
		}
		filter := url.Values{"includeSegmentTypes": {"Intro", "Outro"}}
		// Older Jellyfin releases have only chapters.
		if err := m.get(ctx, "MediaSegments/"+url.PathEscape(id), filter, &result); err == nil {
			for _, entry := range result.Items {
				var kind MarkerKind
				switch strings.ToLower(entry.Type) {
				case "intro":
					kind = MarkerIntro
				case "outro":
					kind = MarkerCredits
				default:
					continue
				}
				if entry.StartTicks >= 0 && entry.EndTicks > entry.StartTicks && entry.EndTicks <= math.MaxInt64/100 {
					markers = append(markers, PlaybackMarker{kind, fromTicks(entry.StartTicks), fromTicks(entry.EndTicks)})
				}
			}
		}
	}
	markers = validMarkers(markers, stream.Duration)
	if hasMarker(markers, MarkerIntro) && hasMarker(markers, MarkerCredits) {
		return sortedMarkers(markers), nil
	}
	chapters, err := m.browserChapters(ctx, item, stream.SourceID)
	if err != nil {
		if len(markers) != 0 {
			return sortedMarkers(markers), nil
		}
		return nil, err
	}
	if m.kind == Emby {
		markers = embyMarkers(chapters, stream.Duration)
	}
	return addNamedMarkers(markers, ordinaryBrowserChapters(chapters), stream.Duration), nil
}

func embyMarkers(chapters []browserChapter, length time.Duration) []PlaybackMarker {
	marked := slices.Clone(chapters)
	slices.SortStableFunc(marked, func(a, b browserChapter) int { return cmp.Compare(a.StartPositionTicks, b.StartPositionTicks) })
	var result []PlaybackMarker
	start := time.Duration(-1)
	for _, chapter := range marked {
		if chapter.StartPositionTicks < 0 || chapter.StartPositionTicks > math.MaxInt64/100 {
			continue
		}
		at := fromTicks(chapter.StartPositionTicks)
		switch strings.ToLower(chapter.MarkerType) {
		case "introstart":
			start = at
		case "introend":
			if start >= 0 && start < at {
				result = append(result, PlaybackMarker{MarkerIntro, start, at})
			}
			start = -1
		case "creditsstart":
			result = append(result, PlaybackMarker{MarkerCredits, at, length})
		}
	}
	return validMarkers(result, length)
}

type plexMarker struct {
	Type                           string
	StartTimeOffset, EndTimeOffset int64
}

func (p *plexClient) PlaybackMarkers(ctx context.Context, item Item, stream Stream) ([]PlaybackMarker, error) {
	response, err := p.get(ctx, plexItemRoute(item.ID), url.Values{"includeMarkers": {"1"}, "includeChapters": {"1"}})
	if err != nil || len(response.Metadata) == 0 {
		return nil, err
	}
	entry := response.Metadata[0]
	chapters, primary := plexChapters(entry, stream.SourceID)
	var markers []PlaybackMarker
	if primary {
		for _, marker := range entry.Marker {
			var kind MarkerKind
			switch marker.Type {
			case "intro":
				kind = MarkerIntro
			case "credits":
				kind = MarkerCredits
			default:
				continue
			}
			if marker.StartTimeOffset >= 0 && marker.EndTimeOffset > marker.StartTimeOffset && marker.EndTimeOffset <= math.MaxInt64/int64(time.Millisecond) {
				markers = append(markers, PlaybackMarker{kind, millis(marker.StartTimeOffset), millis(marker.EndTimeOffset)})
			}
		}
	}
	return addNamedMarkers(validMarkers(markers, stream.Duration), chapters, stream.Duration), nil
}

func hasMarker(markers []PlaybackMarker, kind MarkerKind) bool {
	return slices.ContainsFunc(markers, func(marker PlaybackMarker) bool { return marker.Kind == kind })
}

func validMarkers(markers []PlaybackMarker, length time.Duration) []PlaybackMarker {
	return slices.DeleteFunc(markers, func(marker PlaybackMarker) bool {
		return marker.Start < 0 || marker.End <= marker.Start ||
			(length > 0 && (marker.Start >= length || marker.End-length > time.Second))
	})
}

func addNamedMarkers(markers []PlaybackMarker, chapters []Chapter, length time.Duration) []PlaybackMarker {
	intro, credits := hasMarker(markers, MarkerIntro), hasMarker(markers, MarkerCredits)
	for i, chapter := range chapters {
		end := length
		if i+1 < len(chapters) {
			end = chapters[i+1].Start
		}
		if end-chapter.Start < time.Second || end > length {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(chapter.Name)) {
		case "intro", "opening", "op", "片头", "片頭", "オープニング":
			if !intro && i+1 < len(chapters) && chapter.Start < length/3 && end-chapter.Start <= 2*time.Minute {
				markers = append(markers, PlaybackMarker{MarkerIntro, chapter.Start, end})
			}
		case "credits", "end credits", "outro", "ending", "ed", "片尾", "エンディング":
			if !credits && chapter.Start >= length-length/4 {
				markers = append(markers, PlaybackMarker{MarkerCredits, chapter.Start, end})
			}
		}
	}
	return sortedMarkers(markers)
}

func sortedMarkers(markers []PlaybackMarker) []PlaybackMarker {
	slices.SortStableFunc(markers, func(a, b PlaybackMarker) int {
		if order := cmp.Compare(a.Start, b.Start); order != 0 {
			return order
		}
		if order := cmp.Compare(a.End, b.End); order != 0 {
			return order
		}
		return cmp.Compare(a.Kind, b.Kind)
	})
	return slices.Compact(markers)
}
