package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestDockerTVNextEpisodeAcrossSeasons(t *testing.T) {
	target := nsgotest.Target()
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || (target != "" && target != "linux" && target != "android") {
		t.Skip("requires the Docker media servers and a TV playback viewport")
	}
	if target != "" {
		testTVNextEpisodeAcrossSeasons(t, target, media.Emby)
		return
	}
	for _, provider := range []media.Provider{media.Emby, media.Jellyfin, media.Plex} {
		t.Run(string(provider), func(t *testing.T) { testTVNextEpisodeAcrossSeasons(t, target, provider) })
	}
}

func testTVNextEpisodeAcrossSeasons(t *testing.T, target string, provider media.Provider) {
	t.Helper()
	config, secrets := homeReviewSources(t)
	entry, series, firstSeason, last, next := tvEpisodeFixtures(t, config, secrets, provider)
	onlySeries, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(onlySeries)
	config[autoNextKey] = "true"
	config[playbackPreferencesKey] = `{"DisableProgressRecording":true}`
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomTV)
		return Root(cx)
	}, nsgotest.Size(1920, 1080), nsgotest.Offscreen(),
		nsgotest.Environment(core.Environment{OS: core.OSLinux, Locale: "zh-Hans"}),
		nsgotest.Storage(config), nsgotest.Secrets(secrets))

	app.WaitFor("id:home-hero-play", 8*time.Second)
	app.Press("id:tv-tab-search")
	app.WaitFor("id:search-query", 5*time.Second)
	app.Type("id:search-query", series.Title)
	app.Press("id:search-submit")
	app.WaitUntil(3*time.Second, func() bool { return !app.Find("id:search-query").Focused })
	seriesCard := "id:title-" + series.ID
	app.WaitFor(seriesCard, 8*time.Second)
	app.Press(seriesCard)
	openChild := func(id string) {
		selector := "id:tv-detail-episodes-title-" + id
		app.WaitFor(selector, 8*time.Second)
		app.Scroll("id:tv-detail-scroll", 1500)
		app.Press(selector)
	}
	openChild(firstSeason.ID)
	openChild(last.ID)

	const endCard = "id:tv-next-episode"
	const play = "id:play-next-episode"
	const dismiss = "id:dismiss-next-episode"
	const surface = "id:toggle-play"
	playheadStarted := func() bool {
		position := app.Query("id:player-position")
		return len(position) == 1 && position[0].Text != "00:00"
	}
	openPlayer := func() {
		app.WaitFor("id:detail-play", 5*time.Second)
		if app.Exists("id:detail-from-start") {
			app.Press("id:detail-from-start")
		} else {
			app.Press("id:detail-play")
		}
		app.WaitFor("id:tv-player", 10*time.Second)
		app.WaitFor("id:video-player", 8*time.Second)
		app.WaitUntil(10*time.Second, func() bool {
			if node := app.Query("id:tv-player-episode"); len(node) == 1 {
				return strings.Contains(node[0].Text, last.Title)
			}
			return false
		})
		if target == "" {
			app.Play("id:video-player", core.VideoInfo{Duration: 90 * time.Second})
			app.Progress("id:video-player", 59*time.Second, 90*time.Second)
			if app.Exists(endCard) {
				t.Fatal("the card appeared before the last 30 seconds")
			}
		} else {
			app.WaitUntil(16*time.Second, playheadStarted)
		}
		app.FastForward(3200 * time.Millisecond)
		app.WaitForGone("id:tv-player-osd", 6*time.Second)
		if target == "" {
			app.Progress("id:video-player", time.Minute, 90*time.Second)
		}
		app.WaitFor(endCard, 25*time.Second)
		app.WaitUntil(3*time.Second, func() bool { return app.Find("id:tv-next-episode-title").Text == next.Title })
	}
	focused := func(selector string) bool {
		node := app.Query(selector)
		return len(node) == 1 && node[0].Focused && node[0].Props.Selected
	}
	capture := func(name string) {
		if target != "" {
			app.Screenshot(name)
		}
	}

	openPlayer()
	app.WaitForGone("id:tv-player-osd", 2*time.Second)
	capture("tv-next-preview")
	app.Key("down")
	app.WaitUntil(2*time.Second, func() bool { return focused(play) })
	app.Key("right")
	app.WaitUntil(2*time.Second, func() bool { return focused(dismiss) })
	capture("tv-next-dismiss-focus")
	app.Key("enter")
	app.WaitForGone(endCard, 3*time.Second)
	app.WaitUntil(3*time.Second, func() bool { return app.Find(surface).Focused })
	if target == "" {
		app.Progress("id:video-player", 90*time.Second, 90*time.Second)
		app.FastForward(11 * time.Second)
	} else {
		app.WaitUntil(15*time.Second, func() bool { return app.Find(surface).Props.Label == "播放" })
	}
	if title := app.Find("id:tv-player-episode").Text; !strings.Contains(title, last.Title) {
		t.Fatalf("dismissing the card changed the episode: %q", title)
	}
	app.Key("escape")
	app.WaitFor("id:detail-play", 7*time.Second)

	openPlayer()
	if target == "" {
		app.Progress("id:video-player", 90*time.Second, 90*time.Second)
	}
	app.WaitUntil(15*time.Second, func() bool {
		status := app.Query("id:tv-next-episode-status")
		return len(status) == 1 && status[0].Text != "下一集" && focused(play)
	})
	capture("tv-next-countdown")
	app.FastForward(10 * time.Second)
	app.WaitUntil(17*time.Second, func() bool {
		if labels := app.Query("id:tv-player-episode"); len(labels) == 1 {
			return strings.Contains(labels[0].Text, "第 2 季") && strings.Contains(labels[0].Text, next.Title)
		}
		return false
	})
	if target != "" {
		app.WaitUntil(12*time.Second, playheadStarted)
	}
	app.WaitUntil(3*time.Second, func() bool { return app.Find(surface).Focused })
	app.Key("enter")
	app.WaitUntil(4*time.Second, func() bool { return app.Find(surface).Props.Label == "播放" })
	capture("tv-next-season")
	app.Key("escape")
	app.WaitFor("id:detail-play", 7*time.Second)
}

