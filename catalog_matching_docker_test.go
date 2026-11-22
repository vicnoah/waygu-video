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

	"waygu-video/internal/media"
)

func TestDockerCrossSourceTitles(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("start the integration/servers Docker fixtures and set WAYGU_TEST_SERVERS=1")
	}
	password := os.Getenv("WAYGU_TEST_PASSWORD")
	if password == "" {
		password = "waygu-fixture-local"
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	device := media.NewDevice("Waygu source matching test")
	secrets := map[string]string{}
	sources := []source{
		{ID: "jellyfin", Name: "Jellyfin fixture", Provider: media.Jellyfin, Address: "http://127.0.0.1:18096"},
		{ID: "emby", Name: "Emby fixture", Provider: media.Emby, Address: "http://127.0.0.1:28096"},
		{ID: "plex", Name: "Plex fixture", Provider: media.Plex, Address: "http://127.0.0.1:32400"},
	}
	var movies []media.Item
	var resumed []media.Item
	var clients []media.Client
	for i := range sources {
		entry := &sources[i]
		guest, err := media.NewClient(entry.Provider, entry.Address, device, media.Session{})
		if err != nil {
			t.Fatal(err)
		}
		session, err := guest.SignIn(ctx, media.Credentials{Username: "waygu-test", Password: password})
		if err != nil {
			t.Fatal(entry.Provider, "sign in:", err)
		}
		secrets[entry.ID] = session.Token
		entry.AccountID = session.UserID
		client, err := media.NewClient(entry.Provider, entry.Address, device, session)
		if err != nil {
			t.Fatal(err)
		}
		libraries, err := client.Libraries(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, library := range libraries {
			if library.Name == "Movies" {
				entry.Libraries = []sourceLibrary{{Library: library}}
			}
		}
		if len(entry.Libraries) != 1 {
			t.Fatal(entry.Provider, "fixture movie library not found")
		}
		page, err := client.Catalog(ctx, media.Query{LibraryID: entry.Libraries[0].ID, Search: "Waygu Test Movie"})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("%s fixture identity missing: %v", entry.Provider, err)
		}
		wantIMDb := "tt90000002"
		if entry.Provider == media.Plex {
			wantIMDb = "" // The unclaimed local agent only assigns a Plex-local GUID to the sample.
		}
		if identityOf(page.Items[0]).imdb != wantIMDb {
			t.Fatalf("%s did not return the expected fixture identity", entry.Provider)
		}
		movies = append(movies, page.Items[0])
		clients = append(clients, client)
		if entry.Provider != media.Plex {
			page, err := client.Catalog(ctx, media.Query{LibraryID: entry.Libraries[0].ID, Search: "Waygu Resume Across Sources"})
			if err != nil || len(page.Items) != 1 || identityOf(page.Items[0]).imdb != "tt90000003" {
				t.Fatalf("%s cross-source resume fixture missing: %v", entry.Provider, err)
			}
			resumed = append(resumed, page.Items[0])
		}
	}
	older := setDockerCopyProgress(t, ctx, clients[0], resumed[0], 82*time.Second)
	// Replay the shorter copy afterward so order cannot be inferred from progress.
	time.Sleep(1100 * time.Millisecond)
	later := setDockerCopyProgress(t, ctx, clients[1], resumed[1], 34*time.Second)
	if !later.After(older) {
		t.Fatalf("expected newer Emby date, got Jellyfin %s and Emby %s", older, later)
	}
	encoded, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	reported := make(chan dockerCopyReport, 10)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return appRoot(cx, func(provider media.Provider, address string, device media.Device, session media.Session) (media.Client, error) {
			client, err := media.NewClient(provider, address, device, session)
			if err != nil {
				return nil, err
			}
			return dockerCopyClient{Client: client, VersionClient: client.(media.VersionClient), provider: provider, reports: reported}, nil
		}, nil)
	}, nsgotest.Size(960, 850), nsgotest.Offscreen(), nsgotest.Secrets(secrets),
		nsgotest.Storage(map[string]string{sourcesKey: string(encoded), deviceKey: device.ID}))
	newerResume := "id:resume-emby-" + resumed[1].ID
	app.WaitFor(newerResume, 5*time.Second)
	app.WaitForGone("id:resume-jellyfin-"+resumed[0].ID, time.Second)
	app.Press(newerResume)
	app.WaitFor("id:video-player", 5*time.Second)
	app.Play("id:video-player", core.VideoInfo{Duration: 6 * time.Minute})
	if player := app.FindMounted("id:video-player"); !slices.Contains(player.Seeks, 34*time.Second) {
		t.Fatalf("latest copy did not resume at 34 seconds: %v", player.Seeks)
	}
	app.Press("id:close-player")
	expectDockerCopyStop(t, reported, media.Emby, resumed[1].ID, 34*time.Second)

	preferred, alternate := "id:title-jellyfin-"+movies[0].ID, "id:title-emby-"+movies[1].ID
	app.Press("role:tab Library")
	app.WaitFor(preferred, 5*time.Second)
	app.WaitFor("id:title-plex-"+movies[2].ID, 5*time.Second)
	app.WaitForGone(alternate, time.Second)
	app.Press(preferred)
	app.WaitFor("id:playback-source", 5*time.Second)
	app.Press("id:playback-source")
	app.Press("id:playback-source-1")
	app.Press("id:detail-play")
	video := app.WaitFor("id:video-player", 5*time.Second)
	dimensions, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=height",
		"-of", "csv=p=0", video.Video.Source).Output()
	if err != nil || strings.TrimSpace(string(dimensions)) != "360" {
		t.Fatalf("could not decode the Emby file: %v", err)
	}
	app.Play("id:video-player", core.VideoInfo{Duration: 18 * time.Second})
	app.Progress("id:video-player", 8*time.Second, 18*time.Second)
	app.Press("id:close-player")
	expectDockerCopyStop(t, reported, media.Emby, movies[1].ID, 8*time.Second)

	app.Press("id:back")
	app.Press("role:tab Settings")
	app.Press("id:move-source-up-emby")
	app.Press("role:tab Library")
	app.WaitFor(alternate, 5*time.Second)
	app.WaitForGone(preferred, time.Second)
	app.Press("role:tab Settings")
	app.Press("id:merge-identical-titles")
	app.Press("role:tab Library")
	app.WaitFor(preferred, 5*time.Second)
	app.WaitFor(alternate, 5*time.Second)
}

