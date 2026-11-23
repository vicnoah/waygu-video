package main

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"golang.org/x/sync/errgroup"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) homeSeriesUpdates(cx *core.Scope, limit func() int, idiom core.Idiom) *core.Resource[catalogResult] {
	enabled := core.NewMemo(cx, func() bool {
		prefs := a.appearancePrefs.Get()
		if !prefs.enabled(homeSeries) {
			return false
		}
		if prefs.HomeOrdered || !prefs.enabled(homeRecent) {
			return true
		}
		switch idiom {
		case core.IdiomTV:
			return false
		case core.IdiomTablet:
			return prefs.enabled(homeGrouped)
		default:
			return true
		}
	})
	return core.NewResource(cx, func() (catalogRequest, bool) {
		sources := a.sources.Get()
		a.libraryChanges.Get()
		return catalogRequest{sources: sources, parental: a.parentalRules(), filter: a.sourceFilter.Get(),
			home: true, limit: limit(), merge: a.mergeTitles.Get()}, len(sources) > 0 && enabled.Get()
	}, func(ctx context.Context, request catalogRequest, _ core.ResourceInfo) (catalogResult, error) {
		return a.loadHomeSeriesUpdates(ctx, request), nil
	})
}

func (a *application) loadHomeSeriesUpdates(ctx context.Context, q catalogRequest) catalogResult {
	groups := make([]catalogResult, len(q.sources))
	var workers sync.WaitGroup
	for i, endpoint := range q.sources {
		if !q.parental.sourceAllowed(endpoint.ID) || q.filter != "" && q.filter != endpoint.ID {
			continue
		}
		workers.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			groups[i] = a.seriesUpdatesForSource(ctx, q, endpoint)
		})
	}
	workers.Wait()
	var result catalogResult
	for _, group := range groups {
		result.items = append(result.items, group.items...)
		result.hasMore = result.hasMore || group.hasMore
	}
	slices.SortStableFunc(result.items, func(a, b catalogItem) int {
		return b.latest.AddedAt.Compare(a.latest.AddedAt)
	})
	if q.merge && q.filter == "" && len(q.sources) > 1 {
		result.items = mergeCatalogItems(result.items, true, "")
	}
	result.hasMore = result.hasMore || len(result.items) > q.limit
	result.items = result.items[:min(q.limit, len(result.items))]
	return result
}

func (a *application) seriesUpdatesForSource(ctx context.Context, q catalogRequest, endpoint source) catalogResult {
	var result catalogResult
	libraries := slices.DeleteFunc(slices.Clone(endpoint.Libraries), func(library sourceLibrary) bool {
		kind := strings.ToLower(library.Kind)
		return !q.includesLibrary(endpoint.ID, library) || kind == "movie" || kind == "movies"
	})
	if len(libraries) == 0 {
		return result
	}
	client, err := a.client(ctx, endpoint)
	if err != nil {
		return result
	}
	all := seriesUpdates{endpoint: endpoint, parental: q.parental, index: make(map[string]int)}
	for _, library := range libraries {
		if episodes, ok := client.(media.EpisodeCatalogClient); ok {
			result.hasMore = all.readRecent(ctx, client, episodes, library.ID, q.limit) || result.hasMore
		} else if endpoint.Provider == media.Flymoo {
			all.readFlymoo(ctx, client, library.ID)
		}
	}
	result.items = all.items
	return result
}

type seriesUpdates struct {
	endpoint source
	parental parentalPolicy
	items    []catalogItem
	index    map[string]int
}

