package playback

import (
	"cmp"
	"context"
	"encoding/binary"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Eyevinn/mp4ff/bits"
	"github.com/Eyevinn/mp4ff/mp4"
	"golang.org/x/sync/errgroup"
	"golang.org/x/text/encoding/unicode"

	"waygu-video/internal/mediahttp"
)

const (
	maxCaptionMovieBox = 24 << 20
	maxCaptionSamples  = 24_000
	maxCaptionTime     = 30 * 24 * time.Hour
)

type mp4Region struct{ start, end int64 }

type mp4CaptionSample struct {
	bytes      mp4Region
	start, end time.Duration
}

// FetchMP4Captions reads text samples from a progressive MP4 without downloading
// its video or audio tracks. index is the server's zero-based stream index.
func FetchMP4Captions(ctx context.Context, original mediahttp.Resource, size int64, index int) (*Captions, error) {
	if size < 8 || index < 0 {
		return nil, errCaptions
	}
	client, err := newCaptionClient(original.URL, original.Trust)
	if err != nil {
		return nil, err
	}
	if transport, ok := client.Transport.(*http.Transport); ok {
		defer transport.CloseIdleConnections()
	}
	client.Transport = original.Authorize.Transport(client.Transport)
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	file := captionFile{original, client, size}
	movie, data, err := file.mp4Regions(ctx)
	if err != nil {
		return nil, err
	}
	header, err := file.read(ctx, movie.start, movie.end-movie.start)
	if err != nil {
		return nil, err
	}
	box, err := mp4.DecodeBoxSR(uint64(movie.start), bits.NewFixedSliceReader(header))
	moov, ok := box.(*mp4.MoovBox)
	if err != nil || !ok || moov.Mvex != nil || index >= len(moov.Traks) {
		return nil, errCaptions
	}
	track := moov.Traks[index]
	samples, format, err := mp4CaptionSamples(track, data)
	if err != nil {
		return nil, err
	}
	edits, err := mp4SubtitleEdits(moov, track)
	if err != nil {
		return nil, err
	}
	lines, err := file.readMP4SubtitleSamples(ctx, samples, format)
	if err != nil {
		return nil, err
	}
	var cues []cue
	for i, sample := range samples {
		if lines[i] == "" {
			continue
		}
		for _, edit := range edits {
			start, end := max(sample.start, edit.from), min(sample.end, edit.to)
			if start < end {
				cues = append(cues, cue{start - edit.from + edit.at, end - edit.from + edit.at, lines[i]})
				if len(cues) > maxCaptionSamples {
					return nil, errCaptions
				}
			}
		}
	}
	return newCaptions(cues)
}

func (file captionFile) mp4Regions(ctx context.Context) (mp4Region, []mp4Region, error) {
	var movie mp4Region
	var data []mp4Region
	for offset, boxes := int64(0), 0; offset < file.size; boxes++ {
		if boxes == 256 {
			return mp4Region{}, nil, errCaptions
		}
		hdr, err := file.read(ctx, offset, 8)
		if err != nil {
			return mp4Region{}, nil, err
		}
		length, header := uint64(binary.BigEndian.Uint32(hdr)), int64(8)
		switch length {
		case 0:
			length = uint64(file.size - offset)
		case 1:
			extra, err := file.read(ctx, offset+8, 8)
			if err != nil {
				return mp4Region{}, nil, err
			}
			length, header = binary.BigEndian.Uint64(extra), 16
		}
		if length < uint64(header) || length > uint64(file.size-offset) {
			return mp4Region{}, nil, errCaptions
		}
		end := offset + int64(length)
		switch string(hdr[4:]) {
		case "moov":
			if length > maxCaptionMovieBox || movie.end != 0 {
				return mp4Region{}, nil, errCaptions
			}
			movie = mp4Region{offset, end}
		case "mdat":
			data = append(data, mp4Region{offset + header, end})
		case "moof":
			return mp4Region{}, nil, errCaptions
		}
		offset = end
	}
	if movie.end == 0 || len(data) == 0 {
		return mp4Region{}, nil, errCaptions
	}
	return movie, data, nil
}

