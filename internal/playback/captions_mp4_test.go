package playback

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"

	"waygu-video/internal/mediahttp"
)

//go:embed testdata/embedded-caption.mp4
var captionMP4 []byte

func TestFetchMP4CaptionsReadsOnlyIndexedRanges(t *testing.T) {
	// The fixture's moov box is at the end of the original file. Append a
	// large free box to make a full read detectable even on this tiny clip.
	const padding = 96 << 10
	movie := append(bytes.Clone(captionMP4), make([]byte, padding)...)
	binary.BigEndian.PutUint32(movie[len(captionMP4):], padding)
	copy(movie[len(captionMP4)+4:], "free")
	var requested atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer caption-fixture" {
			http.Error(w, "missing credentials", http.StatusUnauthorized)
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(req.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || end < start {
			t.Errorf("request was not a single bounded range: %s", req.Header.Get("Range"))
			http.Error(w, "missing range", http.StatusBadRequest)
			return
		}
		requested.Add(end - start + 1)
		http.ServeContent(w, req, "embedded-caption.mp4", time.Time{}, bytes.NewReader(movie))
	}))
	defer server.Close()
	original := mediahttp.Resource{URL: server.URL, Headers: http.Header{"Authorization": {"Bearer caption-fixture"}}}
	track, err := FetchMP4Captions(t.Context(), original, int64(len(movie)), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		at   time.Duration
		text string
	}{
		{1249 * time.Millisecond, ""},
		{1250 * time.Millisecond, "In the first caption."},
		{3500 * time.Millisecond, ""},
		{6000 * time.Millisecond, "In the second caption."},
		{7500 * time.Millisecond, ""},
	} {
		if got := track.At(check.at); got != check.text {
			t.Errorf("caption at %s = %q, want %q", check.at, got, check.text)
		}
	}
	if total := requested.Load(); total >= 2<<10 {
		t.Fatalf("requested %d bytes for %d-byte file", total, len(movie))
	}
	if _, err := FetchMP4Captions(t.Context(), original, int64(len(movie)), 0); err == nil {
		t.Fatal("the video track was accepted as a subtitle")
	}
}

func TestFetchMP4CaptionsRejectsFullOrMismatchedResponses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusPartialContent} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Range", "bytes 0-63/64")
			w.WriteHeader(status)
			_, _ = w.Write(make([]byte, 64))
		}))
		defer server.Close()
		if _, err := FetchMP4Captions(t.Context(), mediahttp.Resource{URL: server.URL}, 64, 1); err == nil {
			t.Fatalf("accepted invalid range with HTTP status %d", status)
		}
	}
}

func TestMP4CaptionSampleText(t *testing.T) {
	var data bytes.Buffer
	vtt := &mp4.VttcBox{}
	vtt.AddChild(&mp4.PaylBox{CueText: "<b>First</b> &amp; second"})
	if err := vtt.Encode(&data); err != nil {
		t.Fatal(err)
	}
	if got, err := mp4SubtitleText(data.Bytes(), "wvtt"); err != nil || got != "First & second" {
		t.Fatalf("WebVTT sample = %q, %v", got, err)
	}
	if got, err := mp4SubtitleText([]byte{0, 4, 0xfe, 0xff, 0x7e, 0x41}, "tx3g"); err != nil || got != "繁" {
		t.Fatalf("UTF-16 timed text = %q, %v", got, err)
	}
}

func TestMP4CaptionMaskKeepsVideoBytesAndRanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("ETag", `"original"`)
		http.ServeContent(w, req, "embedded-caption.mp4", time.Time{}, bytes.NewReader(captionMP4))
	}))
	defer server.Close()
	original := mediahttp.Resource{URL: server.URL}
	size := int64(len(captionMP4))
	mask, err := probeCaptionMask(t.Context(), original, size)
	if err != nil || len(mask.offsets) != 1 {
		t.Fatalf("expected one subtitle atom: %v, %v", mask.offsets, err)
	}
	address, stop, err := OpenWithoutMP4Captions(t.Context(), original, size)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	at := mask.offsets[0]
	expected := bytes.Clone(captionMP4)
	if string(expected[at:at+4]) != "trak" {
		t.Fatal("mask did not point to the subtitle box name")
	}
	copy(expected[at:], "free")
	for _, ranged := range []bool{false, true} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := expected
		status := http.StatusOK
		if ranged {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", at+1, at+4))
			want, status = expected[at+1:at+5], http.StatusPartialContent
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != status || !bytes.Equal(data, want) || res.Header.Get("ETag") != "" {
			t.Fatalf("masked stream response for range=%t: status %d, bytes %d, error %v", ranged, res.StatusCode, len(data), err)
		}
	}
	captions, err := FetchMP4Captions(t.Context(), original, size, 1)
	if err != nil || captions.At(2*time.Second) != "In the first caption." {
		t.Fatalf("the original subtitle disappeared: %v", err)
	}
}
