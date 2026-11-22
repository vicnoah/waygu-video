package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/media"
)

func reviewSeriesFixture(t *testing.T, config, secrets map[string]string, sourceID string) (source, media.Client, media.Item, []media.Item) {
	t.Helper()
	return reviewNamedSeriesFixture(t, config, secrets, sourceID, "Waygu Review Series")
}

func reviewNamedSeriesFixture(t *testing.T, config, secrets map[string]string, sourceID, title string) (source, media.Client, media.Item, []media.Item) {
	t.Helper()
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, entry := range sources {
		if entry.ID != sourceID {
			continue
		}
		client, err := media.NewClient(entry.Provider, entry.Address, media.NewDevice("Waygu series review"),
			media.Session{UserID: entry.AccountID, Token: secrets[entry.ID]})
		if err != nil {
			t.Fatal(err)
		}
		for _, library := range entry.Libraries {
			if library.Name != "Shows" {
				continue
			}
			page, err := client.Catalog(ctx, media.Query{LibraryID: library.ID, Search: title})
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range page.Items {
				if item.Kind != media.Series || !strings.HasPrefix(item.Title, title) {
					continue
				}
				item.LibraryID = library.ID
				seasons, err := client.Children(ctx, item)
				if err != nil {
					t.Fatal(err)
				}
				return entry, client, item, seasons
			}
		}
	}
	t.Fatalf("fixture series %q is missing from %s", title, sourceID)
	return source{}, nil, media.Item{}, nil
}

func seriesFixtureEpisodes(t *testing.T, ctx context.Context, client media.Client, seasons []media.Item, number, count int) (media.Item, []media.Item) {
	t.Helper()
	for _, season := range seasons {
		if season.Season != number {
			continue
		}
		rows, err := client.Children(ctx, season)
		if err != nil || len(rows) != count {
			t.Fatalf("season %d: got %d episodes, want %d: %v", number, len(rows), count, err)
		}
		slices.SortFunc(rows, func(a, b media.Item) int { return a.Episode - b.Episode })
		return season, rows
	}
	t.Fatalf("fixture season %d is missing", number)
	return media.Item{}, nil
}

func TestDockerSeriesEpisodes(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			_, client, series, seasons := reviewSeriesFixture(t, config, secrets, provider)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			_, specials := seriesFixtureEpisodes(t, ctx, client, seasons, 0, 1)
			seriesFixtureEpisodes(t, ctx, client, seasons, 1, 5)
			_, second := seriesFixtureEpisodes(t, ctx, client, seasons, 2, 2)
			for _, episode := range []media.Item{specials[0], second[0]} {
				if episode.Kind != media.Episode || episode.SeriesID != series.ID || episode.Episode != 1 {
					t.Fatalf("unexpected fixture episode identity: %+v", episode)
				}
				stream, err := client.Playback(ctx, episode, media.PlaybackOptions{})
				if err != nil || stream.File.URL == "" || stream.ItemID != episode.ID || stream.Duration < 17*time.Second {
					t.Fatalf("resolve episode for playback: %v", err)
				}
			}
		})
	}
}

