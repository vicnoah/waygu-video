package main

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerDesktopMovieReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker media fixtures")
	}
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("desktop design capture runs on Linux GTK")
	}
	config, secrets := homeReviewSources(t)
	emby, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	recommendation := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Test Movie")
	resume := reviewFixtureTitle(t, config, secrets, "emby", "Waygu Resume Test")
	_, downloadable := reviewFixtureItem(t, config, secrets, "emby", "Waygu Download Test")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	extras, err := emby.(media.MovieFeaturesClient).Extras(ctx, movie)
	if err != nil || !slices.ContainsFunc(extras, func(item media.Item) bool { return item.Trailer }) {
		t.Fatalf("trailer fixture is missing: %v, %v", extras, err)
	}
	fixtures := map[string]string{"emby": movie.ID}
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		client, item := reviewFixtureItem(t, config, secrets, provider, "Waygu Track Test")
		fixtures[provider] = item.ID
		versions, err := client.(media.VersionClient).Versions(ctx, item)
		if err != nil || !slices.ContainsFunc(versions, func(version media.Version) bool {
			return version.FrameRate > 0 && version.FilePath != "" && version.Size > 0
		}) {
			t.Fatalf("%s did not return the file path, frame rate and size: %v, %+v", provider, err, versions)
		}
	}
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomDesktop)
				cx.SetSubtreeAppearance(theme.appearance)
				return Root(cx)
			}, nsgotest.Size(1440, 1500), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Idiom(core.IdiomDesktop), nsgotest.Storage(config), nsgotest.Secrets(secrets))
			open := func(sourceID, itemID string) {
				t.Helper()
				app.WaitFor("id:tab-library", 8*time.Second)
				app.Press("id:tab-library")
				app.WaitFor("id:desktop-filter-source", 5*time.Second)
				app.Press("id:desktop-filter-source")
				label := map[string]string{"emby": "Emby", "jellyfin": "Jellyfin", "plex": "Plex"}[sourceID] + " 影视库"
				app.WaitFor("role:menuitem "+label, 3*time.Second)
				app.Press("role:menuitem " + label)
				app.Advance(350 * time.Millisecond)
				card := "id:title-" + sourceID + "-" + itemID
				app.WaitFor(card, 8*time.Second)
				app.Press(card)
				app.WaitFor("id:desktop-movie-hero", 5*time.Second)
				app.WaitFor("id:desktop-sidebar", 3*time.Second)
			}
			open("emby", movie.ID)
			if size := app.Find("id:desktop-movie-poster").Frame.Size; size.Width != 208 || size.Height != 300 {
				t.Fatalf("poster size = %+v, want 208 × 300", size)
			}
			if size := app.Find("id:desktop-movie-hero").Frame.Size; size.Width != 1136 || size.Height != 500 {
				t.Fatalf("hero size = %+v, want 1136 × 500", size)
			}
			if width := app.Find("id:detail-file").Frame.Size.Width; width != 320 {
				t.Fatalf("file information width = %v, want 320", width)
			}
			app.WaitFor("id:detail-trailer", 8*time.Second)
			app.WaitFor("id:desktop-movie-recommendations", 8*time.Second)
			app.WaitFor("id:desktop-movie-cast", 8*time.Second)
			app.WaitFor("id:detail-subtitle", 8*time.Second)
			app.WaitFor("id:detail-favorite", 8*time.Second)
			app.WaitFor("id:detail-download", 5*time.Second)
			app.WaitFor("id:desktop-movie-hero >> text:8.3", 5*time.Second)
			if app.Exists("id:desktop-movie-hero >> text:IMDb") || app.Exists("id:desktop-movie-hero >> text:TMDB") {
				t.Fatal("the Emby community rating acquired a provider label")
			}
			if nsgotest.Target() != "" {
				app.Advance(800 * time.Millisecond)
				app.Screenshot("app-desktop-movie-" + theme.name)
			}
			if theme.appearance != core.AppearanceLight {
				return
			}

			favorite := app.Find("id:detail-favorite").Props.Selected
			app.Press("id:detail-favorite")
			app.WaitUntil(5*time.Second, func() bool { return app.Find("id:detail-favorite").Props.Selected != favorite })
			app.Press("id:detail-favorite")
			app.WaitUntil(5*time.Second, func() bool { return app.Find("id:detail-favorite").Props.Selected == favorite })
			watched := app.Find("id:detail-watched").Props.Selected
			app.Press("id:detail-more")
			app.Press("id:desktop-movie-menu-watched")
			app.WaitUntil(5*time.Second, func() bool { return app.Find("id:detail-watched").Props.Selected != watched })
			app.Press("id:detail-watched")
			app.WaitUntil(5*time.Second, func() bool { return app.Find("id:detail-watched").Props.Selected == watched })
			app.Press("id:detail-danmaku")
			app.WaitFor("id:danmaku-search", 3*time.Second)
			app.Advance(400 * time.Millisecond)
			app.Press("id:detail-close-danmaku")
			app.Advance(400 * time.Millisecond)
			app.WaitForGone("id:detail-danmaku-dialog", 3*time.Second)
			app.Press("id:detail-trailer")
			app.WaitFor("id:video-player", 10*time.Second)
			if nsgotest.Target() != "" {
				app.WaitForGone("text:正在加载…", 10*time.Second)
				position := app.Find("id:player-position").Text
				app.WaitUntil(6*time.Second, func() bool { return app.Find("id:player-position").Text != position })
			}
			app.Press("id:close-player")
			app.WaitFor("id:desktop-movie-hero", 5*time.Second)
			app.WaitFor("id:desktop-movie-recommendations", 5*time.Second)
			app.Press("id:title-emby-" + recommendation)
			app.WaitUntil(4*time.Second, func() bool { return app.Find("id:desktop-movie-title").Text == "Waygu Test Movie" })
			app.Press("id:back")
			app.WaitUntil(4*time.Second, func() bool { return app.Find("id:desktop-movie-title").Text == movie.Title })
			app.Press("id:back")
			open("emby", resume)
			app.WaitFor("id:detail-from-start", 5*time.Second)
			app.Press("id:detail-from-start")
			app.WaitFor("id:video-player", 10*time.Second)
			if nsgotest.Target() == "" {
				app.Play("id:video-player", core.VideoInfo{Duration: 6 * time.Minute})
				if got := app.Find("id:player-position").Text; got != "00:00" {
					t.Fatalf("play from beginning started at %q", got)
				}
			}
			app.Press("id:close-player")
			open("jellyfin", fixtures["jellyfin"])
			app.WaitFor("text:/640×360|320×180/", 5*time.Second)
			open("plex", fixtures["plex"])
			app.WaitFor("text:/640×360|320×180/", 5*time.Second)
			app.Press("id:tab-library")
			app.WaitFor("id:desktop-library", 5*time.Second)
			app.WaitForGone("id:desktop-movie", 2*time.Second)
			shared := "id:title-emby-" + recommendation
			app.WaitFor(shared, 5*time.Second)
			app.Press(shared)
			app.WaitFor("id:playback-source", 5*time.Second)
			app.WaitFor("text:/640×360|320×180/", 5*time.Second)
			app.Press("id:playback-source")
			app.Press("role:option Jellyfin 影视库")
			app.WaitFor("id:detail-file >> text:Jellyfin 影视库", 5*time.Second)

			open("emby", downloadable.ID)
			app.WaitFor("id:detail-file >> text:Emby 影视库", 5*time.Second)
			app.Press("id:detail-download")
			app.WaitFor("id:detail-download-quality-720", 5*time.Second)
			app.Press("id:detail-download-quality-720")
			app.WaitUntil(5*time.Second, func() bool { return !app.Find("id:detail-download-confirm").Props.Disabled })
			app.Press("id:detail-download-confirm")
			app.FastForward(350 * time.Millisecond)
			app.WaitForGone("id:detail-download-quality", 3*time.Second)
			app.WaitFor("role:button 已下载", 30*time.Second)
			raw, _ := app.Storage().GetItem(downloadQueueKey)
			var entries []downloadEntry
			if err := json.Unmarshal([]byte(raw), &entries); err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Item.ID != downloadable.ID || entries[0].Height != 720 ||
				entries[0].Version.SourceID == "" || entries[0].State != downloadComplete {
				t.Fatalf("the selected video was not saved as a 720p download")
			}
			app.Press("id:detail-download")
			app.WaitFor("text:离线播放", 5*time.Second)
		})
	}
}

