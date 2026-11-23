package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerLibraryFormats(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			client, wanted := reviewFixtureItem(t, config, secrets, provider, "Waygu Version Test")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			libraries, err := client.Libraries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, library := range libraries {
				if library.Name != "Movies" {
					continue
				}
				page, err := client.Catalog(ctx, media.Query{LibraryID: library.ID, Limit: 24, Search: "Waygu Version Test", CatalogFacts: true})
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range page.Items {
					if item.ID != wanted.ID {
						continue
					}
					hasHeight := func(height int) bool {
						return slices.ContainsFunc(item.VideoProfiles, func(profile media.VideoProfile) bool { return profile.Height == height })
					}
					if !hasHeight(360) || (provider != "emby" && !hasHeight(180)) {
						t.Fatalf("catalog metadata did not include the indexed formats: %+v", item.VideoProfiles)
					}
					return
				}
			}
			t.Fatal("version fixture is missing from the catalog")
		})
	}
}

func TestLibraryFilterFindsTitlesBeyondFirstTwoHundred(t *testing.T) {
	page, err := scanCatalogPages(t.Context(), pagedLibraryFixture{}.Catalog, media.Query{LibraryID: "movies", CatalogFacts: true})
	if err != nil || page.Total != 230 || len(page.Items) != 230 {
		t.Fatalf("catalog scan loaded %d/%d items: %v", len(page.Items), page.Total, err)
	}
	items := make([]catalogItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, catalogItem{item: item})
	}
	matches := filterPhoneLibrary(items, phoneLibraryState{year: 1995, resolution: 2160, genre: "drama"})
	if len(matches) != 1 || matches[0].item.ID != "229" {
		t.Fatalf("filter matched %d titles, want the one on the last page", len(matches))
	}
}

type pagedLibraryFixture struct{ media.Client }

func (pagedLibraryFixture) Catalog(_ context.Context, query media.Query) (media.Page, error) {
	page := media.Page{Total: 230}
	for offset := query.Offset; offset < min(query.Offset+min(query.Limit, 50), page.Total); offset++ {
		item := media.Item{ID: fmt.Sprint(offset), Kind: media.Movie, Title: fmt.Sprintf("Title %d", offset), Year: 2025}
		if offset == page.Total-1 {
			item.Year, item.Genres = 1995, []string{"Drama"}
			item.VideoProfiles = []media.VideoProfile{{Width: 3840, Height: 1600}}
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

func TestDockerPhoneLibraryReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := dockerReviewSources(t, false)
	client, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	doc := reviewFixtureTitle(t, config, secrets, "jellyfin", "Waygu Track Test")
	_, _, show, _ := reviewSeriesFixture(t, config, secrets, "emby")
	_, _, anime, _ := reviewSeriesFixture(t, config, secrets, "jellyfin")
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	for i := range sources {
		if sources[i].ID != "jellyfin" {
			continue
		}
		for j, library := range sources[i].Libraries {
			switch library.Name {
			case "Shows":
				sources[i].Libraries[j].Category = libraryAnime
			case "Movies":
				sources[i].Libraries[j].Category = libraryDocumentaries
			}
		}
	}
	payload, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(payload)
	flag := func(state bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := client.(media.WatchedClient).SetWatched(ctx, movie, state); err != nil {
			t.Fatal(err)
		}
	}
	flag(true)
	t.Cleanup(func() { flag(movie.Watched) })
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			height := core.Points(1100)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				shellIdiom.Provide(cx, core.IdiomPhone)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.Press("id:tab-library")
			trackCard := "id:title-emby-" + movie.ID
			app.WaitFor(trackCard, 8*time.Second)
			if size := app.Find("id:library-poster-emby-" + movie.ID).Frame.Size; size.Width != 112 || size.Height != 160 {
				t.Fatalf("poster size = %+v, want 112 × 160", size)
			}
			if quality := app.Find("id:library-quality-emby-" + movie.ID).Text; quality != "180p" {
				t.Fatalf("track fixture format = %q, want 180p", quality)
			}
			capture := func(suffix string) {
				if nsgotest.Target() != "" {
					app.Advance(700 * time.Millisecond)
					app.Screenshot("library-" + variant.name + suffix)
				}
			}
			capture("")
			pick := func(control, label string) {
				t.Helper()
				app.Press("id:" + control)
				option := "role:menuitem " + label
				app.WaitFor(option, 2*time.Second)
				app.Press(option)
				app.Advance(350 * time.Millisecond)
			}
			pick("library-resolution", "180p")
			app.WaitFor(trackCard, 5*time.Second)
			if found := app.Find("id:library-quality-emby-" + movie.ID).Text; found != "180p" {
				t.Fatalf("filtered card resolution = %q", found)
			}
			pick("library-genre", "Test Clip")
			app.WaitFor(trackCard, 2*time.Second)
			pick("library-year", "2025")
			app.WaitFor(trackCard, 2*time.Second)
			app.Press("id:library-unwatched")
			app.WaitForGone(trackCard, 2*time.Second)
			app.Press("id:library-unwatched")
			app.WaitFor(trackCard, 2*time.Second)
			pick("library-sort", "片名")
			app.Press("id:library-layout")
			app.WaitFor("id:library-list", time.Second)
			app.Press(trackCard)
			app.WaitFor("id:phone-movie-hero", 5*time.Second)
			app.Press("id:back")
			app.WaitFor("id:library-list", 5*time.Second)
			app.WaitFor(trackCard, 2*time.Second)
			capture("-list")
			app.Press("id:library-layout")
			app.WaitFor("id:library-grid", time.Second)
			openFilters := func() {
				t.Helper()
				app.Press("id:library-filter")
				app.WaitFor("id:library-filter-dialog", time.Second)
				app.Advance(400 * time.Millisecond)
			}
			closeFilters := func() {
				t.Helper()
				app.Press("id:library-filter-close")
				app.Advance(400 * time.Millisecond)
				app.WaitForGone("id:library-filter-dialog", 2*time.Second)
			}
			openFilters()
			app.Press("id:library-clear-filters")
			app.Press("id:library-source")
			app.Press("id:library-source-1")
			closeFilters()
			app.WaitUntil(5*time.Second, func() bool {
				return app.Exists("id:library-summary") && strings.Contains(app.Find("id:library-summary").Text, "来自 1 个媒体源")
			})
			app.WaitFor(trackCard, 2*time.Second)
			openFilters()
			app.Press("id:catalog-library")
			app.Press("id:catalog-library-1")
			closeFilters()
			app.WaitFor(trackCard, 2*time.Second)
			openFilters()
			app.Press("id:library-clear-filters")
			closeFilters()
			for _, section := range []struct {
				category             libraryCategory
				id, sourceID, suffix string
			}{
				{librarySeries, show.ID, "emby", "-shows"},
				{libraryAnime, anime.ID, "jellyfin", "-anime"},
				{libraryDocumentaries, doc, "jellyfin", "-docs"},
			} {
				tab := "id:library-category-" + string(section.category)
				app.Press(tab)
				card := fmt.Sprintf("id:title-%s-%s", section.sourceID, section.id)
				app.WaitFor(card, 5*time.Second)
				if !app.Find(tab).Props.Selected {
					t.Fatalf("the %s category did not become selected", section.category)
				}
				app.WaitForGone(trackCard, 2*time.Second)
				capture(section.suffix)
			}
			openFilters()
			app.Press("id:library-source")
			app.Press("id:library-source-1")
			closeFilters()
			app.WaitFor("id:library-empty", 5*time.Second)
			capture("-empty")
		})
	}
}
