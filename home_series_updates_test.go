package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

type updateFixture struct {
	fixtureServer
	shows    []media.Item
	episodes []media.Item
}

func (f updateFixture) Catalog(context.Context, media.Query) (media.Page, error) {
	return media.Page{Items: f.shows, Total: len(f.shows)}, nil
}

func (f updateFixture) RecentEpisodes(_ context.Context, q media.Query) (media.Page, error) {
	first := min(q.Offset, len(f.episodes))
	// Leave tied batch imports on separate pages.
	return media.Page{Items: f.episodes[first:min(first+2, len(f.episodes))], Total: len(f.episodes)}, nil
}

func (f updateFixture) Detail(_ context.Context, query media.Item) (media.Item, error) {
	for _, show := range f.shows {
		if show.ID == query.ID {
			return show, nil
		}
	}
	return media.Item{}, media.ErrResponse
}

func TestHomeSeriesUpdates(t *testing.T) {
	now := time.Date(2026, 11, 25, 13, 0, 0, 0, time.UTC)
	library := sourceLibrary{Library: media.Library{ID: "tv", Kind: "tvshows"}}
	emby := source{ID: "emby", Name: "Emby", Provider: media.Emby, Address: "http://emby.test",
		Libraries: []sourceLibrary{library}}
	plex := source{ID: "plex", Name: "Plex", Provider: media.Plex, Address: "http://plex.test",
		Libraries: []sourceLibrary{library}}
	show := media.Item{ID: "show-old", Kind: media.Series, Title: "Older Series", AddedAt: now.AddDate(-1, 0, 0),
		ExternalIDs: map[string]string{"imdb": "tt8020150"}}
	copy := show
	copy.ID, copy.Title = "show-copy", "Updated Series"
	second := media.Item{ID: "show-recent", Kind: media.Series, Title: "Newer Series Folder", AddedAt: now.Add(-time.Hour)}
	episode := func(parent string, number int, added time.Time) media.Item {
		return media.Item{ID: parent + added.Format(time.RFC3339) + strconv.Itoa(number), Kind: media.Episode,
			SeriesID: parent, Season: 2, Episode: number, AddedAt: added}
	}
	today := now.Add(-30 * time.Minute)
	older := now.AddDate(0, 0, -2)
	embyFixture := updateFixture{shows: []media.Item{second, show},
		episodes: []media.Item{episode(show.ID, 2, older), episode(second.ID, 1, older.Add(-time.Hour))}}
	plexFixture := updateFixture{shows: []media.Item{copy}, episodes: []media.Item{
		episode(copy.ID, 3, today), episode(copy.ID, 4, today), episode(copy.ID, 5, today)}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return appRoot(cx, func(provider media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			fixture := embyFixture
			if provider == media.Plex {
				fixture = plexFixture
			}
			fixture.fixtureServer.session = session
			return fixture, nil
		}, nil)
	}, nsgotest.Size(390, 1200), nsgotest.WallTime(now), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.Storage(map[string]string{sourcesKey: downloadFixtureJSON(t, []source{emby, plex})}),
		nsgotest.Secrets(map[string]string{"emby": "fixture-token", "plex": "fixture-token"}))
	app.WaitFor("id:home-series-rail >> text:第 5 集 · 今天", 5*time.Second)
	rows := app.Query("id:home-series-rail >> id:series-update-caption")
	if len(rows) != 2 || rows[0].Text != "第 5 集 · 今天" || rows[1].Text != "第 2 季 · 第 1 集" {
		t.Fatalf("wrong series order or duplicate copy: %v", rows)
	}
	app.Find("id:title-plex-show-copy >> id:poster-new")
	app.Press("id:title-plex-show-copy")
	if title := app.WaitFor("id:phone-series-title", 5*time.Second).Text; title != copy.Title {
		t.Fatalf("series update opened %q instead of %q", title, copy.Title)
	}
}

