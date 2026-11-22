package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"waygu-video/internal/danmaku"
	"waygu-video/internal/playback"
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
			if server.kind == Jellyfin {
				verifyDockerQuickConnect(t, ctx, server.address, client.(*mediaBrowser), session.UserID)
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
			digest, size, err := danmaku.Fingerprint(ctx, stream.File.URL, stream.File.Headers)
			if err != nil || len(digest) != 32 || size <= 0 || stream.File.Name == "" {
				t.Fatalf("fingerprint the original video: bytes=%d, %v", size, err)
			}
			verifyDockerHLS(t, ctx, client, movie, digest)
			verifyDockerResume(t, ctx, client, movieLibrary, showLibrary)
			verifyDockerTracks(t, ctx, client, movieLibrary, server.kind)
			verifyDockerVersions(t, ctx, client, movieLibrary)
			verifyDockerPersonal(t, ctx, client, Library{ID: movieLibrary, Kind: "movie"}, movie)
		})
	}
}

func verifyDockerPersonal(t *testing.T, ctx context.Context, client Client, library Library, movie Item) {
	t.Helper()
	query := Query{LibraryID: library.ID, Limit: 24}
	contains := func(page Page, id string) bool {
		return slices.ContainsFunc(page.Items, func(item Item) bool { return item.ID == id })
	}
	if favorites, ok := client.(FavoriteClient); ok {
		t.Cleanup(func() {
			restore, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := favorites.SetFavorite(restore, movie, movie.Favorite); err != nil {
				t.Error("restore favorite fixture:", err)
			}
		})
		for _, value := range []bool{true, false} {
			if err := favorites.SetFavorite(ctx, movie, value); err != nil {
				t.Fatal("set favorite:", err)
			}
			detail, err := client.Detail(ctx, movie)
			if err != nil || detail.Favorite != value {
				t.Fatalf("favorite in item detail = %v, want %v: %v", detail.Favorite, value, err)
			}
			list, err := favorites.Favorites(ctx, query)
			if err != nil || contains(list, movie.ID) != value {
				t.Fatalf("favorite list membership should be %v: %v", value, err)
			}
		}
	}
	watch := client.(WatchedClient)
	t.Cleanup(func() {
		restore, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := watch.SetWatched(restore, movie, movie.Watched); err != nil {
			t.Error("restore watched fixture:", err)
		}
	})
	for _, value := range []bool{true, false} {
		if err := watch.SetWatched(ctx, movie, value); err != nil {
			t.Fatal("set watched:", err)
		}
		detail, err := client.Detail(ctx, movie)
		if err != nil || detail.Watched != value || detail.Position != 0 {
			t.Fatalf("read watched flag = %v, position = %s, want %v and 0: %v", detail.Watched, detail.Position, value, err)
		}
		if value {
			history, err := client.(HistoryClient).History(ctx, library, query)
			if err != nil || !contains(history, movie.ID) {
				t.Fatalf("marked title missing from watch history: %v", err)
			}
		}
	}
}

func verifyDockerVersions(t *testing.T, ctx context.Context, client Client, library string) {
	t.Helper()
	item := findDockerItem(t, ctx, client, Query{LibraryID: library}, Movie, "Waygu Version Test")
	versions, err := client.(VersionClient).Versions(ctx, item)
	if err != nil || len(versions) != 2 {
		t.Fatalf("list grouped versions: got %d versions, %v", len(versions), err)
	}
	slices.SortFunc(versions, func(a, b Version) int { return a.Height - b.Height })
	low, high := versions[0], versions[1]
	if low.Height != 180 || high.Height != 360 || low.ID == high.ID || low.ID == "" || high.ID == "" ||
		low.Bitrate <= 0 || high.Bitrate <= low.Bitrate {
		t.Fatalf("incorrect version metadata: %+v", versions)
	}
	for _, check := range []struct {
		options PlaybackOptions
		want    Version
	}{
		{PlaybackOptions{SourceID: low.ID, VersionPolicy: VersionHighest}, low},
		{PlaybackOptions{SourceID: high.ID}, high},
		{PlaybackOptions{VersionPolicy: VersionHighest}, high},
		{PlaybackOptions{VersionPolicy: VersionFitBitrate, MaxBitrate: (low.Bitrate + high.Bitrate) / 2}, low},
	} {
		stream, err := client.Playback(ctx, item, check.options)
		if err != nil || stream.SourceID != check.want.ID {
			t.Fatalf("open %dp version: %v", check.want.Height, err)
		}
		if check.options.SourceID == "" {
			continue
		}
		address, closeProxy, err := playback.Open(stream.URL, stream.Headers)
		if err != nil {
			t.Fatal("proxy the selected version:", err)
		}
		data, probeErr := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
			"-show_entries", "stream=height", "-of", "csv=p=0", address).Output()
		closeProxy()
		if probeErr != nil || strings.TrimSpace(string(data)) != strconv.Itoa(check.want.Height) {
			t.Fatalf("selected version decoded as %q, want %dp: %v", strings.TrimSpace(string(data)), check.want.Height, probeErr)
		}
	}
}