func mp4CaptionSamples(track *mp4.TrakBox, data []mp4Region) ([]mp4CaptionSample, string, error) {
	count := track.GetNrSamples()
	stbl, format, ok := mp4SubtitleFormat(track)
	if !ok || count == 0 || count > maxCaptionSamples || track.Mdia.Mdhd == nil || track.Mdia.Mdhd.Timescale == 0 {
		return nil, "", errCaptions
	}
	if stbl.Stts == nil || track.CheckDataIsSelfContained() != nil {
		return nil, "", errCaptions
	}
	durations, err := stbl.Stts.SampleDurations(count)
	if err != nil {
		return nil, "", errCaptions
	}
	var offsets []int32
	if stbl.Ctts != nil {
		offsets, err = stbl.Ctts.CompositionTimeOffsets(count)
		if err != nil {
			return nil, "", errCaptions
		}
	}
	chunks, err := stbl.GetContainingChunks(1, count)
	if err != nil {
		return nil, "", errCaptions
	}
	ranges, err := track.GetRangesForSampleInterval(1, count)
	if err != nil {
		return nil, "", errCaptions
	}
	var samples []mp4CaptionSample
	var tick, payload int64
	var sample uint32 = 1
	emptySampleSize := int64(2)
	if format == "wvtt" {
		emptySampleSize = 8
	}
	for i, chunk := range chunks {
		r := ranges[i]
		if chunk.StartSampleNr != sample || !slices.ContainsFunc(data, func(box mp4Region) bool {
			return r.Offset >= uint64(box.start) && r.Offset <= uint64(box.end) && r.Size <= uint64(box.end)-r.Offset
		}) {
			return nil, "", errCaptions
		}
		offset := int64(r.Offset)
		limit := min(chunk.NrSamples, count-sample+1)
		for range limit {
			length := int64(stbl.Stsz.GetSampleSize(int(sample)))
			start := tick
			if offsets != nil {
				start += int64(offsets[sample-1])
			}
			duration := int64(durations[sample-1])
			// A gap holds only a tx3g length field or a vtte box header.
			if duration > 0 && length > emptySampleSize {
				payload += length
				if payload > captionLimit {
					return nil, "", errCaptions
				}
				scale := track.Mdia.Mdhd.Timescale
				samples = append(samples, mp4CaptionSample{mp4Region{offset, offset + length}, mp4Time(start, scale), mp4Time(start+duration, scale)})
			}
			offset += length
			tick += duration
			sample++
		}
		if offset != int64(r.Offset+r.Size) {
			return nil, "", errCaptions
		}
	}
	if sample != count+1 || mp4Time(tick, track.Mdia.Mdhd.Timescale) >= maxCaptionTime {
		return nil, "", errCaptions
	}
	slices.SortStableFunc(samples, func(a, b mp4CaptionSample) int { return cmp.Compare(a.bytes.start, b.bytes.start) })
	return samples, format, nil
}

func mp4SubtitleFormat(track *mp4.TrakBox) (*mp4.StblBox, string, bool) {
	if track.Mdia == nil || track.Mdia.Minf == nil || track.Mdia.Minf.Stbl == nil {
		return nil, "", false
	}
	stbl := track.Mdia.Minf.Stbl
	if stbl.Stsd == nil || len(stbl.Stsd.Children) != 1 {
		return nil, "", false
	}
	format := stbl.Stsd.Children[0].Type()
	return stbl, format, format == "tx3g" || format == "wvtt"
}

