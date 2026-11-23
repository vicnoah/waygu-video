package playback

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"waygu-video/internal/mediahttp"
)

func TestFetchMatroskaCaptions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		padding int
		indexed bool
	}{
		{"indexed", maxMKVScan + 4096, true},
		{"small-unindexed", 0, false},
		{"large-unindexed", maxMKVScan + 4096, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			movie := matroskaFixture(tc.padding, tc.indexed)
			var downloaded atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				value, ranged := strings.CutPrefix(r.Header.Get("Range"), "bytes=")
				first, last, hasEnd := strings.Cut(value, "-")
				a, fromErr := strconv.ParseInt(first, 10, 64)
				b, toErr := strconv.ParseInt(last, 10, 64)
				if r.Header.Get("X-Plex-Token") != "fixture" || !ranged || !hasEnd ||
					fromErr != nil || toErr != nil || b < a || b >= int64(len(movie)) {
					t.Error("expected an authenticated single byte range")
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				downloaded.Add(b - a + 1)
				http.ServeContent(w, r, "fixture.mkv", time.Time{}, bytes.NewReader(movie))
			}))
			defer server.Close()
			source := mediahttp.Resource{URL: server.URL, Headers: http.Header{"X-Plex-Token": {"fixture"}}}
			captions, err := FetchMatroskaCaptions(t.Context(), source, int64(len(movie)), 1)
			if n := downloaded.Load(); n > 8192 {
				t.Errorf("read %d bytes from a file of %d bytes", n, len(movie))
			}
			if tc.name == "large-unindexed" {
				if err == nil {
					t.Fatal("unbounded sequential read was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, sample := range []struct {
				at   time.Duration
				want string
			}{
				{499 * time.Millisecond, ""},
				{500 * time.Millisecond, "理发以后"},
				{874 * time.Millisecond, "理发以后"},
				{875 * time.Millisecond, ""},
				{1500 * time.Millisecond, "下一行"},
				{1750 * time.Millisecond, ""},
			} {
				if got := captions.At(sample.at); got != sample.want {
					t.Errorf("at %v = %q, want %q", sample.at, got, sample.want)
				}
			}
			if _, err := FetchMatroskaCaptions(t.Context(), source, int64(len(movie)), 0); err == nil {
				t.Fatal("audio track was accepted as subtitles")
			}
		})
	}
}

func TestMatroskaSubtitleWebVTTFrame(t *testing.T) {
	body := append([]byte{0x87, 0xff, 0xfe, 0}, "speaker\nline:90%\n<i>第二行</i>"...)
	track := mkvSubtitleTrack{number: 7, codec: "D_WEBVTT/SUBTITLES"}
	frame, err := mkvSubtitleBlock(mkvSimple, body, track)
	if err != nil || !frame.selected || frame.relative != -2 || frame.text != "第二行" {
		t.Fatalf("frame = %+v, %v", frame, err)
	}
	body[3] = 0x02
	if _, err := mkvSubtitleBlock(mkvSimple, body, track); err == nil {
		t.Fatal("accepted a laced subtitle frame")
	}
}

func matroskaFixture(padding int, indexed bool) []byte {
	header := fixtureMKVElement(mkvEBML, fixtureMKVElement(0x4282, []byte("matroska")))
	info := fixtureMKVElement(mkvInfo, fixtureMKVUint(0x2ad7b1, 500_000))
	track := func(number, kind uint32, codec string) []byte {
		return fixtureMKVElement(0xae, fixtureMKVUint(0xd7, number), fixtureMKVUint(0x83, kind), fixtureMKVElement(0x86, []byte(codec)))
	}
	tracks := fixtureMKVElement(mkvTracks, track(3, 2, "A_AAC"), track(7, 17, "S_TEXT/UTF8"))
	timestamp := fixtureMKVUint(mkvTimestamp, 1002)
	block := append([]byte{0x87, 0xff, 0xfe, 0}, "<b>理发以后</b>"...)
	group := fixtureMKVElement(mkvGroup, fixtureMKVElement(mkvBlock, block), fixtureMKVUint(mkvDuration, 750))
	cluster := fixtureMKVElement(mkvCluster, timestamp, group)
	secondTimestamp := fixtureMKVUint(mkvTimestamp, 3000)
	secondBlock := append([]byte{0x87, 0, 0, 0}, "下一行"...)
	secondGroup := fixtureMKVElement(mkvGroup, fixtureMKVElement(mkvBlock, secondBlock), fixtureMKVUint(mkvDuration, 500))
	secondCluster := fixtureMKVElement(mkvCluster, secondTimestamp, secondGroup)
	void := fixtureMKVElement(0xec, make([]byte, padding))
	if !indexed {
		streamingCluster := append(binary.BigEndian.AppendUint32(nil, mkvCluster), 0xff)
		streamingCluster = append(streamingCluster, cluster[8:]...)
		return append(header, fixtureMKVElement(mkvSegment, info, tracks, void, streamingCluster, secondCluster)...)
	}
	seek := func(into uint32) []byte {
		id := binary.BigEndian.AppendUint32(nil, mkvCues)
		return fixtureMKVElement(mkvSeekHead, fixtureMKVElement(0x4dbb, fixtureMKVElement(0x53ab, id), fixtureMKVUint(0x53ac, into)))
	}
	clusterAt := uint32(len(seek(0)) + len(info) + len(tracks) + len(void))
	secondAt := clusterAt + uint32(len(cluster))
	point := func(at, cluster, relative uint32) []byte {
		return fixtureMKVElement(0xbb, fixtureMKVUint(0xb3, at),
			fixtureMKVElement(0xb7, fixtureMKVUint(0xf7, 7), fixtureMKVUint(0xf1, cluster), fixtureMKVUint(0xf0, relative)))
	}
	cues := fixtureMKVElement(mkvCues, point(1000, clusterAt, uint32(len(timestamp))), point(3000, secondAt, uint32(len(secondTimestamp))))
	seekHead := seek(secondAt + uint32(len(secondCluster)))
	return append(header, fixtureMKVElement(mkvSegment, seekHead, info, tracks, void, cluster, secondCluster, cues)...)
}

func fixtureMKVUint(id, value uint32) []byte {
	return fixtureMKVElement(id, binary.BigEndian.AppendUint32(nil, value))
}

func fixtureMKVElement(id uint32, parts ...[]byte) []byte {
	n := 0
	for _, part := range parts {
		n += len(part)
	}
	out := make([]byte, 0, 8+n)
	out = append(out, bytes.TrimLeft(binary.BigEndian.AppendUint32(nil, id), "\x00")...)
	out = binary.BigEndian.AppendUint32(out, uint32(n)|0x10000000)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}