func setDockerCopyProgress(t *testing.T, ctx context.Context, client media.Client, item media.Item, position time.Duration) time.Time {
	t.Helper()
	// Some servers report LastPlayed with second precision. Poll past
	// any older row left by an earlier run with the same position.
	reportedAt := time.Now().UTC().Truncate(time.Second)
	stream, err := client.Playback(ctx, item, media.PlaybackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []media.PlaybackState{{Event: media.Started, Stream: stream},
		{Event: media.Progress, Stream: stream, Position: position}, {Event: media.Stopped, Stream: stream, Position: position}} {
		if err := client.Report(ctx, state); err != nil {
			t.Fatal(err)
		}
	}
	for tries := 0; tries < 25 && ctx.Err() == nil; tries++ {
		items, err := client.Resume(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range items {
			if row.ID == item.ID && row.Position == position && !row.LastPlayed.Before(reportedAt) {
				return row.LastPlayed
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("server did not confirm the new resume position and date")
	return time.Time{}
}

func expectDockerCopyStop(t *testing.T, reported <-chan dockerCopyReport, provider media.Provider, id string, position time.Duration) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case report := <-reported:
			if report.provider != provider || report.state.Stream.ItemID != id || report.err != nil {
				t.Fatalf("wrong target for playback progress: %s, %v", report.provider, report.err)
			}
			if report.state.Event == media.Stopped {
				if report.state.Position != position {
					t.Fatalf("Emby received progress %s", report.state.Position)
				}
				return
			}
		case <-deadline:
			t.Fatal("Emby did not receive the selected copy's playback progress")
		}
	}
}

type dockerCopyReport struct {
	provider media.Provider
	state    media.PlaybackState
	err      error
}

type dockerCopyClient struct {
	media.Client
	media.VersionClient
	provider media.Provider
	reports  chan dockerCopyReport
}

func (c dockerCopyClient) Report(ctx context.Context, state media.PlaybackState) error {
	err := c.Client.Report(ctx, state)
	c.reports <- dockerCopyReport{c.provider, state, err}
	return err
}