func (file captionFile) readMP4SubtitleSamples(parent context.Context, samples []mp4CaptionSample, format string) ([]string, error) {
	lines := make([]string, len(samples))
	group, ctx := errgroup.WithContext(parent)
	group.SetLimit(8)
	for first := 0; first < len(samples) && ctx.Err() == nil; {
		start, end := samples[first].bytes.start, samples[first].bytes.end
		last := first + 1
		// One small read for adjacent subtitle samples, even when their
		// packets are separated by a little muxer padding.
		for last < len(samples) && samples[last].bytes.start-end <= 128 && samples[last].bytes.end-start <= 64<<10 {
			end = max(end, samples[last].bytes.end)
			last++
		}
		batch, results := samples[first:last], lines[first:last]
		group.Go(func() error {
			body, err := file.read(ctx, start, end-start)
			if err != nil {
				return err
			}
			for i, sample := range batch {
				results[i], err = mp4SubtitleText(body[sample.bytes.start-start:sample.bytes.end-start], format)
				if err != nil {
					return err
				}
			}
			return nil
		})
		first = last
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return lines, parent.Err()
}

func mp4SubtitleText(data []byte, format string) (string, error) {
	if format == "wvtt" {
		var cues []string
		sr := bits.NewFixedSliceReader(data)
		for sr.NrRemainingBytes() > 0 {
			box, err := mp4.DecodeBoxSR(0, sr)
			if err != nil {
				return "", errCaptions
			}
			if line, ok := box.(*mp4.VttcBox); ok && line.Payl != nil {
				cues = append(cues, plainCaptionText(line.Payl.CueText))
			}
		}
		return strings.Join(cues, "\n"), nil
	}
	if len(data) < 2 {
		return "", errCaptions
	}
	length := int(binary.BigEndian.Uint16(data))
	if length > len(data)-2 {
		return "", errCaptions
	}
	txt := data[2 : length+2]
	if len(txt) >= 2 && (binary.BigEndian.Uint16(txt) == 0xfeff || binary.BigEndian.Uint16(txt) == 0xfffe) {
		var err error
		txt, err = unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder().Bytes(txt)
		if err != nil {
			return "", errCaptions
		}
	}
	if !utf8.Valid(txt) {
		return "", errCaptions
	}
	return plainCaptionText(strings.TrimPrefix(string(txt), "\uFEFF")), nil
}

type mp4SubtitleEdit struct{ from, to, at time.Duration }

func mp4SubtitleEdits(movie *mp4.MoovBox, track *mp4.TrakBox) ([]mp4SubtitleEdit, error) {
	if track.Edts == nil || len(track.Edts.Elst) == 0 {
		return []mp4SubtitleEdit{{to: maxCaptionTime}}, nil
	}
	if movie.Mvhd == nil || movie.Mvhd.Timescale == 0 || len(track.Edts.Elst) != 1 || len(track.Edts.Elst[0].Entries) > 64 {
		return nil, errCaptions
	}
	var edits []mp4SubtitleEdit
	var at time.Duration
	for _, entry := range track.Edts.Elst[0].Entries {
		if entry.SegmentDuration > 1<<62 || entry.MediaTime < -1 || entry.MediaRateFixed32() != 1<<16 {
			return nil, errCaptions
		}
		duration := mp4Time(int64(entry.SegmentDuration), movie.Mvhd.Timescale)
		if at+duration >= maxCaptionTime {
			return nil, errCaptions
		}
		if entry.MediaTime >= 0 {
			from := mp4Time(entry.MediaTime, track.Mdia.Mdhd.Timescale)
			edits = append(edits, mp4SubtitleEdit{from, from + duration, at})
		}
		at += duration
	}
	return edits, nil
}

func mp4Time(ticks int64, scale uint32) time.Duration {
	seconds, rest := ticks/int64(scale), ticks%int64(scale)
	if seconds >= int64(maxCaptionTime/time.Second) {
		return maxCaptionTime
	}
	return time.Duration(seconds)*time.Second + time.Duration(rest*int64(time.Second)/int64(scale))
}