func tvEpisodeFixtures(t *testing.T, config, secrets map[string]string, provider media.Provider) (source, media.Item, media.Item, media.Item, media.Item) {
	t.Helper()
	var entries []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &entries); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(entries, func(entry source) bool { return entry.Provider == provider })
	if i < 0 {
		t.Fatalf("%s TV fixture is unavailable", provider)
	}
	entry := entries[i]
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, err := media.NewClient(entry.Provider, entry.Address, media.NewDevice("Waygu TV episode review"),
		media.Session{UserID: entry.AccountID, Token: secrets[entry.ID]})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.Catalog(ctx, media.Query{Search: "Waygu Review Series"})
	if err != nil {
		t.Fatal(err)
	}
	series := slices.IndexFunc(page.Items, func(item media.Item) bool { return item.Kind == media.Series && item.Title == "Waygu Review Series" })
	if series < 0 {
		t.Fatalf("Waygu Review Series is missing from %s", provider)
	}
	children := func(item media.Item) []media.Item {
		t.Helper()
		items, err := client.Children(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	seasons := children(page.Items[series])
	season := func(number int) media.Item {
		t.Helper()
		i := slices.IndexFunc(seasons, func(item media.Item) bool { return item.Kind == media.Season && item.Season == number })
		if i < 0 {
			t.Fatalf("season %d is missing", number)
		}
		return seasons[i]
	}
	episode := func(season media.Item, number int) media.Item {
		t.Helper()
		episodes := children(season)
		i := slices.IndexFunc(episodes, func(item media.Item) bool { return item.Kind == media.Episode && item.Episode == number })
		if i < 0 {
			t.Fatalf("episode S%02dE%02d is missing", season.Season, number)
		}
		return episodes[i]
	}
	first := season(1)
	last, next := episode(first, 5), episode(season(2), 1)
	if last.Title == "" || next.Title == "" {
		t.Fatal("fixture episodes have no display title")
	}
	return entry, page.Items[series], first, last, next
}
