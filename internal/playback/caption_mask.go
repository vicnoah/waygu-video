package playback

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/Eyevinn/mp4ff/bits"
	"github.com/Eyevinn/mp4ff/mp4"

	"waygu-video/internal/mediahttp"
)

// OpenWithoutMP4Captions leaves the source intact and masks text tracks in the
// loopback presentation. Native video decoders then leave captions to the app.
func OpenWithoutMP4Captions(ctx context.Context, source mediahttp.Resource, size int64) (string, func(), error) {
	probe, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	mask, err := probeCaptionMask(probe, source, size)
	if err != nil || len(mask.offsets) == 0 {
		return Open(source)
	}
	proxy, local, stop, err := startProxy(source, "127.0.0.1:0", netip.Addr{}, "", 0)
	if err != nil {
		return "", nil, err
	}
	proxy.captionMask = mask
	return local + proxy.add(proxy.origin), stop, nil
}

type mp4CaptionMask struct {
	size    int64
	offsets []int64
}

func probeCaptionMask(ctx context.Context, resource mediahttp.Resource, size int64) (mp4CaptionMask, error) {
	file, closeIdle, err := openCaptionFile(resource, size)
	if err != nil {
		return mp4CaptionMask{}, err
	}
	defer closeIdle()
	movie, _, err := file.mp4Regions(ctx)
	if err != nil {
		return mp4CaptionMask{}, err
	}
	data, err := file.read(ctx, movie.start, movie.end-movie.start)
	if err != nil {
		return mp4CaptionMask{}, err
	}
	r := bits.NewFixedSliceReader(data)
	if _, err := mp4.DecodeHeaderSR(r); err != nil {
		return mp4CaptionMask{}, errCaptions
	}
	mask := mp4CaptionMask{size: size}
	for r.NrRemainingBytes() > 0 {
		pos := r.GetPos()
		hdr, err := mp4.DecodeHeaderSR(r)
		if err != nil || hdr.Size > uint64(len(data)-pos) || hdr.Size < uint64(hdr.Hdrlen) {
			return mp4CaptionMask{}, errCaptions
		}
		length := int(hdr.Size)
		if hdr.Name == "trak" {
			box, err := mp4.DecodeBoxSR(0, bits.NewFixedSliceReader(data[pos:pos+length]))
			track, ok := box.(*mp4.TrakBox)
			if err != nil || !ok {
				return mp4CaptionMask{}, errCaptions
			}
			if _, _, ok := mp4SubtitleFormat(track); ok {
				// A free box of the same length keeps every media sample
				// offset and HTTP byte range valid.
				mask.offsets = append(mask.offsets, movie.start+int64(pos)+4)
			}
		}
		r.SkipBytes(length - hdr.Hdrlen)
	}
	return mask, nil
}

func (mask mp4CaptionMask) responseStart(res *http.Response) (int64, bool) {
	if encoding := res.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return 0, false
	}
	if res.StatusCode == http.StatusOK && (res.ContentLength == mask.size || res.ContentLength < 0) {
		return 0, true
	}
	if res.StatusCode == http.StatusPartialContent {
		region, ok := strings.CutPrefix(res.Header.Get("Content-Range"), "bytes ")
		first, rest, dash := strings.Cut(region, "-")
		last, total, slash := strings.Cut(rest, "/")
		a, startErr := strconv.ParseInt(first, 10, 64)
		b, endErr := strconv.ParseInt(last, 10, 64)
		return a, ok && dash && slash && startErr == nil && endErr == nil &&
			a >= 0 && b >= a && b < mask.size && total == strconv.FormatInt(mask.size, 10)
	}
	return 0, false
}

type maskedCaptionReader struct {
	source  io.Reader
	pos     int64
	offsets []int64
}

func (r *maskedCaptionReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	end := r.pos + int64(n)
	for _, at := range r.offsets {
		lo, hi := max(r.pos, at), min(end, at+4)
		if lo < hi {
			copy(p[lo-r.pos:hi-r.pos], "free"[lo-at:hi-at])
		}
	}
	r.pos = end
	return n, err
}
