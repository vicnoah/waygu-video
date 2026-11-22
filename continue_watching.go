package main

import (
	"context"
	"slices"
	"sync"
	"time"

	"waygu-video/internal/media"
)

const continueWatchingLimit = 24

// A resume record points at a server copy, not just a title. Keeping the
// entire winning copy prevents seeking on one server and reporting to another.
func continueWatching(resume, catalog []catalogItem, merge bool) []catalogItem {
	entries := slices.Clone(resume)
	if !merge {
		sortRecent(entries)
		return entries[:min(len(entries), continueWatchingLimit)]
	}
	for _, entry := range catalog {
		item := entry.item
		if item.Kind == media.Movie && item.Watched && item.Position <= 0 && !item.LastPlayed.IsZero() {
			entries = append(entries, entry)
		}
	}
	aliases := aliasesFor(catalog)
	for _, entry := range entries {
		aliases.add(resumeTitleID(entry.item))
		aliases.add(identityWithIDs(media.Series, entry.item.SeriesExternalIDs))
	}
	episodes := indexResumeEpisodes(entries, aliases)
	sortRecent(entries)
	winners := make([]catalogItem, 0, min(len(resume), continueWatchingLimit))
	seen := make(map[resumeIdentity]bool)
	for _, entry := range entries {
		key := episodes.identity(entry.item, aliases)
		if key.title.kind != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		if canResume(entry.item) {
			winners = append(winners, entry)
			if len(winners) == continueWatchingLimit {
				break
			}
		}
	}
	return winners
}

func canResume(item media.Item) bool {
	return item.ID != "" && playable(item) && item.Position > 0 && (item.Duration <= 0 || item.Position < item.Duration)
}

func sortRecent(entries []catalogItem) {
	slices.SortStableFunc(entries, func(a, b catalogItem) int {
		return b.item.LastPlayed.Compare(a.item.LastPlayed)
	})
}

type resumeIdentity struct {
	title          titleIdentity
	season, number int
}

func resumeTitleID(item media.Item) titleIdentity {
	if item.Kind == media.Episode {
		return identityWithIDs(media.Episode, item.ExternalIDs)
	}
	return identityOf(item)
}

func episodeIdentity(item media.Item, aliases titleAliases) (direct, parent resumeIdentity) {
	if item.Kind != media.Episode {
		return direct, parent
	}
	if ids := aliases.canonical(resumeTitleID(item)); ids.kind != "" {
		direct = resumeIdentity{title: ids, season: item.Season, number: item.Episode}
	}
	// Season zero can mean unknown numbering. Specials can match by an episode ID.
	if ids := aliases.canonical(identityWithIDs(media.Series, item.SeriesExternalIDs)); ids.kind != "" && item.Season > 0 && item.Episode > 0 {
		parent = resumeIdentity{title: ids, season: item.Season, number: item.Episode}
	}
	return direct, parent
}

type resumeEpisodes struct {
	parents   map[resumeIdentity]resumeIdentity
	conflicts map[resumeIdentity]bool
}

func indexResumeEpisodes(entries []catalogItem, aliases titleAliases) resumeEpisodes {
	index := resumeEpisodes{parents: make(map[resumeIdentity]resumeIdentity), conflicts: make(map[resumeIdentity]bool)}
	known := make(map[resumeIdentity]titleIdentity)
	for _, entry := range entries {
		direct, parent := episodeIdentity(entry.item, aliases)
		if direct.title.kind == "" || parent.title.kind == "" {
			continue
		}
		if previous, exists := index.parents[direct]; exists && previous != parent {
			index.parents[direct] = resumeIdentity{}
		} else {
			index.parents[direct] = parent
		}
		ids := known[parent]
		if (ids.imdb != "" && direct.title.imdb != "" && ids.imdb != direct.title.imdb) ||
			(ids.tmdb != "" && direct.title.tmdb != "" && ids.tmdb != direct.title.tmdb) {
			index.conflicts[parent] = true
			continue
		}
		if ids.imdb == "" {
			ids.imdb = direct.title.imdb
		}
		if ids.tmdb == "" {
			ids.tmdb = direct.title.tmdb
		}
		known[parent] = ids
	}
	return index
}

func (episodes resumeEpisodes) identity(item media.Item, aliases titleAliases) resumeIdentity {
	if item.Kind != media.Episode {
		return resumeIdentity{title: aliases.canonical(resumeTitleID(item))}
	}
	direct, parent := episodeIdentity(item, aliases)
	if parent.title.kind == "" {
		parent = episodes.parents[direct]
	}
	if parent.title.kind != "" && !episodes.conflicts[parent] {
		return parent
	}
	return direct
}

func enrichResumeSeries(ctx context.Context, client media.Client, resume, catalog []catalogItem) {
	known := make(map[string]map[string]string)
	for _, entry := range catalog {
		if entry.item.Kind == media.Series && len(entry.item.ExternalIDs) > 0 {
			known[entry.item.ID] = entry.item.ExternalIDs
		}
	}
	var missing []string
	for _, entry := range resume {
		item := entry.item
		if item.Kind != media.Episode || item.SeriesID == "" || item.Season <= 0 || item.Episode <= 0 {
			continue
		}
		if _, exists := known[item.SeriesID]; !exists && len(item.SeriesExternalIDs) > 0 {
			known[item.SeriesID] = item.SeriesExternalIDs
		}
		if _, exists := known[item.SeriesID]; !exists {
			known[item.SeriesID] = nil
			missing = append(missing, item.SeriesID)
		}
	}
	if len(missing) > 0 {
		lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		var workers sync.WaitGroup
		resolved := make([]map[string]string, len(missing))
		workerCount := min(4, len(missing))
		for start := range workerCount {
			workers.Go(func() {
				for i := start; i < len(missing) && lookup.Err() == nil; i += workerCount {
					if item, err := client.Detail(lookup, media.Item{ID: missing[i], Kind: media.Series}); err == nil && item.Kind == media.Series {
						resolved[i] = item.ExternalIDs
					}
				}
			})
		}
		workers.Wait()
		for i, id := range missing {
			if len(known[id]) == 0 {
				known[id] = resolved[i]
			}
		}
	}
	for i := range resume {
		item := &resume[i].item
		if item.Kind == media.Episode && len(item.SeriesExternalIDs) == 0 {
			item.SeriesExternalIDs = known[item.SeriesID]
		}
	}
}
