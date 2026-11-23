package main

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

type homeDay struct {
	start, end time.Time
}

func calendarHomeDay(now time.Time) homeDay {
	year, month, day := now.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	return homeDay{start: start, end: start.AddDate(0, 0, 1)}
}

func (a *application) homeDailyAdditions(cx *core.Scope) *core.Resource[int] {
	day := core.NewSignal(cx, calendarHomeDay(cx.WallTime()))
	cx.Effect(func() {
		cx.AfterFunc(day.Get().end.Sub(cx.WallTime()), func() {
			day.Set(calendarHomeDay(cx.WallTime()))
		})
	})
	return core.NewResource(cx, func() (catalogRequest, bool) {
		a.libraryChanges.Get()
		sources := a.sources.Get()
		return catalogRequest{sources: sources, parental: a.parentalRules(), filter: a.sourceFilter.Get(),
			merge: a.mergeTitles.Get(), home: true, addedDay: day.Get()}, len(sources) > 0
	}, func(ctx context.Context, request catalogRequest, _ core.ResourceInfo) (int, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		result := a.loadCatalog(ctx, request)
		return len(result.items), result.failure
	})
}

func homeAddedOnDay(ctx context.Context, list func(context.Context, media.Query) (media.Page, error),
	query media.Query, day homeDay, sorted bool) (media.Page, error) {
	query.Limit = 100
	var items []media.Item
	seen := make(map[string]bool)
	for {
		batch, err := list(ctx, query)
		if err != nil {
			return media.Page{}, err
		}
		var progressed bool
		for _, item := range batch.Items {
			if item.AddedAt.IsZero() {
				return media.Page{}, media.ErrResponse
			}
			if sorted && item.AddedAt.Before(day.start) {
				return media.Page{Items: items, Total: len(items)}, nil
			}
			if !seen[item.ID] {
				seen[item.ID] = true
				progressed = true
				if !item.AddedAt.Before(day.start) && item.AddedAt.Before(day.end) {
					items = append(items, item)
				}
			}
		}
		query.Offset += len(batch.Items)
		if query.Offset >= batch.Total {
			return media.Page{Items: items, Total: len(items)}, nil
		}
		if !progressed {
			return media.Page{}, media.ErrResponse
		}
	}
}
