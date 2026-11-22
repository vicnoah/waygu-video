package main

import (
	"context"
	"encoding/json"
	"errors"
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
