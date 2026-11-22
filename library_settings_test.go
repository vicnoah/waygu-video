package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestManageLibraryPreferences(t *testing.T) {
	// This is a pre-preferences configuration.
	const saved = `[{"ID":"library-fixture","Provider":"jellyfin","Address":"https://media.example.test","Name":"Family server",
		"Libraries":[{"ID":"movies","Name":"Movies","Kind":"movies"},{"ID":"shows","Name":"Shows","Kind":"tvshows"},
			{"ID":"private","Name":"Private library","Kind":"movies"}]}]`
	app := nsgotest.Start(t, libraryFixtureRoot, nsgotest.Size(480, 1100), nsgotest.Offscreen(),
		nsgotest.Storage(map[string]string{sourcesKey: saved}),
		nsgotest.Secrets(map[string]string{"library-fixture": "fixture-token"}))
	app.WaitFor("id:title-movies-title", time.Second)
	app.WaitFor("id:resume-movies-resume", time.Second)
	app.WaitFor("id:title-private-title", time.Second)
	pressLibraryTestTab(app, screenSettings, "Settings")
	phoneSettings := app.Exists("id:open-phone-source-settings")
	if phoneSettings {
		app.Press("id:open-phone-source-settings")
	}
	app.Press("id:manage-libraries-library-fixture")
	app.Press("id:library-up-shows")
	app.Press("id:library-category-shows")
	app.Press("id:library-category-shows-3")
	app.Press("id:library-home-movies")
	app.Press("id:library-visible-private")
	app.Press("id:refresh-libraries")
	app.WaitFor("id:library-visible-new", time.Second)
	app.Press("id:save-libraries")
	app.WaitFor("id:manage-libraries-library-fixture", time.Second)

	config, _ := app.Storage().GetItem(sourcesKey)
	var sources []source
	if err := json.Unmarshal([]byte(config), &sources); err != nil || len(sources) != 1 {
		t.Fatal("library preferences did not persist")
	}
	expected := []sourceLibrary{
		{Library: media.Library{ID: "shows", Name: "Shows", Kind: "tvshows"}, Category: libraryAnime},
		{Library: media.Library{ID: "movies", Name: "Movies", Kind: "movies"}, HideFromHome: true},
		{Library: media.Library{ID: "private", Name: "Private library", Kind: "movies"}, Hidden: true},
		{Library: media.Library{ID: "new", Name: "New library", Kind: "movies"}, Hidden: true},
	}
	if !reflect.DeepEqual(sources[0].Libraries, expected) {
		t.Fatalf("saved libraries: %+v", sources[0].Libraries)
	}
	app.Press("id:manage-libraries-library-fixture")
	app.Press("id:library-home-movies")
	app.Press("id:back")
	if unchanged, _ := app.Storage().GetItem(sourcesKey); unchanged != config {
		t.Fatal("canceling the editor saved the draft")
	}

	if phoneSettings {
		app.Press("id:back")
	}
	pressLibraryTestTab(app, screenHome, "Home")
	app.WaitFor("id:title-shows-title", time.Second)
	app.WaitFor("id:resume-shows-resume", time.Second)
	app.WaitForGone("id:title-movies-title", time.Second)
	app.WaitForGone("id:resume-movies-resume", time.Second)
	app.WaitForGone("id:title-private-title", time.Second)
	app.WaitForGone("id:resume-private-resume", time.Second)

	pressLibraryTestTab(app, screenLibrary, "Library")
	app.WaitFor("id:title-movies-title", time.Second)
	if app.Exists("id:library-category-anime") {
		app.Press("id:library-category-anime")
	} else {
		app.Press("id:catalog-category")
		app.Press("id:catalog-category-3")
	}
	app.WaitFor("id:title-shows-title", time.Second)
	app.WaitForGone("id:title-movies-title", time.Second)
	app.WaitForGone("id:title-new-title", time.Second)

	pressLibraryTestTab(app, screenSearch, "Search")
	app.Type("id:search-query", "fixture")
	app.Press("id:search-submit")
	app.WaitFor("id:title-movies-title", time.Second)
	app.WaitFor("id:title-shows-title", time.Second)
	app.WaitForGone("id:title-private-title", time.Second)
}

