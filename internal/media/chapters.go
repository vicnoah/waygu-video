package media

import (
	"cmp"
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Chapter struct {
	Name  string
	Start time.Duration
}

// ChapterClient returns chapter starts for the selected playback source.
type ChapterClient interface {
	Chapters(context.Context, Item, string) ([]Chapter, error)
}

type browserChapter struct {
	Name, MarkerType   string
	StartPositionTicks int64
}

func (m *mediaBrowser) browserChapters(ctx context.Context, item Item, sourceID string) ([]browserChapter, error) {
	var chapters []browserChapter
	if m.kind == Emby && sourceID != "" {
		var info browserPlaybackInfo
		query := m.userQuery()
		query.Set("MediaSourceId", sourceID)
		if err := m.get(ctx, "Items/"+url.PathEscape(item.ID)+"/PlaybackInfo", query, &info); err != nil {
			return nil, err
		}
		index := slices.IndexFunc(info.MediaSources, func(source browserSource) bool { return source.ID == sourceID })
		if index < 0 {
			return nil, nil
		}
		source := info.MediaSources[index]
		chapters = source.Chapters
		if source.ItemID != "" {
			item.ID = source.ItemID
		} else if index > 0 && len(chapters) == 0 {
			return nil, nil
		}
	} else if m.kind == Jellyfin && sourceID != "" {
		// Jellyfin reports the underlying video item's ID without dashes.
		item.ID = sourceID
	}
	if len(chapters) == 0 {
		var response struct{ Chapters []browserChapter }
		route := "Items/" + url.PathEscape(item.ID)
		query := m.userQuery()
		query.Set("Fields", "Chapters")
		if m.kind == Emby {
			route = m.userRoute() + "/" + route
		}
		err := m.get(ctx, route, query, &response)
		if m.kind == Jellyfin && errors.Is(err, StatusError(http.StatusNotFound)) {
			err = m.get(ctx, m.userRoute()+"/"+route, query, &response)
		}
		if err != nil {
			return nil, err
		}
		chapters = response.Chapters
	}
	return chapters, nil
}

func (m *mediaBrowser) Chapters(ctx context.Context, item Item, sourceID string) ([]Chapter, error) {
	chapters, err := m.browserChapters(ctx, item, sourceID)
	return ordinaryBrowserChapters(chapters), err
}

func ordinaryBrowserChapters(chapters []browserChapter) []Chapter {
	var result []Chapter
	for _, chapter := range chapters {
		if chapter.MarkerType != "" && !strings.EqualFold(chapter.MarkerType, "Chapter") {
			continue
		}
		if ticks := chapter.StartPositionTicks; ticks >= 0 && ticks <= math.MaxInt64/100 {
			result = append(result, Chapter{chapter.Name, fromTicks(ticks)})
		}
	}
	return sortedChapters(result)
}

type plexChapter struct {
	Tag             string
	StartTimeOffset int64
}

func (p *plexClient) Chapters(ctx context.Context, item Item, sourceID string) ([]Chapter, error) {
	response, err := p.get(ctx, plexItemRoute(item.ID), url.Values{"includeChapters": {"1"}})
	if err != nil {
		return nil, err
	}
	if len(response.Metadata) == 0 {
		return nil, nil
	}
	chapters, _ := plexChapters(response.Metadata[0], sourceID)
	return chapters, nil
}

func plexChapters(entry plexMetadata, sourceID string) ([]Chapter, bool) {
	chapters := entry.Chapter
	primary := true
	if sourceID != "" {
		index := slices.IndexFunc(entry.Media, func(source plexMedia) bool { return strconv.Itoa(source.ID) == sourceID })
		if index < 0 || len(entry.Media[index].Part) == 0 {
			return nil, false
		}
		primary = index == 0
		if part := entry.Media[index].Part[0]; len(part.Chapter) > 0 {
			chapters = part.Chapter
		} else if !primary {
			// The response's item-level chapter list is for its primary copy.
			return nil, false
		}
	}
	var result []Chapter
	for _, chapter := range chapters {
		if offset := chapter.StartTimeOffset; offset >= 0 && offset <= math.MaxInt64/int64(time.Millisecond) {
			result = append(result, Chapter{chapter.Tag, millis(offset)})
		}
	}
	return sortedChapters(result), primary
}

func sortedChapters(chapters []Chapter) []Chapter {
	slices.SortStableFunc(chapters, func(a, b Chapter) int { return cmp.Compare(a.Start, b.Start) })
	return slices.CompactFunc(chapters, func(a, b Chapter) bool { return a.Start == b.Start })
}
