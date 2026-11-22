package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/media"
)

func TestOfflinePlaybackRestoresPendingAfterRestart(t *testing.T) {
	entry := source{ID: "offline", Provider: media.Emby, Address: "https://offline.example.test", AccountID: "viewer",
		Libraries: []sourceLibrary{{Library: media.Library{ID: "movies"}}}}
	config, err := json.Marshal([]source{entry})
	if err != nil {
		t.Fatal(err)
	}
	start := func(client *offlineFixture, extra map[string]string) *nsgotest.App {
		extra[sourcesKey] = string(config)
		return nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return appRoot(cx, func(media.Provider, string, media.Device, media.Session) (media.Client, error) {
				return client, nil
			}, nil)
		}, nsgotest.Size(1000, 850), nsgotest.Offscreen(),
			nsgotest.Storage(extra), nsgotest.Secrets(map[string]string{"offline": "sensitive-fixture-token"}))
	}
	before := &offlineFixture{restored: make(chan media.OfflineProgress, 10)}
	first := start(before, map[string]string{})
	first.WaitFor("id:title-test-film", time.Second)
	first.Press("id:title-test-film")
	first.WaitFor("text:Fixture synopsis", time.Second)
	first.Press("id:detail-play")
	first.WaitFor("id:video-player", time.Second)
	first.Play("id:video-player", core.VideoInfo{Duration: 2 * time.Minute})
	const checkpoint = 73 * time.Second
	first.Progress("id:video-player", checkpoint, 2*time.Minute)
	first.PauseVideo("id:video-player", core.VideoPauseControls)

	key := (pendingProgress{Account: mediaAccountKey(entry), Update: media.OfflineProgress{ItemID: "test-film"}}).key()
	readPending := func() string {
		t.Helper()
		value, found := first.Storage().GetItem(key)
		var stored pendingProgress
		if !found || json.Unmarshal([]byte(value), &stored) != nil || stored.Update.Position != checkpoint ||
			stored.Update.MediaID != "video-file" || stored.Update.Previous != 12*time.Second {
			t.Fatalf("checkpoint was not saved with its baseline: %+v", stored.Update)
		}
		if strings.Contains(value, "sensitive-fixture-token") || strings.Contains(value, "stream.mp4") {
			t.Fatal("pending playback stored credentials or a session URL")
		}
		return value
	}
	readPending() // Saved while the player is still open and reports fail.
	first.Press("id:close-player")
	first.WaitFor("id:detail-play", time.Second)
	pending := readPending()

	after := &offlineFixture{restored: make(chan media.OfflineProgress, 10)}
	second := start(after, map[string]string{key: pending})
	second.WaitFor("id:resume-test-film", time.Second)
	second.Press("id:resume-test-film")
	second.WaitFor("id:video-player", time.Second)
	second.Play("id:video-player", core.VideoInfo{Duration: 2 * time.Minute})
	if video := second.FindMounted("id:video-player"); !slices.Contains(video.Seeks, checkpoint) {
		t.Fatal("playback did not use the saved checkpoint")
	}
	second.Press("id:close-player")
	second.WaitFor("id:resume-test-film", time.Second)
	second.Advance(2 * time.Second)
	second.WaitUntil(2*time.Second, func() bool { return len(after.restored) > 0 })
	update := <-after.restored
	if update.Position != checkpoint || update.Previous != 12*time.Second {
		t.Fatalf("restored checkpoint was %+v", update)
	}
	second.WaitUntil(time.Second, func() bool {
		_, found := second.Storage().GetItem(key)
		return !found
	})
}

