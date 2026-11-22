package main

import (
	"context"
	"encoding/json"
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

type librarySettingsFixture struct {
	fixtureServer
	scans, reads atomic.Int32
}

func (f *librarySettingsFixture) Libraries(context.Context) ([]media.Library, error) {
	f.reads.Add(1)
	return []media.Library{{ID: "movies", Name: "Movies", Kind: "movies"}, {ID: "shows", Name: "Shows", Kind: "tvshows"},
		{ID: "new", Name: "New library", Kind: "movies"}}, nil
}

func (f *librarySettingsFixture) ScanLibraries(context.Context) error {
	f.scans.Add(1)
	return nil
}

func TestPhoneLibraryMetadataReview(t *testing.T) {
	if target := nsgotest.Target(); target != "" && target != "linux" {
		t.Skip("design comparisons use the Linux GTK backend")
	}
	entry := source{ID: "library", Name: "Fixture", Address: "https://library.example.test", Provider: media.Emby,
		Libraries: []sourceLibrary{
			{Library: media.Library{ID: "movies", Name: "Movies"}, Category: libraryDocumentaries, Hidden: true},
			{Library: media.Library{ID: "shows", Name: "Shows"}, Hidden: true},
		}}
	config, _ := json.Marshal([]source{entry})
	for _, theme := range reviewAppearances {
		t.Run(theme.name, func(t *testing.T) {
			height := core.Points(1400)
			if nsgotest.Target() == "linux" {
				height += 37
			}
			server := &librarySettingsFixture{fixtureServer: fixtureServer{session: media.Session{Token: "fixture-token"}}}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				shellIdiom.Provide(cx, core.IdiomPhone)
				cx.SetSubtreeAppearance(theme.appearance)
				return appRoot(cx, func(media.Provider, string, media.Device, media.Session) (media.Client, error) { return server, nil }, nil)
			}, nsgotest.Size(390, height), nsgotest.Environment(core.Environment{Locale: "zh-Hans"}),
				nsgotest.Storage(map[string]string{sourcesKey: string(config),
					libraryPreferencesKey: `{"Language":"zh-CN","RefreshMinutes":30}`}),
				nsgotest.Secrets(map[string]string{entry.ID: "fixture-token"}))
			app.Press("id:tab-settings")
			app.Press("id:open-library-settings")
			app.WaitFor("id:phone-library-settings", time.Second)
			if nsgotest.Target() == "linux" {
				app.Advance(350 * time.Millisecond)
				app.Screenshot("settings-library-" + theme.name)
			}
			if theme.name == "dark" {
				return
			}
			app.Press("id:metadata-server-first")
			app.Press("id:metadata-progress-sync")
			app.Press("id:metadata-language")
			app.WaitFor("role:option 日本語", time.Second)
			app.Press("role:option 日本語")
			app.Press("id:metadata-hidden-libraries")
			app.WaitFor("id:show-library-library-movies", time.Second)
			app.Press("id:show-library-library-movies")
			app.WaitForGone("id:show-library-library-movies", time.Second)
			app.Press("id:metadata-close-dialog")
			app.WaitForGone("id:metadata-dialog", time.Second)
			app.Press("id:metadata-tmdb")
			app.WaitFor("id:metadata-tmdb-token", time.Second)
			app.Type("id:metadata-tmdb-token", "tmdb-fixture-secret")
			app.Press("id:metadata-tmdb-save")
			app.WaitFor("id:metadata-tmdb-remove", time.Second)
			app.Press("id:metadata-close-dialog")
			app.WaitForGone("id:metadata-dialog", time.Second)
			if prefs := readLibraryPreferences(app.Storage()); !prefs.TMDB || !prefs.PreferExternal || !prefs.DisableProgress || prefs.Language != "ja-JP" {
				t.Fatalf("saved preferences = %+v", prefs)
			}
			for _, key := range app.Storage().Keys() {
				if value, _ := app.Storage().GetItem(key); strings.Contains(value, "tmdb-fixture-secret") {
					t.Fatal("TMDB credential was written to regular storage")
				}
			}
			libraries := func() []sourceLibrary {
				var saved []source
				raw, _ := app.Storage().GetItem(sourcesKey)
				if err := json.Unmarshal([]byte(raw), &saved); err != nil || len(saved) != 1 {
					t.Fatalf("sources = %+v, %v", saved, err)
				}
				return saved[0].Libraries
			}
			app.Press("id:metadata-rescan")
			app.WaitFor("id:metadata-confirm-scan", time.Second)
			app.Advance(350 * time.Millisecond)
			app.Press("id:metadata-confirm-scan")
			app.Advance(350 * time.Millisecond)
			app.WaitFor("id:metadata-scan-finished", 3*time.Second)
			if got := libraries(); len(got) != 3 || got[0].Hidden || got[0].Category != libraryDocumentaries || !got[1].Hidden || !got[2].Hidden {
				t.Fatalf("scan changed imported library preferences: %+v", got)
			}
			if server.scans.Load() != 1 {
				t.Fatalf("server scan calls = %d", server.scans.Load())
			}
			app.Press("id:metadata-cache-clear")
			app.WaitFor("id:metadata-confirm-clear", time.Second)
			app.Advance(350 * time.Millisecond)
			app.Press("id:metadata-confirm-clear")
			app.Advance(350 * time.Millisecond)
			app.WaitFor("id:metadata-cache-cleared", time.Second)
			if nsgotest.Target() == "" {
				app.FastForward(16 * time.Second)
				app.WaitUntil(time.Second, func() bool { return server.reads.Load() >= 3 })
				before := server.reads.Load()
				app.FastForward(30 * time.Minute)
				app.WaitUntil(time.Second, func() bool { return server.reads.Load() > before })
				if server.scans.Load() != 1 {
					t.Fatal("automatic refresh triggered another server scan")
				}
			}
		})
	}
}

