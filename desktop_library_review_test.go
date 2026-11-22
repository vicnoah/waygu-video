package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDesktopLibraryFiltersAndIndex(t *testing.T) {
	left := catalogItem{item: media.Item{ID: "a", Title: "《阿飞正传》", Year: 1990, Genres: []string{"Drama"},
		FileSize: 50, VideoProfiles: []media.VideoProfile{{Width: 3840, Height: 2160}, {Width: 1920, Height: 1080, Range: "HDR10"}}}}
	right := catalogItem{item: media.Item{ID: "b", Title: "Été", Year: 2024, Genres: []string{"Drama"}, FileSize: 80,
		VideoProfiles: []media.VideoProfile{{Width: 3840, Height: 2160, Range: "HDR10"}}}}
	if got := desktopTitleKey(left.item.Title); got != "afeizhengchuan" {
		t.Fatalf("Mandarin index key = %q", got)
	}
	if got := desktopTitleKey(right.item.Title); got != "ete" {
		t.Fatalf("French index key = %q", got)
	}
	if got := desktopTitleLetter(desktopTitleKey("《1917》")); got != "#" {
		t.Fatalf("numeric index = %q", got)
	}
	state := desktopLibraryState{filters: desktopLibraryFilters{Genre: "drama", Resolution: 2160, HDR: true}, sort: libraryFileSize}
	if rows := desktopLibraryRows(desktopLibraryEntries([]catalogItem{left, right}), state.filters, state.sort); len(rows) != 1 || rows[0].entry.item.ID != "b" {
		t.Fatalf("stacked UHD and HDR filters matched %+v", rows)
	}
	state.filters = desktopLibraryFilters{}
	if rows := desktopLibraryRows(desktopLibraryEntries([]catalogItem{left, right}), state.filters, state.sort); len(rows) != 2 || rows[0].entry.item.ID != "b" {
		t.Fatalf("file size order = %+v", rows)
	}
}

func TestDockerDesktopLibraryReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("desktop design capture runs on Linux GTK")
	}
	config, secrets := homeReviewSources(t)
	_, fixture := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomDesktop)
				cx.SetSubtreeAppearance(theme.appearance)
				return Root(cx)
			}, nsgotest.Size(1440, 1500), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.Press("id:tab-library")
			summary := func() string { return app.Find("id:desktop-library-summary").Text }
			allSources := func() bool { return strings.Contains(summary(), "来自 3 个媒体源") }
			app.WaitUntil(10*time.Second, func() bool { return allSources() || app.Exists("id:desktop-library-retry") })
			if !allSources() {
				app.Press("id:desktop-library-retry")
				app.WaitUntil(10*time.Second, allSources)
			}
			if !strings.Contains(summary(), "共") && !strings.Contains(summary(), "已知") {
				t.Fatalf("missing file sizes from catalog: %s", summary())
			}
			card := "id:title-emby-" + fixture.ID
			app.WaitFor(card, 4*time.Second)
			poster := app.Find("id:library-poster-emby-" + fixture.ID).Frame.Size
			if poster.Width != 144 || poster.Height != 208 {
				t.Fatalf("poster is %+v, want 144 × 208", poster)
			}
			capture := func(suffix string) {
				if nsgotest.Target() != "" {
					app.Advance(750 * time.Millisecond)
					app.Screenshot("app-desktop-library-" + theme.name + suffix)
				}
			}
			capture("")
			if theme.name != "light" {
				return
			}
			app.Press("id:desktop-filter-hdr")
			app.WaitForGone(card, 4*time.Second)
			app.Press("id:desktop-filter-hdr")
			app.WaitFor(card, 4*time.Second)
			pick := func(id, label string) {
				t.Helper()
				app.Press("id:" + id)
				option := "role:menuitem " + label
				app.WaitFor(option, 3*time.Second)
				app.Press(option)
				app.Advance(350 * time.Millisecond)
			}
			pick("desktop-filter-source", "Emby 影视库")
			app.WaitUntil(8*time.Second, func() bool { return strings.Contains(summary(), "来自 1 个媒体源") })
			pick("desktop-filter-resolution", "180p")
			app.WaitFor(card, 5*time.Second)
			app.WaitFor("id:desktop-filter-remove-resolution", 2*time.Second)
			pick("desktop-library-sort", "文件大小")
			app.Press("id:desktop-library-mode-list")
			app.WaitFor("id:desktop-library-list", 3*time.Second)
			app.WaitFor(card, 3*time.Second)
			capture("-list")
			app.Press(card)
			app.WaitForGone("id:desktop-library", 5*time.Second)
			app.Press("id:back")
			app.WaitFor("id:desktop-library-list", 5*time.Second)
			app.WaitFor(card, 5*time.Second)
			app.WaitFor("id:desktop-filter-remove-resolution", 2*time.Second)
			app.Press("id:desktop-library-mode-wall")
			app.WaitFor("id:desktop-library-wall", 3*time.Second)
			app.WaitFor(card, 3*time.Second)
			capture("-wall")
			app.Press("id:desktop-library-filters-open")
			app.WaitFor("id:desktop-collection-name", 3*time.Second)
			app.Type("id:desktop-collection-name", "Emby SD")
			app.Press("id:desktop-save-collection")
			app.Advance(400 * time.Millisecond)
			app.WaitForGone("id:desktop-filter-dialog", 3*time.Second)
			stored := readDesktopCollections(app.Storage())
			if len(stored) != 1 || stored[0].Name != "Emby SD" || stored[0].Filters.Source != "emby" ||
				stored[0].Filters.Resolution != 180 || stored[0].Sort != libraryFileSize {
				t.Fatalf("saved collection = %+v", stored)
			}
			collection := "id:desktop-collection-" + stored[0].ID
			app.Press("id:tab-library")
			app.WaitForGone("id:desktop-filter-remove-resolution", 3*time.Second)
			app.Press(collection)
			app.WaitFor("id:desktop-filter-remove-resolution", 3*time.Second)
			app.WaitFor(card, 5*time.Second)
			if got := app.Find("id:desktop-library-title").Text; got != "Emby SD" {
				t.Fatalf("active collection title = %q", got)
			}
			app.Press("id:desktop-index-W")
			app.WaitUntil(3*time.Second, func() bool { return app.Find("id:desktop-index-W").Props.Selected })
			app.Press("id:desktop-library-filters-open")
			app.WaitFor("id:desktop-delete-collection", 3*time.Second)
			app.Press("id:desktop-delete-collection")
			app.Advance(400 * time.Millisecond)
			app.WaitForGone(collection, 3*time.Second)
			if stored := readDesktopCollections(app.Storage()); len(stored) != 0 {
				t.Fatalf("deleted collection still persisted: %+v", stored)
			}
			app.Press("id:desktop-library-shuffle")
			app.WaitFor("id:video-player", 8*time.Second)
			app.Press("id:close-player")
		})
	}
}

func TestDesktopLibraryCollectionRestoresFromStorage(t *testing.T) {
	collection := desktopCollection{ID: "saved", Name: "Test", Category: libraryMovies, Filters: desktopLibraryFilters{HDR: true}}
	saved, err := json.Marshal([]desktopCollection{collection})
	if err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomDesktop)
		return Root(cx)
	}, nsgotest.Size(1440, 900), nsgotest.Storage(map[string]string{seenKey: "true", desktopCollectionsKey: string(saved)}))
	app.Press(fmt.Sprintf("id:desktop-collection-%s", collection.ID))
	app.WaitFor("id:desktop-library-grid", 2*time.Second)
	if !app.Find("id:desktop-filter-hdr").Props.Selected {
		t.Fatal("saved HDR filter did not restore")
	}
}