type desktopMovieFixture struct{ fixtureServer }

func (desktopMovieFixture) Catalog(context.Context, media.Query) (media.Page, error) {
	return media.Page{Total: 1, Items: []media.Item{{
		ID: "test-film", Kind: media.Movie, Title: "山海之外", OriginalTitle: "Beyond the Coast", Year: 2024, Duration: 139 * time.Minute,
		Genres: []string{"科幻", "冒险"}, ContentRating: "PG-13", Rating: 8.5, Position: 56*time.Minute + 21*time.Second,
	}}}, nil
}

func (desktopMovieFixture) Detail(_ context.Context, item media.Item) (media.Item, error) {
	item.Overview = "一位气象学家沿着被遗忘的海岸寻找失踪的考察队。旅途越深入，远处的灯塔就越清晰，她也逐渐发现，过去的讯息一直与眼前的人产生着联系。"
	item.Rating = 9.6
	item.Ratings = media.TitleRatings{IMDb: 8.5, TMDB: 8.2}
	item.AddedAt = time.Date(2026, time.November, 17, 22, 41, 0, 0, time.UTC)
	item.Countries = []string{"中国", "新加坡"}
	item.People = []media.Person{
		{Name: "沈珂", Director: true}, {Name: "林远", Kind: "writer"}, {Name: "钟语", Kind: "writer"},
		{Name: "周瑶", Kind: "actor", Role: "许晴"}, {Name: "陆松", Kind: "actor", Role: "高宇"},
		{Name: "李青", Kind: "actor", Role: "小周"}, {Name: "申月", Kind: "actor", Role: "吴萍"},
		{Name: "林海", Kind: "actor", Role: "赵言"}, {Name: "关丛", Kind: "actor", Role: "船长"},
		{Name: "周泉", Kind: "actor", Role: "李望"},
	}
	return item, nil
}

