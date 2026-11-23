package downloads

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"waygu-video/internal/mediahttp"
)

func TestTransferResumesVerifiedFile(t *testing.T) {
	contents := bytes.Repeat([]byte("test video contents"), 36_000)
	var calls atomic.Int32
	const eTag = `"same-file"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-fixture" {
			t.Error("original file request lost its media authorization")
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("ETag", eTag)
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)))
			w.Header().Set("Content-Length", strconv.Itoa(len(contents)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(contents[:256<<10])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if calls.Load() == 2 {
			if r.Header.Get("Range") != "bytes=131072-" || r.Header.Get("If-Range") != eTag {
				t.Errorf("download did not request the verified continuation: %s", r.Header.Get("Range"))
			}
			checksum := md5.Sum(contents[128<<10:])
			w.Header().Set("Content-MD5", base64.StdEncoding.EncodeToString(checksum[:]))
		}
		http.ServeContent(w, r, "fixture.mp4", time.Unix(1, 0), bytes.NewReader(contents))
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	store := &Store{Dir: dir}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture")))
	src := Source{Resource: mediahttp.Resource{URL: server.URL, Headers: http.Header{"Authorization": {"Bearer local-fixture"}}}, Name: "fixture.mp4", MediaID: "first"}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	_, err := store.Transfer(ctx, id, src, Limits{}, func(actual, _ int64) {
		if actual > 0 {
			stop()
		}
	})
	if err == nil || store.Bytes(id) != 128<<10 {
		t.Fatalf("paused transfer: %v, %d bytes", err, store.Bytes(id))
	}
	if _, _, err := store.Open(id); err == nil {
		t.Fatal("partial file became playable")
	}

	store = &Store{Dir: dir}
	file, err := store.Transfer(t.Context(), id, src, Limits{}, nil)
	if err != nil || file.SHA256 != fmt.Sprintf("%x", sha256.Sum256(contents)) {
		t.Fatalf("completed file did not pass the checksum: %v", err)
	}
	path, restored, err := store.Open(id)
	if err != nil || restored.Size != int64(len(contents)) || restored.MediaID != src.MediaID {
		t.Fatalf("reopen downloaded file: %+v, %v", restored, err)
	}
	payload, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(payload, contents) {
		t.Fatal("downloaded bytes differ from the source")
	}
	tooLittle := int64(len(contents) - 1)
	if _, err := store.Transfer(t.Context(), fmt.Sprintf("%x", sha256.Sum256([]byte("other"))),
		Source{Resource: src.Resource, Name: "fixture.mkv", MediaID: "second"}, Limits{Quota: tooLittle}, nil); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("expected quota refusal, got %v", err)
	}
	if err := store.Remove(id); err != nil || store.Bytes(id) != 0 {
		t.Fatalf("remove completed file: %v", err)
	}
}

func TestTransferCountsOptionalTracksAgainstQuota(t *testing.T) {
	contents := []byte("synthetic video")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "fixture.mp4", time.Unix(1, 0), bytes.NewReader(contents))
	}))
	t.Cleanup(server.Close)
	store := &Store{Dir: t.TempDir()}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte("tracks")))
	source := Source{Resource: mediahttp.Resource{URL: server.URL}, Name: "fixture.mp4", MediaID: "fixture",
		Tracks: func(context.Context) Tracks {
			return Tracks{Subtitles: []Subtitle{{ID: "en", Language: "en", Text: "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nfixture\n"}}}
		}}
	if _, err := store.Transfer(t.Context(), id, source, Limits{Quota: int64(len(contents))}, nil); err != nil {
		t.Fatal("the video did not complete at its allowed quota:", err)
	}
	if _, err := store.ReadTracks(id); !os.IsNotExist(err) || store.Bytes(id) != int64(len(contents)) {
		t.Fatalf("subtitle data exceeded the quota: %v", err)
	}
	if err := store.Remove(id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transfer(t.Context(), id, source, Limits{}, nil); err != nil {
		t.Fatal(err)
	}
	tracks, err := store.ReadTracks(id)
	if err != nil || len(tracks.Subtitles) != 1 || store.Bytes(id) <= int64(len(contents)) {
		t.Fatalf("optional tracks were not stored or included in usage: %+v, %v", tracks, err)
	}
	if err := store.Remove(id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadTracks(id); !os.IsNotExist(err) {
		t.Fatalf("removing the download left its subtitles: %v", err)
	}
}

func TestTransferKeepsVerifiedVideoWhileTracksArePaused(t *testing.T) {
	contents := []byte("video transferred before auxiliary tracks")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "fixture.mp4", time.Unix(1, 0), bytes.NewReader(contents))
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	store := &Store{Dir: dir}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte("paused-tracks")))
	source := Source{Resource: mediahttp.Resource{URL: server.URL}, Name: "fixture.mp4", MediaID: "fixture",
		Tracks: func(ctx context.Context) Tracks {
			<-ctx.Done()
			return Tracks{}
		}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := store.Transfer(ctx, id, source, Limits{}, nil)
		result <- err
	}()
	prepared := filepath.Join(dir, id, "complete.pending")
	deadline := time.After(3 * time.Second)
	interval := time.NewTicker(5 * time.Millisecond)
	defer interval.Stop()
	for fileSize(prepared) == 0 {
		select {
		case err := <-result:
			t.Fatalf("transfer ended before optional tracks could be paused: %v", err)
		case <-deadline:
			t.Fatal("the verified video was not staged while tracks were pending")
		case <-interval.C:
		}
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("pausing while fetching optional tracks returned %v", err)
	}
	if _, _, err := store.Open(id); err == nil {
		t.Fatal("the paused download was published before finishing its tracks")
	}
	server.Close()
	source.Tracks = func(context.Context) Tracks { return Tracks{AudioLanguage: "eng"} }
	resumed := &Store{Dir: dir}
	file, err := resumed.Transfer(t.Context(), id, source, Limits{}, nil)
	if err != nil || file.Size != int64(len(contents)) || file.SHA256 != fmt.Sprintf("%x", sha256.Sum256(contents)) {
		t.Fatalf("retry did not reuse the verified video: %+v, %v", file, err)
	}
	tracks, err := resumed.ReadTracks(id)
	if err != nil || tracks.AudioLanguage != "eng" {
		t.Fatalf("retry did not save the resumed tracks: %+v, %v", tracks, err)
	}
}
