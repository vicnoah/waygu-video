package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDesktopSidebarCounts(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" && target != "macos" && target != "windows" {
		t.Skip("requires a desktop")
	}
	namedLibrary := func(id, kind string) sourceLibrary {
		return sourceLibrary{Library: media.Library{ID: id, Name: id, Kind: kind}}
	}
	mixed := namedLibrary("mixed", "")
	anime := namedLibrary("anime", "tvshows")
	anime.Category = libraryAnime
	docs := namedLibrary("docs", "movies")
	docs.Category, docs.HideFromHome = libraryDocumentaries, true
	hidden := namedLibrary("hidden", "movies")
	hidden.Hidden = true
	mirror := namedLibrary("mirror", "movies")
	sources := []source{
		{ID: "local", Provider: media.Emby, Name: "Local cinema", Address: "http://local.example.test",
			Libraries: []sourceLibrary{mixed, anime, docs, hidden}},
		{ID: "mirror", Provider: media.Jellyfin, Name: "Second library", Address: "http://mirror.example.test",
			Libraries: []sourceLibrary{mirror}},
	}
	matching := media.Item{ID: "shared", Title: "Mirror Film", Kind: media.Movie, ExternalIDs: map[string]string{"Tmdb": "101"},
		AddedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), DolbyAtmos: true,
		VideoProfiles: []media.VideoProfile{{Width: 3840, Height: 2160, Range: "DOVI"}}}
	first := []media.Item{matching, {ID: "series", Title: "The Railway", Kind: media.Series}}
	for i := range 101 {
		first = append(first, media.Item{ID: "film-" + strconv.Itoa(i), Title: "Film " + strconv.Itoa(i), Kind: media.Movie})
	}
	pages := map[string][]media.Item{
		"mixed":  first,
		"anime":  {{ID: "animation", Title: "Animation", Kind: media.Series}},
		"docs":   {matching},
		"hidden": {{ID: "private", Title: "Hidden Film", Kind: media.Movie}},
		"mirror": {matching, {ID: "exclusive", Title: "Second Library Film", Kind: media.Movie}},
	}
	saved := downloadFixtureJSON(t, sources)
	height := core.Points(900)
	if nsgotest.Target() == "linux" {
		height += 37
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomDesktop)
		return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			return desktopSidebarCatalog{fixtureServer{session: session}, pages}, nil
		}, nil)
	}, nsgotest.Size(1440, height), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
		nsgotest.Storage(map[string]string{sourcesKey: saved, appearanceKey: "dark"}),
		nsgotest.Secrets(map[string]string{"local": "fixture-token", "mirror": "fixture-token"}))

	for _, check := range []struct {
		category libraryCategory
		count    string
	}{{libraryMovies, "103"}, {librarySeries, "1"}, {libraryAnime, "1"}, {libraryDocumentaries, "1"}} {
		if got := app.WaitFor("id:desktop-category-count-"+string(check.category), 8*time.Second).Text; got != check.count {
			t.Fatalf("%s sidebar count = %q, want %q", check.category, got, check.count)
		}
	}
	app.WaitFor("id:home-hero-atmos", 5*time.Second)
	if nsgotest.Target() != "" {
		app.Screenshot("desktop-category-counts")
	}
	app.Press("id:home-hero-dot-1")
	app.WaitForGone("id:home-hero-atmos", time.Second)
	app.Press("id:tab-library")
	app.WaitFor("id:desktop-library-summary", 5*time.Second)
	app.WaitUntil(8*time.Second, func() bool {
		return strings.HasPrefix(app.Find("id:desktop-library-summary").Text, "103 部 ·")
	})
	app.Press("id:desktop-library-documentaries")
	app.WaitUntil(8*time.Second, func() bool {
		return app.Find("id:desktop-library-title").Text == "纪录片" && app.Exists("id:title-local-shared") &&
			strings.HasPrefix(app.Find("id:desktop-library-summary").Text, "1 部 ·")
	})
}

type desktopSidebarCatalog struct {
	fixtureServer
	pages map[string][]media.Item
}

func (f desktopSidebarCatalog) Catalog(_ context.Context, query media.Query) (media.Page, error) {
	items := f.pages[query.LibraryID]
	start := min(query.Offset, len(items))
	end := len(items)
	if query.Limit > 0 {
		end = min(start+query.Limit, end)
	}
	return media.Page{Items: items[start:end], Total: len(items)}, nil
}