type localProgressFixture struct {
	offlineFixture
	reports atomic.Int32
}

func (f *localProgressFixture) Report(context.Context, media.PlaybackState) error {
	f.reports.Add(1)
	return nil
}

func TestMetadataPlaybackRemainsLocalWhenSyncDisabled(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("uses playback clock fixtures")
	}
	entry := source{ID: "local", Name: "Fixture", Address: "https://offline.example.test", Provider: media.Emby, AccountID: "viewer",
		Libraries: []sourceLibrary{{Library: media.Library{ID: "movies", Name: "Movies"}}}}
	config, _ := json.Marshal([]source{entry})
	server := &localProgressFixture{offlineFixture: offlineFixture{restored: make(chan media.OfflineProgress, 2)}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shellIdiom.Provide(cx, core.IdiomPhone)
		return appRoot(cx, func(media.Provider, string, media.Device, media.Session) (media.Client, error) { return server, nil }, nil)
	}, nsgotest.Offscreen(), nsgotest.Size(390, 1400), nsgotest.Environment(core.Environment{Locale: "en"}),
		nsgotest.Storage(map[string]string{sourcesKey: string(config)}), nsgotest.Secrets(map[string]string{entry.ID: "fixture-token"}))
	settings := func() {
		app.Press("id:tab-settings")
		app.Press("id:open-library-settings")
		app.Press("id:metadata-progress-sync")
		app.Press("id:back")
		app.Press("id:tab-home")
	}
	settings()
	app.WaitFor("id:home-hero-details", time.Second)
	app.Press("id:home-hero-details")
	app.WaitFor("id:detail-play", time.Second)
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	app.Play("id:video-player", core.VideoInfo{Duration: 2 * time.Minute})
	const at = 61 * time.Second
	app.Progress("id:video-player", at, 2*time.Minute)
	app.PauseVideo("id:video-player", core.VideoPauseControls)
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", time.Second)
	checkpoint := (pendingProgress{Account: mediaAccountKey(entry), Update: media.OfflineProgress{ItemID: "test-film"}}).key()
	raw, _ := app.Storage().GetItem(checkpoint)
	var saved pendingProgress
	if json.Unmarshal([]byte(raw), &saved) != nil || !saved.LocalOnly || saved.Update.Position != at {
		t.Fatalf("local checkpoint = %+v", saved)
	}
	app.Press("id:detail-play")
	app.WaitFor("id:video-player", time.Second)
	app.Play("id:video-player", core.VideoInfo{Duration: 2 * time.Minute})
	if !slices.Contains(app.FindMounted("id:video-player").Seeks, at) {
		t.Fatal("local progress did not restore from the details page")
	}
	app.Press("id:close-player")
	app.WaitFor("id:detail-play", time.Second)
	app.Press("id:back")
	settings()
	app.FastForward(2 * time.Second)
	if server.reports.Load() != 0 || len(server.restored) != 0 {
		t.Fatal("progress captured during opt-out reached the server")
	}
}

func TestDockerLibraryMetadataScans(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SCAN") != "1" {
		t.Skip("requires dedicated Docker media servers that can rescan test content")
	}
	config, secrets := homeReviewSources(t)
	var sources []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &sources); err != nil {
		t.Fatal(err)
	}
	for _, entry := range sources {
		t.Run(string(entry.Provider), func(t *testing.T) {
			client, err := media.NewClient(entry.Provider, entry.Address, media.NewDevice("Waygu rescan test"),
				media.Session{UserID: entry.AccountID, Token: secrets[entry.ID]})
			if err != nil {
				t.Fatal(err)
			}
			scanner := client.(media.LibraryScanClient)
			if err := scanner.ScanLibraries(t.Context()); err != nil {
				t.Fatal(err)
			}
			libraries, err := client.Libraries(t.Context())
			if err != nil || len(libraries) == 0 {
				t.Fatalf("libraries after scan request = %+v, %v", libraries, err)
			}
		})
	}
	client, _ := media.NewClient(media.Flymoo, "http://127.0.0.1:1", media.NewDevice("fixture"), media.Session{})
	if _, found := client.(media.LibraryScanClient); found {
		t.Fatal("Flymoo scan was exposed without a supported protocol")
	}
}
