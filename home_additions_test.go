package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestHomeAddedOnDayPages(t *testing.T) {
	day := calendarHomeDay(time.Date(2026, 11, 24, 17, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)))
	entries := []media.Item{{ID: "tomorrow", AddedAt: day.end}}
	for i := range 104 {
		entries = append(entries, media.Item{ID: fmt.Sprint(i), AddedAt: day.start})
	}
	entries = append(entries, media.Item{ID: "yesterday", AddedAt: day.start.Add(-time.Second)})
	var calls int
	list := func(_ context.Context, q media.Query) (media.Page, error) {
		calls++
		return media.Page{Items: entries[q.Offset:min(q.Offset+q.Limit, len(entries))], Total: len(entries)}, nil
	}
	page, err := homeAddedOnDay(t.Context(), list, media.Query{}, day, true)
	if err != nil || len(page.Items) != 104 || calls != 2 {
		t.Fatalf("daily titles = %d from %d pages, err = %v", len(page.Items), calls, err)
	}
	entries[0], entries[len(entries)-1] = entries[len(entries)-1], entries[0]
	page, err = homeAddedOnDay(t.Context(), list, media.Query{}, day, false)
	if err != nil || len(page.Items) != 104 {
		t.Fatalf("daily titles from an unordered catalog = %d, err = %v", len(page.Items), err)
	}
	entries[0].AddedAt = time.Time{}
	if _, err := homeAddedOnDay(t.Context(), list, media.Query{}, day, false); err != media.ErrResponse {
		t.Fatalf("missing timestamp error = %v, want %v", err, media.ErrResponse)
	}
}

func TestPhoneHomeDailyMetadata(t *testing.T) {
	now := time.Date(2026, 11, 24, 23, 59, 58, 0, time.FixedZone("UTC+8", 8*3600))
	item := media.Item{ID: "film", Title: "Fixture Film", Kind: media.Movie, Year: 2024,
		Genres: []string{"科幻", "冒险"}, Duration: 166 * time.Minute,
		Ratings: media.TitleRatings{IMDb: 8.5}, Rating: 8.5, ExternalIDs: map[string]string{"imdb": "tt1234567"},
		VideoProfiles: []media.VideoProfile{{Width: 3840, Height: 2160, Range: "DOVI"}},
		DolbyAtmos:    true, AddedAt: now.Add(-time.Hour),
	}
	movies := sourceLibrary{Library: media.Library{ID: "movies", Name: "Movies", Kind: "movies"}}
	hidden := sourceLibrary{Library: media.Library{ID: "hidden", Name: "Hidden", Kind: "movies"}, HideFromHome: true}
	first := source{ID: "emby", Name: "Fixture Emby", Address: "http://emby.example", Provider: media.Emby,
		Libraries: []sourceLibrary{movies, hidden}}
	second := source{ID: "plex", Name: "Fixture Plex", Address: "http://plex.example", Provider: media.Plex,
		Libraries: []sourceLibrary{movies}}
	copy := item
	copy.ID = "other-copy"
	old := media.Item{ID: "older", Title: "Yesterday", Kind: media.Movie, AddedAt: now.AddDate(0, 0, -1)}
	hiddenItem := media.Item{ID: "hidden-film", Title: "Hidden today", Kind: media.Movie, AddedAt: item.AddedAt}
	var favorite atomic.Bool
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return appRoot(cx, func(provider media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
			pages := map[string][]media.Item{"movies": {item, old}, "hidden": {hiddenItem}}
			if provider == media.Plex {
				pages = map[string][]media.Item{"movies": {copy}}
			}
			return desktopSidebarCatalog{fixtureServer{session: session}, pages, &favorite}, nil
		}, nil)
	}, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
		nsgotest.WallTime(now), nsgotest.Storage(map[string]string{sourcesKey: downloadFixtureJSON(t, []source{first, second})}),
		nsgotest.Secrets(map[string]string{"emby": "fixture-token", "plex": "fixture-token"}))
	app.WaitFor("text:2 个媒体源在线 · 今日新入库 1 部", 5*time.Second)
	if got := app.Find("id:phone-hero-facts").Text; got != "2024 · 166 分钟 · 科幻 / 冒险 · IMDb 8.5" {
		t.Fatalf("featured facts = %q", got)
	}
	for _, label := range []string{"4K", "Dolby Vision", "Atmos"} {
		app.Find("id:phone-hero-formats >> text:" + label)
	}
	if got := app.Find("id:poster-quality").Text; got != "Dolby Vision" {
		t.Errorf("poster format = %q", got)
	}
	app.FastForward(3 * time.Second)
	app.WaitFor("text:2 个媒体源在线 · 今日新入库 0 部", 5*time.Second)
}