func (desktopMovieFixture) Versions(context.Context, media.Item) ([]media.Version, error) {
	return []media.Version{
		{ID: "4k", Width: 3840, Height: 2160, Container: "mkv", VideoCodec: "hevc", FilePath: "/电影档案/山海之外 (2024)/beyond-the-coast.mkv",
			FrameRate: 23.976, VideoRange: "DOVI", Size: 58 << 30, Bitrate: 46_800_000, Method: media.DirectPlay,
			Tracks: []media.Track{{Type: "audio", ID: "1", Title: "中文 · TrueHD 7.1 Atmos", Default: true},
				{Type: "audio", ID: "2", Title: "Japanese · AAC"}, {Type: "subtitle", ID: "3", Language: "zh", Title: "简体中文 · ASS", URL: "http://catalog.example.test/subtitle.vtt"},
				{Type: "subtitle", ID: "4", Language: "eng", Title: "English", External: true, URL: "http://catalog.example.test/english.vtt"}}},
		{ID: "hd", Width: 1920, Height: 1080, Container: "mp4", VideoCodec: "h264", Size: 4 << 30},
	}, nil
}

func (desktopMovieFixture) Extras(context.Context, media.Item) ([]media.Item, error) {
	return []media.Item{{Kind: media.Video, ID: "trailer", Trailer: true}}, nil
}

func (desktopMovieFixture) RelatedMovies(context.Context, media.Item) ([]media.Item, error) {
	titles := []string{"回声", "南方的雪", "离岛", "风从北方来", "长街"}
	items := make([]media.Item, 0, len(titles))
	for _, title := range titles {
		items = append(items, media.Item{Kind: media.Movie, ID: title, Title: title, Genres: []string{"科幻"}, Year: 2020,
			VideoProfiles: []media.VideoProfile{{Width: 3840, Height: 2160}}})
	}
	return items, nil
}

func (desktopMovieFixture) SetFavorite(context.Context, media.Item, bool) error { return nil }
func (desktopMovieFixture) SetWatched(context.Context, media.Item, bool) error  { return nil }
func (desktopMovieFixture) Favorites(context.Context, media.Query) (media.Page, error) {
	return media.Page{}, nil
}

func movieDesignFixtureConfig(t *testing.T) map[string]string {
	t.Helper()
	entry := source{ID: "capture", Name: "电影档案", Address: "http://catalog.example.test", Provider: media.Jellyfin,
		Libraries: []sourceLibrary{{Library: media.Library{ID: "test-library", Name: "Movies"}, Category: libraryMovies}}}
	data, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{sourcesKey: string(data),
		danmakuBindingKey(route{source: entry, item: media.Item{ID: "test-film"}}): `{"episodeId":123}`}
}

func movieDesignFixtureRoot(cx *core.Scope) core.Node {
	return appRoot(cx, func(_ media.Provider, _ string, _ media.Device, session media.Session) (media.Client, error) {
		return desktopMovieFixture{fixtureServer{session: session}}, nil
	}, nil)
}