func TestImportLibraryVisibilityAndCategory(t *testing.T) {
	app := nsgotest.Start(t, libraryFixtureRoot, nsgotest.Size(480, 1100), nsgotest.Offscreen())
	app.Press("id:add-source")
	app.Press("id:choose-jellyfin")
	app.Type("id:server-address", "https://media.example.test")
	app.Type("id:server-username", "viewer")
	app.Type("id:server-password", "fixture-password")
	app.Press("id:sign-in")
	app.WaitFor("id:import-library-private", time.Second)
	app.Press("id:back")
	app.WaitFor("id:sign-in", time.Second)
	app.Type("id:server-password", "fixture-password")
	app.Press("id:sign-in")
	app.WaitFor("id:import-library-private", time.Second)
	movies := app.WaitFor("id:import-library-count-movies", time.Second)
	empty := app.WaitFor("id:import-library-count-private", time.Second)
	if movies.Text != "1,248 titles" || empty.Text != "0 titles" || app.Exists("id:import-library-count-new") {
		t.Fatalf("unexpected import counts for the populated, empty, and unavailable libraries")
	}
	app.Press("id:import-library-private")
	app.Press("id:import-library-new")
	app.Press("id:import-library-options-movies")
	app.Press("id:library-home-movies")
	app.Press("id:import-library-options-cancel")
	app.Press("id:import-library-options-movies")
	if !app.Find("id:library-home-movies").Props.Checked {
		t.Fatal("canceling options changed the import draft")
	}
	app.Press("id:library-home-movies")
	app.Press("id:import-library-options-apply")
	app.Press("id:import-library-options-shows")
	app.Press("id:library-category-shows")
	app.Press("id:library-category-shows-3")
	app.Press("id:import-library-options-apply")
	app.Press("id:import-libraries")
	app.WaitFor("id:title-shows-title", time.Second)
	app.WaitForGone("id:title-movies-title", time.Second)
	app.WaitForGone("id:title-private-title", time.Second)
	app.WaitForGone("id:title-new-title", time.Second)
	pressLibraryTestTab(app, screenLibrary, "Library")
	app.WaitFor("id:title-movies-title", time.Second)
	app.WaitForGone("id:title-private-title", time.Second)
	app.WaitForGone("id:title-new-title", time.Second)
}

func pressLibraryTestTab(app *nsgotest.App, page screen, title string) {
	for _, selector := range []string{"role:tab " + title, "id:tv-tab-" + string(page), "id:tab-" + string(page)} {
		if len(app.Query(selector)) > 0 {
			app.Press(selector)
			return
		}
	}
	app.Press("role:tab " + title)
}

func libraryFixtureRoot(cx *core.Scope) core.Node {
	return appRoot(cx, func(kind media.Provider, address string, _ media.Device, session media.Session) (media.Client, error) {
		if kind != media.Jellyfin || address != "https://media.example.test" {
			return nil, media.ErrInvalidAddress
		}
		return libraryFixtureServer{fixtureServer{session: session}}, nil
	}, nil)
}

type libraryFixtureServer struct{ fixtureServer }

func (f libraryFixtureServer) Libraries(context.Context) ([]media.Library, error) {
	if f.session.Token != "fixture-token" {
		return nil, media.ErrAuthentication
	}
	return []media.Library{
		{ID: "movies", Name: "Movies", Kind: "movies"}, {ID: "shows", Name: "Shows", Kind: "tvshows"},
		{ID: "private", Name: "Private library", Kind: "movies"}, {ID: "new", Name: "New library", Kind: "movies"},
	}, nil
}

func (f libraryFixtureServer) LibraryItemCount(_ context.Context, id string) (int, error) {
	if f.session.Token != "fixture-token" {
		return 0, media.ErrAuthentication
	}
	switch id {
	case "movies":
		return 1248, nil
	case "shows":
		return 326, nil
	case "private":
		return 0, nil
	default:
		return 0, media.ErrResponse
	}
}

func (f libraryFixtureServer) Catalog(ctx context.Context, q media.Query) (media.Page, error) {
	libraries, err := f.Libraries(ctx)
	if err != nil {
		return media.Page{}, err
	}
	var matches []media.Item
	for _, library := range libraries {
		if q.LibraryID == library.ID && (q.Search == "" || strings.EqualFold(q.Search, "fixture")) {
			kind := media.Movie
			if library.ID == "shows" {
				kind = media.Series
			}
			matches = append(matches, media.Item{ID: library.ID + "-title", Title: library.Name + " Fixture", Kind: kind})
		}
	}
	return media.Page{Items: matches, Total: len(matches)}, nil
}

func (f libraryFixtureServer) Resume(ctx context.Context) ([]media.Item, error) {
	libraries, err := f.Libraries(ctx)
	if err != nil {
		return nil, err
	}
	var items []media.Item
	for _, library := range libraries {
		items = append(items, media.Item{ID: library.ID + "-resume", Title: library.Name + " Resume", LibraryID: library.ID,
			Kind: media.Movie, Position: time.Minute})
	}
	return items, nil
}
