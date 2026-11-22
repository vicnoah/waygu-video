package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
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
	deadline := time.After(5 * time.Second)
	stopped := false
	for !stopped {
		select {
		case report := <-reported:
			if report.provider != media.Emby || report.state.Stream.ItemID != movies[1].ID || report.err != nil {
				t.Fatalf("wrong target for playback progress: %s, %v", report.provider, report.err)
			}
			if report.state.Event == media.Stopped {
				stopped = true
				if report.state.Position != 8*time.Second {
					t.Fatalf("Emby received progress %s", report.state.Position)
				}
			}
		case <-deadline:
			t.Fatal("Emby did not receive the selected copy's playback progress")
		}
	}

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