func verifyDockerTracks(t *testing.T, ctx context.Context, client Client, library string, provider Provider) {
	t.Helper()
	movie := findDockerItem(t, ctx, client, Query{LibraryID: library}, Movie, "Waygu Track Test")
	stream, err := client.Playback(ctx, movie, PlaybackOptions{})
	if err != nil {
		t.Fatal("load tracks:", err)
	}
	versions, err := client.(VersionClient).Versions(ctx, movie)
	if err != nil || len(versions) != 1 || versions[0].Container == "" || versions[0].Height != 180 || len(versions[0].Tracks) != len(stream.Tracks) {
		t.Fatalf("load track metadata for the detail page: %d versions, %v", len(versions), err)
	}
	var secondAudio *Track
	var availableCaptions []Track
	for _, track := range stream.Tracks {
		switch {
		case track.Type == "audio" && (track.Language == "jpn" || track.Language == "ja" || strings.Contains(track.Title, "Japanese")):
			secondAudio = &track
		case track.Type == "subtitle" && track.URL != "":
			availableCaptions = append(availableCaptions, track)
		}
	}
	if secondAudio == nil {
		t.Fatal("the Japanese audio track was not listed")
	}
	var checkedEmbedded, checkedExternal bool
	for _, track := range availableCaptions {
		cues, err := playback.FetchCaptions(ctx, track.URL)
		if err != nil || cues.At(2*time.Second) != "Waygu track selection fixture" {
			t.Fatalf("read subtitle track %s: %v", track.ID, err)
		}
		if track.External {
			checkedExternal = true
		} else {
			checkedEmbedded = true
		}
	}
	if !checkedExternal || (provider != Plex && !checkedEmbedded) {
		t.Fatal("the expected text subtitle tracks were not offered")
	}

	index, err := strconv.Atoi(secondAudio.ID)
	if err != nil {
		t.Fatal("invalid audio track ID")
	}
	off := -1
	alternate, err := client.Playback(ctx, movie, PlaybackOptions{SourceID: stream.SourceID, AudioIndex: &index, SubtitleIndex: &off})
	if err != nil || alternate.Method != Transcode || !slices.ContainsFunc(alternate.Tracks, func(track Track) bool {
		return track.Type == "audio" && track.ID == secondAudio.ID && track.Selected
	}) {
		t.Fatalf("switch to the Japanese track: method %s, %v", alternate.Method, err)
	}
	address, closeProxy, err := playback.Open(alternate.URL, alternate.Headers)
	if err != nil {
		t.Fatal("proxy the selected audio stream:", err)
	}
	defer closeProxy()
	samples, err := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-loglevel", "error", "-i", address,
		"-map", "0:a:0", "-ss", "1", "-t", "1", "-ac", "1", "-ar", "8000", "-f", "s16le", "pipe:1").Output()
	if err != nil || len(samples) < 8000 {
		t.Fatalf("decode selected audio: %v", err)
	}
	var changes int
	for i := 2; i+1 < len(samples); i += 2 {
		before, after := int16(binary.LittleEndian.Uint16(samples[i-2:])), int16(binary.LittleEndian.Uint16(samples[i:]))
		if before < 0 && after >= 0 {
			changes++
		}
	}
	hertz := changes * 16000 / len(samples)
	if hertz < 820 || hertz > 940 {
		t.Fatalf("selected audio measured %d Hz, expected the 880 Hz Japanese track", hertz)
	}
}

func verifyDockerHLS(t *testing.T, ctx context.Context, client Client, movie Item, originalDigest string) {
	t.Helper()
	stream, err := client.Playback(ctx, movie, PlaybackOptions{MaxBitrate: 300_000})
	if err != nil || stream.Method != Transcode {
		t.Fatalf("300 kbps playback: method %s, %v", stream.Method, err)
	}
	if digest, _, err := danmaku.Fingerprint(ctx, stream.File.URL, stream.File.Headers); err != nil || digest != originalDigest {
		t.Fatalf("transcoding did not retain access to the original video: %v", err)
	}
	address, stop, err := playback.Open(stream.URL, stream.Headers)
	if err != nil {
		t.Fatal("could not open the local playback proxy")
	}
	defer stop()
	for depth := 0; depth < 4; depth++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			t.Fatal("invalid HLS resource address")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal("failed to read the HLS resource")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("HLS resource %d: status %d, %v", depth, response.StatusCode, err)
		}
		if depth > 0 && len(data) >= 376 && data[0] == 0x47 && data[188] == 0x47 {
			return
		}
		if !bytes.HasPrefix(data, []byte("#EXTM3U")) {
			t.Fatalf("resource %d is neither an HLS playlist nor an MPEG-TS segment (%q)", depth, response.Header.Get("Content-Type"))
		}
		var reference string
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				reference = line
				break
			}
		}
		if reference == "" {
			t.Fatal("playlist contains no media resources")
		}
		resolved, err := request.URL.Parse(reference)
		if err != nil || !sameOrigin(request.URL, resolved) {
			t.Fatal("playlist resource did not stay on the playback proxy")
		}
		address = resolved.String()
	}
	t.Fatal("no playable segment found in the HLS playlists")
}