func TestDockerRememberPlaybackPosition(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" || nsgotest.Target() != "" {
		t.Skip("requires the Docker media servers and the simulated playback surface")
	}
	config, secrets := homeReviewSources(t)
	for _, provider := range []string{"emby", "jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			client, movie := reviewFixtureItem(t, config, secrets, provider, "Waygu Resume Test")
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
			defer cancel()
			const previous = 92 * time.Second
			const position = 123 * time.Second
			setDockerCopyProgress(t, ctx, client, movie, previous)
			app := nsgotest.Start(t, Root, nsgotest.Idiom(core.IdiomPhone), nsgotest.Size(390, 800),
				nsgotest.Offscreen(), nsgotest.Storage(config), nsgotest.Secrets(secrets))
			for _, remember := range []bool{false, true} {
				app.Press("id:tab-settings")
				app.Press("id:open-phone-playback-settings")
				app.Press("id:phone-remember-progress")
				if app.Find("id:phone-remember-progress").Props.Checked != remember {
					t.Fatalf("remember playback progress switch should be %t", remember)
				}
				app.Press("id:back")
				app.Press("id:tab-search")
				app.Clear("id:search-query")
				app.Type("id:search-query", movie.Title)
				app.Press("id:search-source-" + provider)
				result := "id:title-" + provider + "-" + movie.ID
				app.WaitFor(result, 10*time.Second)
				app.Press(result)
				app.WaitFor("id:detail-play >> text:Resume from", 5*time.Second)
				app.Press("id:detail-play")
				app.WaitFor("id:video-player", 5*time.Second)
				app.Play("id:video-player", core.VideoInfo{Duration: movie.Duration})
				if seeks := app.FindMounted("id:video-player").Seeks; !slices.Contains(seeks, previous) {
					t.Fatalf("did not resume at the existing position %s (seeks %v)", previous, seeks)
				}
				app.Progress("id:video-player", position, movie.Duration)
				app.PauseVideo("id:video-player", core.VideoPauseControls)
				app.Press("id:close-player")
				app.WaitFor("id:detail-play", time.Second)
				if !remember {
					app.Advance(2 * time.Second)
					for _, key := range app.Storage().Keys() {
						if strings.HasPrefix(key, progressKeyPrefix) {
							t.Fatal("playback with recording off wrote a pending checkpoint")
						}
					}
					detail, err := client.Detail(ctx, movie)
					if err != nil || detail.Position != previous || detail.Watched {
						t.Fatalf("playback with recording off changed server progress to %s or watched=%t: %v",
							detail.Position, detail.Watched, err)
					}
					app.Press("id:back")
					continue
				}
				app.WaitUntil(5*time.Second, func() bool {
					detail, err := client.Detail(ctx, movie)
					return err == nil && detail.Position == position
				})
			}
		})
	}
}

type offlineFixture struct {
	fixtureServer
	restored chan media.OfflineProgress
	position atomic.Int64
}

func (*offlineFixture) Catalog(context.Context, media.Query) (media.Page, error) {
	return media.Page{Total: 1, Items: []media.Item{{ID: "test-film", Kind: media.Movie, Title: "Offline Film",
		Poster: "https://offline.example.test/cover?api_key=sensitive-fixture-token"}}}, nil
}

func (f *offlineFixture) Resume(context.Context) ([]media.Item, error) {
	if position := time.Duration(f.position.Load()); position > 0 {
		return []media.Item{{ID: "test-film", Kind: media.Movie, Title: "Offline Film", LibraryID: "movies",
			Position: position, Duration: 2 * time.Minute, LastPlayed: time.Now()}}, nil
	}
	return nil, errors.New("fixture server is offline")
}

func (*offlineFixture) Playback(_ context.Context, item media.Item, _ media.PlaybackOptions) (media.Stream, error) {
	return media.Stream{ItemID: item.ID, SourceID: "video-file", Position: 12 * time.Second, Duration: 2 * time.Minute,
		Method: media.DirectPlay, URL: "https://offline.example.test/stream.mp4?api_key=sensitive-fixture-token"}, nil
}

func (*offlineFixture) Report(context.Context, media.PlaybackState) error {
	return errors.New("fixture progress endpoint is offline")
}

func (f *offlineFixture) SyncOfflineProgress(_ context.Context, update media.OfflineProgress) error {
	f.position.Store(int64(update.Position))
	f.restored <- update
	return nil
}
