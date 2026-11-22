package media

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDockerServers(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("start the integration/servers Docker fixtures and set WAYGU_TEST_SERVERS=1")
	}
	password := os.Getenv("WAYGU_TEST_PASSWORD")
	if password == "" {
		password = "waygu-fixture-local"
	}
	for _, server := range []struct {
		kind    Provider
		address string
	}{
		{Jellyfin, "http://127.0.0.1:18096"},
		{Emby, "http://127.0.0.1:28096"},
		{Plex, "http://127.0.0.1:32400"},
	} {
		t.Run(string(server.kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			device := NewDevice("Waygu integration tests")
			guest, err := NewClient(server.kind, server.address, device, Session{})
			if err != nil {
				t.Fatal(err)
			}
			info, err := guest.Probe(ctx)
			if err != nil || info.ID == "" || info.Version == "" {
				t.Fatalf("probe: %+v, %v", info, err)
			}
			t.Logf("server version: %s", info.Version)
			session, err := guest.SignIn(ctx, Credentials{Username: "waygu-test", Password: password})
			if err != nil {
				t.Fatal("sign in:", err)
			}
			client, err := NewClient(server.kind, server.address, device, session)
			if err != nil {
				t.Fatal(err)
			}
			libraries, err := client.Libraries(ctx)
			if err != nil {
				t.Fatal("libraries:", err)
			}
			var movieLibrary, showLibrary string
			for _, library := range libraries {
				switch library.Name {
				case "Movies":
					movieLibrary = library.ID
				case "Shows":
					showLibrary = library.ID
				}
			}
			if movieLibrary == "" || showLibrary == "" {
				t.Fatalf("fixture libraries missing: %+v", libraries)
			}
			movie := findDockerItem(t, ctx, client, Query{LibraryID: movieLibrary}, Movie, "Waygu Test Movie")
			findDockerItem(t, ctx, client, Query{Search: "Waygu Test Movie"}, Movie, "Waygu Test Movie")
			movie, err = client.Detail(ctx, movie)
			if err != nil || movie.Duration < 17*time.Second || movie.Duration > 19*time.Second {
				t.Fatalf("movie detail duration: %v, %v", movie.Duration, err)
			}
			show := findDockerItem(t, ctx, client, Query{LibraryID: showLibrary}, Series, "Waygu Test Series")
			seasons, err := client.Children(ctx, show)
			if err != nil || len(seasons) != 1 || seasons[0].Kind != Season || seasons[0].Season != 1 {
				t.Fatalf("seasons: %+v, %v", seasons, err)
			}
			episodes, err := client.Children(ctx, seasons[0])
			if err != nil || len(episodes) != 1 || episodes[0].Kind != Episode || episodes[0].Episode != 1 {
				t.Fatalf("episodes: %+v, %v", episodes, err)
			}
			stream, err := client.Playback(ctx, movie, PlaybackOptions{})
			if err != nil || stream.Method != DirectPlay {
				t.Fatalf("playback: method %s, %v", stream.Method, err)
			}
			verifyDockerMP4(t, ctx, stream)
			verifyDockerResume(t, ctx, client, movieLibrary)
		})
	}
}

func verifyDockerResume(t *testing.T, ctx context.Context, client Client, movieLibrary string) {
	t.Helper()
	movie := findDockerItem(t, ctx, client, Query{LibraryID: movieLibrary}, Movie, "Waygu Resume Test")
	stream, err := client.Playback(ctx, movie, PlaybackOptions{})
	if err != nil || stream.Duration < 350*time.Second {
		t.Fatalf("resume fixture: duration %v, %v", stream.Duration, err)
	}
	for _, event := range []PlaybackState{
		{Event: Started, Stream: stream, Position: 0},
		{Event: Progress, Stream: stream, Position: 63 * time.Second, Paused: true},
		{Event: Stopped, Stream: stream, Position: 64 * time.Second},
	} {
		if err := client.Report(ctx, event); err != nil {
			t.Fatalf("report %s: %v", event.Event, err)
		}
	}
	wait := time.NewTicker(200 * time.Millisecond)
	defer wait.Stop()
	deadline := time.After(6 * time.Second)
	for {
		recent, err := client.Resume(ctx)
		if err != nil {
			t.Fatal("continue watching:", err)
		}
		for _, item := range recent {
			if item.ID == movie.ID && item.Position >= 63*time.Second && item.Position <= 65*time.Second {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatal("continue watching did not return the reported position")
		case <-wait.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func findDockerItem(t *testing.T, ctx context.Context, client Client, query Query, kind Kind, title string) Item {
	t.Helper()
	page, err := client.Catalog(ctx, query)
	if err != nil {
		t.Fatal("catalog:", err)
	}
	for _, item := range page.Items {
		if item.Kind == kind && strings.HasPrefix(item.Title, title) {
			return item
		}
	}
	t.Fatalf("catalog entry %q missing (returned %d items)", title, len(page.Items))
	return Item{}
}

func verifyDockerMP4(t *testing.T, ctx context.Context, stream Stream) {
	t.Helper()
	for _, selection := range []string{"bytes=0-127", "bytes=128-255"} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, stream.URL, nil)
		if err != nil {
			t.Fatal("invalid stream address")
		}
		for key, values := range stream.Headers {
			request.Header[key] = values
		}
		request.Header.Set("Range", selection)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal("failed to read stream")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 129))
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusPartialContent || len(data) != 128 {
			t.Fatalf("%s: status %d, bytes %d, %v", selection, response.StatusCode, len(data), err)
		}
		if selection == "bytes=0-127" && !bytes.Equal(data[4:8], []byte("ftyp")) {
			t.Fatal("stream does not start with an MP4 header")
		}
	}
}