func TestDockerPhoneSeriesReview(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the Docker fixtures")
	}
	config, secrets := homeReviewSources(t)
	entry, client, series, seasons := reviewSeriesFixture(t, config, secrets, "emby")
	ctx, cancel := context.WithTimeout(t.Context(), 18*time.Second)
	defer cancel()
	first, episodes := seriesFixtureEpisodes(t, ctx, client, seasons, 1, 5)
	second, secondEpisodes := seriesFixtureEpisodes(t, ctx, client, seasons, 2, 2)
	_, specials := seriesFixtureEpisodes(t, ctx, client, seasons, 0, 1)
	if err := client.(media.WatchedClient).SetWatched(ctx, episodes[0], true); err != nil {
		t.Fatal(err)
	}
	for i, episode := range episodes {
		count := 12 + i
		binding, err := json.Marshal(danmakuBinding{DandanEpisode: danmaku.DandanEpisode{ID: int64(i + 1)}, Count: &count})
		if err != nil {
			t.Fatal(err)
		}
		config[danmakuBindingKey(route{source: entry, item: episode})] = string(binding)
	}
	encoded, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	config[sourcesKey] = string(encoded)
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			seed, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			for _, episode := range secondEpisodes {
				if err := client.(media.WatchedClient).SetWatched(seed, episode, false); err != nil {
					t.Fatal(err)
				}
			}
			setDockerCopyProgress(t, seed, client, episodes[1], 144*time.Second)
			height := core.Points(1500)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetSubtreeAppearance(variant.appearance)
				shellIdiom.Provide(cx, core.IdiomPhone)
				return Root(cx)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(config), nsgotest.Secrets(secrets))
			app.WaitFor("id:title-"+series.ID, 10*time.Second)
			app.Press("id:title-" + series.ID)
			app.WaitFor("id:series-resume-progress", 5*time.Second)
			app.WaitFor("id:episode-progress-"+episodes[1].ID, 5*time.Second)
			if caption := app.Find("id:series-resume-caption").Text; !strings.Contains(caption, "本季剩余 3 集") {
				t.Fatalf("resume caption = %q", caption)
			}
			if height := app.Find("id:phone-series-hero").Frame.Size.Height; height != 420 {
				t.Fatalf("hero height = %v, want 420", height)
			}
			if height := app.Find("id:detail-play").Frame.Size.Height; height != 52 {
				t.Fatalf("play button height = %v, want 52", height)
			}
			if frame := app.Find("id:episode-art-" + episodes[0].ID).Frame.Size; frame.Width != 128 || frame.Height != 72 {
				t.Fatalf("episode thumbnail = %+v, want 128 × 72", frame)
			}
			app.WaitFor("text:13 条弹幕", time.Second)
			app.LongPress("id:child-" + episodes[4].ID)
			app.WaitFor("id:series-episode-menu", 3*time.Second)
			app.Advance(350 * time.Millisecond)
			app.Press("id:series-episode-details")
			app.WaitFor("id:detail-download", 5*time.Second)
			app.Press("id:detail-download")
			app.WaitFor("role:button 已下载", 8*time.Second)
			app.Press("id:back")
			app.WaitFor("id:episode-progress-"+episodes[1].ID, 5*time.Second)
			app.WaitFor("text:/已下载/", 3*time.Second)
			capture := func(suffix string) {
				if nsgotest.Target() != "" {
					app.Advance(700 * time.Millisecond)
					app.Screenshot("series-detail-" + variant.name + suffix)
				}
			}
			capture("")
			app.Press("id:series-sort")
			app.WaitFor("id:series-descending", time.Second)
			app.Press("id:series-descending")
			if a, b := app.Find("id:child-"+episodes[0].ID), app.Find("id:child-"+episodes[4].ID); a.Frame.Origin.Y <= b.Frame.Origin.Y {
				t.Fatal("descending sort left the earliest episode first")
			}
			app.Press("id:series-sort")
			app.Press("id:series-hide-watched")
			app.WaitForGone("id:child-"+episodes[0].ID, time.Second)
			app.Press("id:series-tab-" + second.ID)
			app.WaitFor("id:child-"+secondEpisodes[0].ID, 5*time.Second)
			app.WaitForGone("id:child-"+episodes[1].ID, time.Second)
			capture("-s2")
			app.Press("id:child-" + secondEpisodes[0].ID)
			video := app.WaitFor("id:video-player", 8*time.Second)
			if nsgotest.Target() == "" {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				probe, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
					"-show_entries", "stream=height", "-of", "csv=p=0", video.Video.Source).Output()
				if err != nil || strings.TrimSpace(string(probe)) != "360" {
					t.Fatalf("season two video failed to decode: %q, %v", probe, err)
				}
			} else {
				app.WaitForGone("text:正在加载…", 10*time.Second)
				before := app.Find("id:player-position").Text
				app.WaitUntil(6*time.Second, func() bool { return app.Find("id:player-position").Text != before })
			}
			app.Press("id:close-player")
			app.WaitFor("id:child-"+secondEpisodes[1].ID, 5*time.Second)
			if !app.Find("id:series-tab-" + second.ID).Props.Selected {
				t.Fatal("returning from playback lost the selected season")
			}
			app.WaitFor("text:倒序", time.Second)
			app.Press("id:series-tab-specials")
			app.WaitFor("id:child-"+specials[0].ID, 5*time.Second)
			capture("-extras")
			app.Press("id:series-tab-" + first.ID)
			app.WaitFor("id:episode-progress-"+episodes[1].ID, 5*time.Second)
			app.Press("id:detail-play")
			app.WaitFor("id:video-player", 8*time.Second)
			if nsgotest.Target() == "" {
				app.Play("id:video-player", core.VideoInfo{Duration: 6 * time.Minute})
				if actual := app.Find("id:player-position").Text; actual != "02:24" {
					t.Fatalf("playback resumed at %q, want 02:24", actual)
				}
			} else {
				app.WaitForGone("text:正在加载…", 10*time.Second)
				app.WaitUntil(6*time.Second, func() bool {
					return strings.HasPrefix(app.Find("id:player-position").Text, "02:")
				})
			}
			app.Press("id:close-player")
			app.WaitFor("id:child-"+episodes[4].ID, 5*time.Second)
			app.Press("id:child-" + episodes[4].ID)
			saved := app.WaitFor("id:video-player", 8*time.Second)
			app.WaitFor("text:离线播放", 5*time.Second)
			if strings.HasPrefix(saved.Video.Source, "http") {
				t.Fatal("downloaded episode opened as a network stream")
			}
			if nsgotest.Target() != "android" && nsgotest.Target() != "web" {
				if info, err := os.Stat(saved.Video.Source); err != nil || info.Size() == 0 {
					t.Fatalf("downloaded episode was not available for local playback: %v", err)
				}
			}
		})
	}
}