func TestDesktopMovieDesignReview(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("desktop design capture runs on Linux GTK")
	}
	config := movieDesignFixtureConfig(t)
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			height := core.Points(1500)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			free, quota := int64(20<<30), int64(5<<30)
			if theme.appearance == core.AppearanceDark {
				free, quota = quota, free
			}
			config[downloadQuotaKey] = strconv.FormatInt(quota, 10)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomDesktop)
				cx.SetSubtreeAppearance(theme.appearance)
				return movieDesignFixtureRoot(cx)
			}, nsgotest.Size(1440, height), nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
				nsgotest.Idiom(core.IdiomDesktop), nsgotest.Storage(config), nsgotest.Secrets(map[string]string{"capture": "fixture-token"}),
				nsgotest.DiskSpace(core.DiskSpace{Available: free}, nil))
			app.Press("id:tab-library")
			app.WaitFor("id:title-test-film", 5*time.Second)
			app.Press("id:title-test-film")
			app.WaitFor("id:desktop-movie-hero >> text:IMDb", 5*time.Second)
			app.WaitFor("id:desktop-movie-hero >> text:8.5", 5*time.Second)
			app.WaitFor("id:desktop-movie-hero >> text:TMDB", 5*time.Second)
			app.WaitFor("id:desktop-movie-hero >> text:82%", 5*time.Second)
			if app.Exists("id:desktop-movie-hero >> text:9.6") {
				t.Fatal("the hero mixed the generic star with named ratings")
			}
			app.WaitFor("id:desktop-movie-recommendations", 5*time.Second)
			app.WaitFor("id:playback-version", 5*time.Second)
			app.WaitFor("id:detail-favorite", 5*time.Second)
			app.WaitFor("id:detail-trailer", 5*time.Second)
			app.WaitFor("id:detail-progress", 5*time.Second)
			app.WaitFor("text:已匹配 · 弹弹play", 5*time.Second)
			pick := func(id, match string) {
				t.Helper()
				app.Press("id:" + id)
				app.Press("role:option /" + regexp.QuoteMeta(match) + "/")
				app.Advance(400 * time.Millisecond)
			}
			pick("detail-subtitle", "简体中文")
			if nsgotest.Target() != "" {
				app.Advance(700 * time.Millisecond)
				app.Screenshot("app-desktop-movie-design-" + theme.name)
			}
			pick("detail-audio", "Japanese")
			if got := app.Find("id:detail-audio-value").Text; !strings.Contains(got, "Japanese") {
				t.Fatalf("audio display = %q", got)
			}
			if got := app.Find("id:detail-subtitle-value").Text; !strings.Contains(got, "简体中文") {
				t.Fatalf("subtitle display = %q", got)
			}
			openDownload := func() {
				app.WaitFor("id:detail-download", 5*time.Second)
				app.Press("id:detail-download")
				app.WaitFor("id:detail-download-quality", 3*time.Second)
				app.Advance(350 * time.Millisecond)
			}
			openDownload()
			app.WaitFor("text:约 58.0 GB", 3*time.Second)
			app.WaitFor("text:预计需要 58.0 GB · 可用 5.0 GB", 3*time.Second)
			if !app.Find("id:detail-download-confirm").Props.Disabled {
				t.Fatal("the 4K original exceeds the available space")
			}
			if nsgotest.Target() != "" {
				app.Screenshot("app-desktop-movie-download-blocked-" + theme.name)
			}
			app.Press("id:detail-download-quality-1080")
			app.WaitFor("text:预计需要 9.7 GB · 可用 5.0 GB", 3*time.Second)
			if !app.Find("id:detail-download-confirm").Props.Disabled {
				t.Fatal("1080p transcode exceeds the available space")
			}
			app.Press("id:detail-download-quality-720")
			app.WaitForGone("id:detail-download-insufficient-space", 3*time.Second)
			if app.Find("id:detail-download-confirm").Props.Disabled {
				t.Fatal("720p transcode fits in the available space")
			}
			app.Press("id:detail-download-quality-0")
			app.Press("id:detail-download-storage")
			app.WaitFor("id:download-quality", 3*time.Second)
			app.Press("id:back")
			app.WaitFor("id:playback-version", 3*time.Second)
			pick("playback-version", "1080p")
			if got := app.Find("id:playback-version-value").Text; !strings.Contains(got, "1080p") {
				t.Fatalf("version display = %q", got)
			}
			openDownload()
			app.WaitFor("text:约 4.0 GB", 3*time.Second)
			if app.Find("id:detail-download-confirm").Props.Disabled || app.Exists("id:detail-download-insufficient-space") {
				t.Fatal("the selected 1080p original fits in the available space")
			}
			if nsgotest.Target() != "" {
				app.Screenshot("app-desktop-movie-download-ready-" + theme.name)
			}
			app.Press("role:button 取消")
			app.FastForward(350 * time.Millisecond)
			app.WaitForGone("id:detail-download-quality", 3*time.Second)
			if _, exists := app.Storage().GetItem(downloadQueueKey); exists {
				t.Fatal("visiting storage or cancelling wrote to the queue")
			}
			app.Press("id:back")
			app.WaitFor("id:desktop-library", 5*time.Second)
			app.WaitFor("id:title-test-film", 5*time.Second)
			app.Press("id:title-test-film")
			app.WaitUntil(5*time.Second, func() bool {
				return app.Exists("id:playback-version-value") && strings.Contains(app.Find("id:playback-version-value").Text, "1080p")
			})
			app.Resize(880, 900)
			app.WaitForGone("id:desktop-movie-poster", 3*time.Second)
			app.Press("id:desktop-sidebar-close")
			app.WaitFor("id:desktop-movie-poster", 3*time.Second)
			app.Press("id:back")
			app.WaitFor("id:desktop-sidebar-open", 3*time.Second)
		})
	}
}
