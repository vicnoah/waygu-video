package main

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"waygu-video/internal/media"
)

func (a *application) loadSeriesEpisodes(ctx context.Context, current route, tab string, seasons []media.Item) seriesEpisodePage {
	page := seriesEpisodePage{tab: tab}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := a.client(current.source)
	if err != nil {
		page.failure = err
		return page
	}
	seen := make(map[string]bool)
	add := func(items []media.Item, season int) {
		for _, item := range items {
			if !playable(item) || item.ID == "" || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			if item.Season == 0 {
				item.Season = season
			}
			page.items = append(page.items, inheritSeriesEpisode(item, current.item))
		}
	}
	for _, season := range seasons {
		if tab != season.ID && (tab != seriesSpecials || season.Season != 0) {
			continue
		}
		season.SeriesID = cmp.Or(season.SeriesID, current.item.ID)
		season.SeriesTitle = cmp.Or(season.SeriesTitle, current.item.Title)
		items, err := client.Children(ctx, season)
		add(items, season.Season)
		page.failure = errors.Join(page.failure, err)
	}
	if tab == seriesSpecials {
		if client, ok := client.(media.ExtrasClient); ok {
			items, err := client.Extras(ctx, current.item)
			add(items, 0)
			page.failure = errors.Join(page.failure, err)
		}
	}
	slices.SortStableFunc(page.items, func(a, b media.Item) int {
		if a.Kind == media.Episode && b.Kind != media.Episode {
			return -1
		}
		if b.Kind == media.Episode && a.Kind != media.Episode {
			return 1
		}
		return cmp.Compare(a.Episode, b.Episode)
	})
	return page
}

func inheritSeriesEpisode(item, series media.Item) media.Item {
	item.SeriesID = cmp.Or(item.SeriesID, series.ID)
	item.SeriesTitle = cmp.Or(item.SeriesTitle, series.Title)
	item.LibraryID = cmp.Or(item.LibraryID, series.LibraryID)
	if len(item.SeriesExternalIDs) == 0 {
		item.SeriesExternalIDs = series.ExternalIDs
	}
	return item
}

func latestSeriesBookmark(rows []catalogItem) catalogItem {
	var winner catalogItem
	for _, row := range rows {
		if row.item.Kind == media.Episode && canResume(row.item) &&
			(winner.item.ID == "" || winner.item.LastPlayed.Before(row.item.LastPlayed)) {
			winner = row
		}
	}
	return winner
}

func sameSeriesBookmark(a, b catalogItem) bool {
	return a.item.ID == b.item.ID && a.item.Position == b.item.Position && a.item.Duration == b.item.Duration &&
		a.item.Watched == b.item.Watched && a.item.Title == b.item.Title && a.item.Season == b.item.Season &&
		a.item.Episode == b.item.Episode && a.item.LastPlayed.Equal(b.item.LastPlayed) && a.pending == b.pending
}

type seriesVersion struct {
	itemID  string
	version media.Version
}

func (a *application) seriesVersion(ctx context.Context, source source, item media.Item, options media.PlaybackOptions) (seriesVersion, error) {
	result := seriesVersion{itemID: item.ID}
	client, err := a.client(source)
	if err != nil {
		return result, err
	}
	if provider, ok := client.(media.VersionClient); ok {
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		versions, err := provider.Versions(ctx, item)
		if err != nil {
			return result, err
		}
		result.version, _ = media.PreferredVersion(versions, options)
		if result.version.ID == "" && options.SourceID != "" {
			options.SourceID = ""
			result.version, _ = media.PreferredVersion(versions, options)
		}
	}
	return result, nil
}

func (a *application) openSeriesEpisode(view seriesViewState, entry catalogItem, resume, details bool) {
	if entry.item.ID == "" {
		return
	}
	next := route{screen: screenPlayer, source: entry.source, item: entry.item, resume: resume, resumeLocal: entry.pending}
	if details {
		next.screen = screenDetail
	}
	if saved, ok := a.seriesDownload(entry.source, entry.item, a.downloads.entries.GetUntracked()); ok && saved.State == downloadComplete {
		next.downloadID = saved.ID
	}
	stack := slices.Clone(a.routes.GetUntracked())
	stack[len(stack)-1].seriesView = view
	a.routes.Set(append(stack, next))
}

func (a *application) seriesDownload(source source, item media.Item, entries []downloadEntry) (downloadEntry, bool) {
	return a.downloads.find(downloadID(mediaAccountKey(source), item.ID), entries)
}