func (s *seriesUpdates) readRecent(ctx context.Context, client media.Client, episodes media.EpisodeCatalogClient, library string, limit int) bool {
	q := media.Query{LibraryID: library, Limit: 100, CatalogFacts: true}
	lookup := make(map[string]media.Item)
	libraryGroups := make(map[string]bool)
	seen := make(map[string]bool)
	for ctx.Err() == nil {
		page, err := episodes.RecentEpisodes(ctx, q)
		if err != nil || len(page.Items) == 0 {
			return false
		}
		previous := len(seen)
		for _, episode := range page.Items {
			if seen[episode.ID] || episode.ID == "" {
				continue
			}
			seen[episode.ID] = true
			if episode.SeriesID == "" || episode.AddedAt.IsZero() || episode.Kind != media.Episode ||
				episode.LibraryID != "" && episode.LibraryID != library {
				continue
			}
			show, found := lookup[episode.SeriesID]
			if !found {
				item, err := client.Detail(ctx, media.Item{ID: episode.SeriesID, Kind: media.Series, LibraryID: library})
				if err == nil {
					show = item
				}
				lookup[episode.SeriesID] = show
			}
			if show.ID == episode.SeriesID && s.add(show, episode, library) {
				libraryGroups[show.ID] = true
			}
		}
		q.Offset += len(page.Items)
		if q.Offset >= page.Total || len(seen) == previous {
			return false
		}
		if len(libraryGroups) >= limit {
			oldest := page.Items[len(page.Items)-1].AddedAt
			// A batch import can share one timestamp, even across pages.
			// Finish that timestamp before choosing which episode to show.
			distinct := 0
			for id := range libraryGroups {
				if s.items[s.index[id]].latest.AddedAt.After(oldest) {
					distinct++
				}
			}
			if distinct >= limit {
				return true
			}
		}
	}
	return false
}

func (s *seriesUpdates) add(show, episode media.Item, library string) bool {
	if !s.eligible(show, episode, library) {
		return false
	}
	show.LibraryID, episode.LibraryID = library, library
	if i, found := s.index[show.ID]; found {
		if newerEpisode(episode, *s.items[i].latest) {
			s.items[i].latest = &episode
		}
	} else {
		s.index[show.ID] = len(s.items)
		s.items = append(s.items, catalogItem{source: s.endpoint, item: show, latest: &episode})
	}
	return true
}

func (s *seriesUpdates) eligible(show, episode media.Item, library string) bool {
	if show.Kind != media.Series || episode.Kind != media.Episode || episode.ID == "" || episode.SeriesID != show.ID || episode.AddedAt.IsZero() || show.LibraryID != "" && show.LibraryID != library ||
		episode.LibraryID != "" && episode.LibraryID != library {
		return false
	}
	show.LibraryID, episode.LibraryID = library, library
	if episode.ContentRating == "" {
		episode.ContentRating = show.ContentRating
	}
	if len(episode.Genres) == 0 {
		episode.Genres = show.Genres
	}
	if !s.parental.visible(s.endpoint.ID, show) || !s.parental.visible(s.endpoint.ID, episode) {
		return false
	}
	return true
}

func newerEpisode(a, b media.Item) bool {
	return cmp.Or(a.AddedAt.Compare(b.AddedAt), cmp.Compare(a.Season, b.Season), cmp.Compare(a.Episode, b.Episode)) > 0
}

func (s *seriesUpdates) readFlymoo(ctx context.Context, client media.Client, library string) {
	page, _ := scanCatalogPages(ctx, client.Catalog, media.Query{LibraryID: library})
	// FnOS exposes season and episode lists through its existing detail routes.
	// Bound those requests without using the series' own addition order as a cutoff.
	var workers errgroup.Group
	workers.SetLimit(4)
	results := make([]media.Item, len(page.Items))
	for i, show := range page.Items {
		if ctx.Err() != nil {
			break
		}
		if show.Kind != media.Series || show.LibraryID != "" && show.LibraryID != library {
			continue
		}
		show.LibraryID = library
		if !s.parental.visible(s.endpoint.ID, show) {
			continue
		}
		workers.Go(func() error {
			seasons, _ := client.Children(ctx, show)
			for _, season := range seasons {
				if season.Kind == media.Season {
					episodes, _ := client.Children(ctx, season)
					for _, episode := range episodes {
						if s.eligible(show, episode, library) && newerEpisode(episode, results[i]) {
							results[i] = episode
						}
					}
				}
			}
			return nil
		})
	}
	_ = workers.Wait()
	for i, episode := range results {
		s.add(page.Items[i], episode, library)
	}
}

func seriesUpdateCaption(cx *core.Scope, episode media.Item, today bool) string {
	number := strconv.Itoa(episode.Episode)
	if today && episode.Episode > 0 {
		return locales.HomeEpisodeToday(cx, number)
	}
	if episode.Season == 0 && episode.Episode > 0 {
		return locales.HomeSpecialEpisode(cx, number)
	}
	if episode.Episode == 0 {
		return episode.Title
	}
	return locales.HomeEpisode(cx, strconv.Itoa(episode.Season), number)
}