func verifyDockerQuickConnect(t *testing.T, ctx context.Context, address string, authorized *mediaBrowser, userID string) {
	t.Helper()
	client, err := NewClient(Jellyfin, address, NewDevice("Waygu Quick Connect fixture"), Session{})
	if err != nil {
		t.Fatal(err)
	}
	guest := client.(QuickConnectClient)
	code, err := guest.BeginQuickConnect(ctx)
	if err != nil || len(code.Code) != 6 {
		t.Fatalf("quick connect code: %v", err)
	}
	if _, done, err := guest.CheckQuickConnect(ctx, code); err != nil || done {
		t.Fatalf("pending authorization: %v, %v", done, err)
	}
	if err := authorized.post(ctx, "QuickConnect/Authorize", url.Values{"code": {code.Code}}, nil, nil); err != nil {
		t.Fatalf("approve quick connect: %v", err)
	}
	session, done, err := guest.CheckQuickConnect(ctx, code)
	if err != nil || !done || session.UserID != userID || session.Token == "" {
		t.Fatalf("exchange quick connect: %v", err)
	}
}

func verifyDockerResume(t *testing.T, ctx context.Context, client Client, movieLibrary, showLibrary string) {
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
				if item.LastPlayed.IsZero() {
					t.Fatal("continue watching did not return the fixture's last-played date")
				}
				if scoped, ok := client.(LibraryResumeClient); ok {
					movies, err := scoped.ResumeLibrary(ctx, movieLibrary)
					if err != nil || !slices.ContainsFunc(movies, func(item Item) bool { return item.ID == movie.ID }) {
						t.Fatalf("movie library resume did not include the fixture: %v", err)
					}
					shows, err := scoped.ResumeLibrary(ctx, showLibrary)
					if err != nil || slices.ContainsFunc(shows, func(item Item) bool { return item.ID == movie.ID }) {
						t.Fatalf("show library resume returned a movie from another library: %v", err)
					}
				} else if item.LibraryID != movieLibrary {
					t.Fatalf("Plex resume library ID is %q, want %q", item.LibraryID, movieLibrary)
				}
				verifyDockerOfflineProgress(t, ctx, client, item, stream)
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

func verifyDockerOfflineProgress(t *testing.T, ctx context.Context, client Client, item Item, stream Stream) {
	t.Helper()
	syncer := client.(OfflineProgressClient)
	update := OfflineProgress{ItemID: item.ID, MediaID: stream.SourceID, Duration: stream.Duration,
		Previous: item.Position, Position: 89 * time.Second, At: item.LastPlayed.Add(2 * time.Second)}
	if err := syncer.SyncOfflineProgress(ctx, update); err != nil {
		t.Fatal("restore offline progress:", err)
	}
	read := func() Item {
		t.Helper()
		items, err := client.Resume(ctx)
		if err != nil {
			t.Fatal("continue watching after recovery:", err)
		}
		for _, row := range items {
			if row.ID == item.ID {
				return row
			}
		}
		t.Fatal("the restored item disappeared from continue watching")
		return Item{}
	}
	restored := read()
	if restored.Position != update.Position || restored.LastPlayed.Before(item.LastPlayed) {
		t.Fatalf("offline progress = %v at %v, previously %v at %v, want %v",
			restored.Position, restored.LastPlayed, item.Position, item.LastPlayed, update.Position)
	}
	update.Position, update.Previous, update.At = 121*time.Second, restored.Position, item.LastPlayed.Add(-time.Second)
	if err := syncer.SyncOfflineProgress(ctx, update); err != nil {
		t.Fatal("compare stale offline progress:", err)
	}
	if current := read(); current.Position != restored.Position {
		t.Fatalf("a delayed checkpoint replaced the newer server position: got %v, want %v", current.Position, restored.Position)
	}
}

func findDockerItem(t *testing.T, ctx context.Context, client Client, query Query, kind Kind, title string) Item {
	t.Helper()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(20 * time.Second)
	for {
		page, err := client.Catalog(ctx, query)
		if err != nil {
			t.Fatal("catalog:", err)
		}
		for _, item := range page.Items {
			if item.Kind == kind && strings.HasPrefix(item.Title, title) {
				return item
			}
		}
		select {
		case <-deadline:
			t.Fatalf("catalog entry %q missing (returned %d items)", title, len(page.Items))
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
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
