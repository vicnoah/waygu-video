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
	"strconv"
	"sync/atomic"
	"testing"
	"time"
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
	src := Source{URL: server.URL, Name: "fixture.mp4", MediaID: "first", Headers: http.Header{"Authorization": {"Bearer local-fixture"}}}
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
		Source{URL: server.URL, Name: "fixture.mkv", MediaID: "second", Headers: src.Headers}, Limits{Quota: tooLittle}, nil); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("expected quota refusal, got %v", err)
	}
	if err := store.Remove(id); err != nil || store.Bytes(id) != 0 {
		t.Fatalf("remove completed file: %v", err)
	}
}