// This catalog exercises the dense layout and HDR controls alongside the real-server test.
type desktopReviewCatalog struct{ fixtureServer }

func (desktopReviewCatalog) Catalog(_ context.Context, q media.Query) (media.Page, error) {
	titles := []string{
		"山间来信", "海边的灯", "冬日站台", "银河快车", "星河以北", "一座空城", "北岸日记",
		"岛上来客", "远行的人", "晚风小镇", "浮云之下", "秋天车站", "月亮旅馆", "白昼尽头",
		"雾起青山", "安静的港湾", "潮声", "长街夜话", "风里有信", "高塔来电", "河岸之约",
		"镜中的城", "林间旅途", "南方的雪", "旧日影院", "晴空之后", "热砂", "深夜停车场",
		"雨过天青", "夏日访客", "暗夜火车", "穿越群山", "低空飞行", "二号月台", "归来的船",
		"回声之谷", "凌晨的海", "逆流", "破晓花园", "山海渡口", "最后一幕", "南岸",
		"离岛", "风从北方来", "夜航", "周末放映厅", "午夜茶馆", "1980 年夏",
	}
	page := media.Page{Total: len(titles)}
	for i := q.Offset; i < min(q.Offset+q.Limit, page.Total); i++ {
		item := media.Item{ID: fmt.Sprint(i), Kind: media.Movie, Title: titles[i],
			Year: 2025 - i%4, Genres: []string{"剧情"}, AddedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour),
			FileSize: 30 << 30, Watched: i%5 == 2,
			VideoProfiles: []media.VideoProfile{{Width: 3840, Height: 2160, Range: "HDR10"}}}
		if i%7 == 1 && !item.Watched {
			item.Position, item.Duration = 28*time.Minute, 114*time.Minute
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

func TestDesktopLibraryGridReview(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("desktop design capture runs on Linux GTK")
	}
	entry := source{ID: "capture", Name: "电影档案", Address: "http://catalog.example.test", Provider: media.Jellyfin,
		Libraries: []sourceLibrary{{Library: media.Library{ID: "test-library", Name: "Movies"}, Category: libraryMovies}}}
	stored, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomDesktop)
				cx.SetSubtreeAppearance(theme.appearance)
				return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
					return desktopReviewCatalog{fixtureServer{session: session}}, nil
				}, nil)
			}, nsgotest.Size(1440, 1500), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Storage(map[string]string{sourcesKey: string(stored)}), nsgotest.Secrets(map[string]string{"capture": "fixture-token"}))
			app.Press("id:tab-library")
			first, seventh := "id:title-0", "id:title-6"
			app.WaitFor(first, 5*time.Second)
			app.WaitFor(seventh, 5*time.Second)
			if app.Find(first).Frame.Origin.Y != app.Find(seventh).Frame.Origin.Y {
				t.Fatal("seven posters did not fit on the first row")
			}
			app.Press("id:desktop-filter-resolution")
			app.Press("role:menuitem 4K")
			app.Advance(400 * time.Millisecond)
			app.Press("id:desktop-filter-hdr")
			app.WaitFor("id:desktop-filter-remove-resolution", time.Second)
			if !app.Find("id:desktop-filter-hdr").Props.Selected {
				t.Fatal("HDR filter is inactive")
			}
			if nsgotest.Target() != "" {
				app.Screenshot("app-desktop-library-grid-" + theme.name)
			}
			app.Resize(1440, 900)
			grid := "id:desktop-library-grid"
			start := app.Find(grid).Offset.Y
			app.Press("id:desktop-index-Z")
			app.WaitUntil(3*time.Second, func() bool { return app.Find(grid).Offset.Y > start })
			app.WaitFor("id:title-40", 3*time.Second)
			if !app.Find("id:desktop-index-Z").Props.Selected {
				t.Fatal("the native alphabet index did not select Z")
			}
		})
	}
}
