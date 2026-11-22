package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestContinueWatchingChoosesLatestCopy(t *testing.T) {
	start := time.Unix(1700000000, 0)
	older := matchingFixture("jellyfin", "older", media.Movie, "A movie", "tt0100100", "")
	newer := matchingFixture("emby", "newer", media.Movie, "Localized movie", "", "123")
	metadata := matchingFixture("plex", "bridge", media.Movie, "Another name", "tt0100100", "123")
	stale := matchingFixture("emby", "stale", media.Movie, "Finished movie", "tt0200200", "")
	done := matchingFixture("jellyfin", "done", media.Movie, "Finished movie", "tt0200200", "")
	undated := matchingFixture("flymoo", "undated", media.Movie, "A movie", "tt0300300", "")
	older.item.Position, older.item.LastPlayed = 40*time.Minute, start
	newer.item.Position, newer.item.LastPlayed = 3*time.Minute, start.Add(time.Minute)
	stale.item.Position, stale.item.LastPlayed = 20*time.Minute, start.Add(2*time.Minute)
	done.item.Watched, done.item.LastPlayed = true, start.Add(3*time.Minute)
	undated.item.Position = 50 * time.Minute
	rows := []catalogItem{older, undated, stale, newer}
	got := continueWatching(rows, []catalogItem{metadata, done}, true)
	if len(got) != 2 || got[0].source.ID != "emby" || got[0].item.ID != "newer" || got[0].item.Position != 3*time.Minute || got[1].item.ID != "undated" {
		t.Fatalf("unexpected merged resume rows: %+v", got)
	}
	unmerged := continueWatching(rows, []catalogItem{metadata, done}, false)
	if len(unmerged) != 4 || unmerged[0].item.ID != "stale" || unmerged[3].item.ID != "undated" || rows[0].item.ID != "older" {
		t.Fatalf("unexpected unmerged ordering: %+v", unmerged)
	}
}

func TestContinueWatchingMatchesEpisodeNumbersAndIDs(t *testing.T) {
	start := time.Unix(1700000000, 0)
	episode := func(sourceID, id string, number int, episodeIMDb string, series map[string]string, at time.Time) catalogItem {
		entry := matchingFixture(sourceID, id, media.Episode, "Episode", episodeIMDb, "")
		entry.item.Season, entry.item.Episode, entry.item.SeriesExternalIDs = 1, number, series
		entry.item.Position, entry.item.LastPlayed = 35*time.Second, at
		return entry
	}
	imdbSeries := map[string]string{"imdb": "tt0400400"}
	tmdbSeries := map[string]string{"tmdb": "444"}
	series := matchingFixture("plex", "series", media.Series, "Show", "tt0400400", "444")
	entries := []catalogItem{
		episode("jellyfin", "episode-older", 2, "tt0400402", imdbSeries, start),
		episode("emby", "episode-latest", 2, "", tmdbSeries, start.Add(time.Second)),
		episode("plex", "episode-unlinked", 2, "tt0400402", nil, start),
		episode("jellyfin", "different-episode", 3, "", imdbSeries, start.Add(-time.Second)),
	}
	got := continueWatching(entries, []catalogItem{series}, true)
	if len(got) != 2 || got[0].item.ID != "episode-latest" || got[1].item.ID != "different-episode" {
		t.Fatalf("incorrect episode grouping: %+v", got)
	}
	entries = append(entries, episode("flymoo", "conflicting-episode", 2, "tt0500502", tmdbSeries, start))
	got = continueWatching(entries, []catalogItem{series}, true)
	if len(got) != 4 {
		t.Fatalf("conflicting episode IDs were combined by numbering: %+v", got)
	}
}

func TestContinueWatchingLooksUpSeriesIdentityOnce(t *testing.T) {
	var calls atomic.Int32
	client := resumeSeriesFixture{calls: &calls}
	episodes := []catalogItem{
		{item: media.Item{ID: "ep-1", Kind: media.Episode, SeriesID: "series", Season: 1, Episode: 1}},
		{item: media.Item{ID: "ep-2", Kind: media.Episode, SeriesID: "series", Season: 1, Episode: 2}},
	}
	enrichResumeSeries(t.Context(), client, episodes, nil)
	if calls.Load() != 1 || episodes[0].item.SeriesExternalIDs["tmdb"] != "444" || episodes[1].item.SeriesExternalIDs["tmdb"] != "444" {
		t.Fatalf("missing parent identity for the two episodes: %+v, requests=%d", episodes, calls.Load())
	}
}

type resumeSeriesFixture struct {
	fixtureServer
	calls *atomic.Int32
}