func TestDockerRecentEpisodes(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the three Docker media servers")
	}
	config, secrets := dockerReviewSources(t, false)
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range sources {
		t.Run(string(endpoint.Provider), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			client, err := media.NewClient(endpoint.Provider, endpoint.Address, media.NewDevice("Series Updates Test"),
				media.Session{UserID: endpoint.AccountID, Token: secrets[endpoint.ID]})
			if err != nil {
				t.Fatal(err)
			}
			library := slices.IndexFunc(endpoint.Libraries, func(library sourceLibrary) bool { return library.Name == "Shows" })
			if library < 0 {
				t.Fatal("missing Shows fixture library")
			}
			id := endpoint.Libraries[library].ID
			reader := client.(media.EpisodeCatalogClient)
			page, err := reader.RecentEpisodes(ctx, media.Query{LibraryID: id, Limit: 1, CatalogFacts: true})
			if err != nil || len(page.Items) != 1 || page.Total < 2 {
				t.Fatalf("first episode page = %v / %d: %v", len(page.Items), page.Total, err)
			}
			latest := page.Items[0]
			next, err := reader.RecentEpisodes(ctx, media.Query{LibraryID: id, Offset: 1, Limit: 1})
			if err != nil || len(next.Items) != 1 || next.Items[0].ID == latest.ID || next.Items[0].AddedAt.After(latest.AddedAt) {
				t.Fatalf("episode page did not advance by addition time: %v", err)
			}
			if latest.Kind != media.Episode || latest.SeriesID == "" || latest.AddedAt.IsZero() {
				t.Fatal("episode response lacks its series ID or addition time")
			}
			var results seriesUpdates
			results.endpoint, results.index = endpoint, make(map[string]int)
			results.readRecent(ctx, client, reader, id, 36)
			if len(results.items) < 2 {
				t.Fatalf("library returned %d distinct series updates", len(results.items))
			}
			for _, item := range results.items {
				if item.item.Title == "" || item.item.ID != item.latest.SeriesID || item.item.LibraryID != id {
					t.Fatal("series update lost the authenticated parent entry")
				}
			}
		})
	}
}

func TestFlymooHomeSeriesUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response string
		switch r.URL.Path {
		case "/api/v1/item/list":
			var request struct {
				Library string `json:"ancestor_guid"`
			}
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&request) != nil || request.Library != "anime" {
				t.Error("Flymoo updates must use the imported library")
			}
			response = `{"total":1,"list":[{"guid":"show","title":"Flymoo Series","type":"TV","mdb_guid":"anime"}]}`
		case "/api/v1/season/list/show":
			response = `[{"guid":"season","season_number":"2"}]`
		case "/api/v1/episode/list/season":
			response = `[{"guid":"first","episode_number":"1","create_time":"2026-11-22T12:00:00Z"},{"guid":"second","episode_number":"2","create_time":"2026-11-24T12:00:00Z"}]`
		default:
			t.Errorf("unexpected Flymoo route %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	client, err := media.NewClient(media.Flymoo, server.URL, media.NewDevice("Series Updates Test"), media.Session{Token: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	results := seriesUpdates{index: make(map[string]int)}
	results.readFlymoo(t.Context(), client, "anime")
	if len(results.items) != 1 || results.items[0].latest.ID != "second" || results.items[0].latest.Season != 2 ||
		results.items[0].latest.Episode != 2 || results.items[0].item.LibraryID != "anime" {
		t.Fatalf("unexpected Flymoo series updates: %+v", results.items)
	}
}

func TestHomeSeriesUpdatesCustomizedLayouts(t *testing.T) {
	endpoint := source{ID: "library-fixture", Name: "Series fixture", Address: "https://media.example.test", Provider: media.Jellyfin,
		Libraries: []sourceLibrary{{Library: media.Library{ID: "shows", Name: "Shows", Kind: "tvshows"}}}}
	config := map[string]string{sourcesKey: downloadFixtureJSON(t, []source{endpoint}),
		appearancePreferencesKey: `{"HomeOrdered":true}`}
	for _, variant := range []struct {
		name     string
		idiom    core.Idiom
		width    core.Points
		height   core.Points
		selector string
	}{
		{"tablet", core.IdiomTablet, 1180, 820, "id:home-series-rail"},
		{"tv", core.IdiomTV, 1920, 1080, "id:tv-series"},
	} {
		t.Run(variant.name, func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, variant.idiom)
				return libraryFixtureRoot(cx)
			}, nsgotest.Size(variant.width, variant.height), nsgotest.Idiom(variant.idiom), nsgotest.Storage(config),
				nsgotest.Secrets(map[string]string{endpoint.ID: "fixture-token"}))
			app.WaitFor(variant.selector+" >> text:Season 1 · Episode 1", 5*time.Second)
		})
	}
}
