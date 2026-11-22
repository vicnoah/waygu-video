package main

import (
	"context"
	"slices"
	"testing"

	"waygu-video/internal/media"
)

func TestMergeCatalogExternalIDs(t *testing.T) {
	entries := []catalogItem{
		matchingFixture("emby", "1", media.Movie, "Localized title", " TT0100100 ", ""),
		matchingFixture("plex", "2", media.Movie, "Original title", "", "000123"),
		matchingFixture("jellyfin", "3", media.Movie, "Another title", "tt0100100", "123"),
		matchingFixture("flymoo", "4", media.Movie, "Localized title", "tt0200200", "790"),
		matchingFixture("plex", "5", media.Series, "Localized title", "tt0100100", "123"),
		matchingFixture("flymoo", "6", media.Movie, "Localized title", "", ""),
	}
	merged := mergeCatalogItems(entries, false, libraryMovies)
	if len(merged) != 4 || merged[0].item.ID != "1" || merged[0].match.ids.tmdb != "123" ||
		!slices.EqualFunc(merged[0].match.copies, entries[1:3], func(copy catalogCopy, entry catalogItem) bool {
			return copy.item.ID == entry.item.ID && copy.source.ID == entry.source.ID
		}) {
		t.Fatalf("expected one combined movie and three distinct entries: %+v", merged)
	}
	if copies := merged[0].match.copies; len(copies) != 2 || copies[0].source.ID != "plex" {
		t.Fatal("source precedence or alternatives changed")
	}

	conflicting := matchingFixture("other", "7", media.Movie, "Unrelated movie", "tt0100100", "999")
	merged = mergeCatalogItems(append(entries, conflicting), false, "")
	if len(merged) != 6 || len(merged[0].match.copies) != 0 || len(merged[1].match.copies) != 1 {
		t.Fatal("an ambiguous IMDb ID merged titles with conflicting TMDB IDs")
	}
}

func TestFindCrossSourceCopyBeyondFirstPage(t *testing.T) {
	anchor := matchingFixture("jellyfin", "anchor", media.Movie, "English title", "tt0100100", "123")
	matches := findLibraryCopies(t.Context(), matchingPageFixture{t: t}, "visible", anchor.item.Title, identityOf(anchor.item))
	if len(matches) != 1 || matches[0].ID != "localized" {
		t.Fatalf("wrong localized copy: %+v", matches)
	}
}

func matchingFixture(sourceID, id string, kind media.Kind, title, imdb, tmdb string) catalogItem {
	return catalogItem{source: source{ID: sourceID}, item: media.Item{ID: id, Kind: kind, Title: title,
		ExternalIDs: map[string]string{"IMDb": imdb, "tmdb": tmdb}}}
}

type matchingPageFixture struct {
	fixtureServer
	t *testing.T
}

func (f matchingPageFixture) Catalog(_ context.Context, query media.Query) (media.Page, error) {
	if query.LibraryID != "visible" {
		f.t.Fatalf("lookup left the imported library: %+v", query)
	}
	switch {
	case query.Search != "":
		unrelated := matchingFixture("plex", "same-name", media.Movie, query.Search, "tt0200200", "456")
		return media.Page{Items: []media.Item{unrelated.item}, Total: 1}, nil
	case query.Offset == 0:
		// Some servers return fewer results than the requested page size.
		return media.Page{Items: []media.Item{{ID: "placeholder", Kind: media.Movie, Title: "Other movie"}}, Total: 2}, nil
	case query.Offset == 1:
		localized := matchingFixture("plex", "localized", media.Movie, "本地化片名", "", "123")
		return media.Page{Items: []media.Item{localized.item}, Total: 2}, nil
	default:
		f.t.Fatalf("unexpected catalog page: %+v", query)
		return media.Page{}, nil
	}
}