func (f resumeSeriesFixture) Detail(_ context.Context, item media.Item) (media.Item, error) {
	f.calls.Add(1)
	if item.ID != "series" || item.Kind != media.Series {
		return media.Item{}, media.ErrResponse
	}
	item.ExternalIDs = map[string]string{"tmdb": "444"}
	return item, nil
}

func TestContinueWatchingUsesSafeCachedSnapshot(t *testing.T) {
	a := source{ID: "cache", Provider: media.Plex, Address: "https://cache.example.test", Name: "Cache fixture",
		Libraries: []sourceLibrary{{Library: media.Library{ID: "movies"}}, {Library: media.Library{ID: "private"}}}}
	serverMode := &atomic.Int32{}
	root := func(cx *core.Scope) core.Node {
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return resumeCacheFixture{fixtureServer{session: session}, serverMode}, nil
		}, nil)
	}
	start := func(entry source, cache string) *nsgotest.App {
		t.Helper()
		encoded, err := json.Marshal([]source{entry})
		if err != nil {
			t.Fatal(err)
		}
		return nsgotest.Start(t, root, nsgotest.Size(920, 850), nsgotest.Offscreen(),
			nsgotest.Storage(map[string]string{sourcesKey: string(encoded), resumeCacheKey(entry): cache}),
			nsgotest.Secrets(map[string]string{"cache": "sensitive-fixture-token"}))
	}
	first := start(a, "")
	first.WaitFor("id:resume-thriller", time.Second)
	first.WaitFor("id:resume-thriller-progress", time.Second)
	first.WaitUntil(time.Second, func() bool {
		value, _ := first.Storage().GetItem(resumeCacheKey(a))
		return strings.Contains(value, "thriller") && strings.Contains(value, "private-title")
	})
	cache, _ := first.Storage().GetItem(resumeCacheKey(a))
	if strings.Contains(cache, "sensitive-fixture-token") {
		t.Fatal("the offline snapshot retained credentials in an artwork URL")
	}

	serverMode.Store(1)
	a.Libraries[1].HideFromHome = true
	second := start(a, cache)
	second.WaitFor("id:resume-thriller", time.Second)
	second.WaitForGone("id:resume-private-title", time.Second)

	serverMode.Store(2)
	second.Press("role:button Retry")
	second.WaitForGone("id:resume-thriller", time.Second)
	second.WaitUntil(time.Second, func() bool {
		_, exists := second.Storage().GetItem(resumeCacheKey(a))
		return !exists
	})
}

type resumeCacheFixture struct {
	fixtureServer
	mode *atomic.Int32
}

func (f resumeCacheFixture) Catalog(context.Context, media.Query) (media.Page, error) {
	return media.Page{}, nil
}

func (f resumeCacheFixture) Resume(context.Context) ([]media.Item, error) {
	if f.mode.Load() == 1 {
		return nil, errors.New("fixture server is offline")
	}
	if f.mode.Load() == 2 {
		return nil, nil
	}
	return []media.Item{
		{ID: "thriller", Kind: media.Movie, Title: "Thriller", LibraryID: "movies", Position: 25 * time.Second,
			Duration: time.Minute, Poster: "https://cache.example.test/image?api_key=sensitive-fixture-token"},
		{ID: "private-title", Kind: media.Movie, Title: "Hidden record", LibraryID: "private", Position: 15 * time.Second},
	}, nil
}

func TestResumeShelfRetainsSuccessfulRowsFromPartialFlymooResponse(t *testing.T) {
	library := sourceLibrary{Library: media.Library{ID: "movies"}}
	entry := source{ID: "flymoo", Libraries: []sourceLibrary{library}}
	known := media.Item{ID: "remote", Kind: media.Movie, LibraryID: library.ID, Position: 8 * time.Second}
	cached := media.Item{ID: "cached", Kind: media.Movie, LibraryID: library.ID, Position: 6 * time.Second}
	shelf, refreshed, err := resumeShelf(t.Context(), partialResumeFixture{item: known}, entry, entry.Libraries, []media.Item{cached})
	if !refreshed || err == nil || !slices.EqualFunc(shelf, []media.Item{known, cached}, func(entry catalogItem, item media.Item) bool {
		return entry.item.ID == item.ID
	}) {
		t.Fatalf("partial response discarded a confirmed record: %+v", shelf)
	}
}

type partialResumeFixture struct {
	fixtureServer
	item media.Item
}

func (f partialResumeFixture) Resume(context.Context) ([]media.Item, error) {
	return []media.Item{f.item}, errors.New("one play/info request failed")
}
