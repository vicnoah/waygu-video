package playback

import (
	"context"
	"encoding/binary"
	"math"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"waygu-video/internal/mediahttp"
)

const (
	maxMKVScan  = 16 << 20
	maxMKVTable = 8 << 20
	maxMKVBlock = 64 << 10
	mkvMissing  = ^uint64(0)
)

type mkvSubtitleTrack struct {
	number          uint64
	codec           string
	scale           float64
	defaultDuration time.Duration
	delay           time.Duration
}

// FetchMatroskaCaptions reads a Matroska text track via its subtitle CuePoints.
// Small files without a complete seek index can also be read sequentially.
func FetchMatroskaCaptions(ctx context.Context, original mediahttp.Resource, size int64, index int) (*Captions, error) {
	if size < mkvHeaderMax || index < 0 {
		return nil, errCaptions
	}
	base, closeIdle, err := openCaptionFile(original, size)
	if err != nil {
		return nil, err
	}
	defer closeIdle()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	file := mkvCaptionFile{captionFile: base}
	file.head, err = file.read(ctx, 0, min(size, 4096))
	if err != nil {
		return nil, err
	}
	segment, err := file.segment(ctx)
	if err != nil {
		return nil, err
	}
	tables, err := file.tables(ctx, segment)
	if err != nil {
		return nil, err
	}
	readTable := func(id uint64) ([]byte, error) {
		table := tables[id]
		if table.id == 0 || table.end-table.data > maxMKVTable {
			return nil, errCaptions
		}
		return file.bytes(ctx, table.data, table.end-table.data)
	}
	info, err := readTable(mkvInfo)
	if err != nil {
		return nil, err
	}
	scale := uint64(time.Millisecond)
	err = mkvChildren(info, func(id uint64, body []byte) error {
		if id == 0x2ad7b1 { // TimestampScale
			scale = mkvUint(body)
		}
		return nil
	})
	if err != nil || scale == 0 || scale > uint64(maxCaptionTime) {
		return nil, errCaptions
	}
	tracks, err := readTable(mkvTracks)
	if err != nil {
		return nil, err
	}
	track, err := mkvTextTrack(tracks, index)
	if err != nil {
		return nil, err
	}
	if tables[mkvCues].id != 0 {
		index, err := readTable(mkvCues)
		if err != nil {
			return nil, err
		}
		points, err := mkvCuePoints(index, track.number)
		if err != nil {
			return nil, err
		}
		if len(points) > 0 {
			lines, err := file.indexedCues(ctx, segment, track, float64(scale), points)
			if err == nil {
				return newCaptions(lines)
			}
		}
	}
	if size > maxMKVScan {
		return nil, errCaptions
	}
	data, err := file.bytes(ctx, segment.data, segment.end-segment.data)
	if err != nil {
		return nil, err
	}
	cues, err := mkvScanCues(data, track, float64(scale))
	if err != nil {
		return nil, err
	}
	return newCaptions(cues)
}

func mkvTextTrack(data []byte, index int) (mkvSubtitleTrack, error) {
	track := mkvSubtitleTrack{scale: 1}
	err := mkvChildren(data, func(id uint64, data []byte) error {
		if id != 0xae { // TrackEntry
			return nil
		}
		index--
		if index != -1 {
			return nil
		}
		var kind uint64
		var encodings bool
		err := mkvChildren(data, func(id uint64, value []byte) error {
			switch id {
			case 0xd7: // TrackNumber
				track.number = mkvUint(value)
			case 0x83: // TrackType
				kind = mkvUint(value)
			case 0x86: // CodecID
				track.codec = string(value)
			case 0x23314f: // TrackTimestampScale
				switch len(value) {
				case 4:
					track.scale = float64(math.Float32frombits(binary.BigEndian.Uint32(value)))
				case 8:
					track.scale = math.Float64frombits(binary.BigEndian.Uint64(value))
				default:
					return errCaptions
				}
			case 0x23e383: // DefaultDuration
				track.defaultDuration = time.Duration(mkvUint(value))
			case 0x56aa: // CodecDelay
				track.delay = time.Duration(mkvUint(value))
			case 0x6d80: // ContentEncodings
				encodings = true
			}
			return nil
		})
		if err != nil || kind != 17 || track.number == 0 || track.number >= 1<<56 || encodings ||
			!(track.scale > 0 && track.scale <= float64(maxCaptionTime)) ||
			track.defaultDuration < 0 || track.defaultDuration > maxCaptionTime || track.delay < 0 || track.delay > maxCaptionTime {
			return errCaptions
		}
		switch track.codec {
		case "S_TEXT/UTF8", "S_TEXT/WEBVTT", "D_WEBVTT/SUBTITLES", "D_WEBVTT/CAPTIONS":
			return nil
		default:
			return errCaptions
		}
	})
	if err != nil || track.number == 0 {
		return mkvSubtitleTrack{}, errCaptions
	}
	return track, nil
}

type mkvCuePoint struct {
	cluster, relative, at, duration uint64
}

func mkvCuePoints(data []byte, track uint64) ([]mkvCuePoint, error) {
	var points []mkvCuePoint
	err := mkvChildren(data, func(id uint64, body []byte) error {
		if id != 0xbb { // CuePoint
			return nil
		}
		start := len(points)
		at := mkvMissing
		err := mkvChildren(body, func(id uint64, body []byte) error {
			switch id {
			case 0xb3: // CueTime
				at = mkvUint(body)
			case 0xb7: // CueTrackPositions
				var number uint64
				point := mkvCuePoint{cluster: mkvMissing, relative: mkvMissing}
				err := mkvChildren(body, func(id uint64, value []byte) error {
					switch id {
					case 0xf7: // CueTrack
						number = mkvUint(value)
					case 0xf1: // CueClusterPosition
						point.cluster = mkvUint(value)
					case 0xf0: // CueRelativePosition
						point.relative = mkvUint(value)
					case 0xb2: // CueDuration
						point.duration = mkvUint(value)
					}
					return nil
				})
				if err != nil {
					return err
				}
				if number == track {
					if len(points) == maxCaptionSamples {
						return errCaptions
					}
					points = append(points, point)
				}
			}
			return nil
		})
		if err != nil || at == mkvMissing {
			return errCaptions
		}
		for i := start; i < len(points); i++ {
			points[i].at = at
		}
		return nil
	})
	return points, err
}

func (file mkvCaptionFile) indexedCues(parent context.Context, segment mkvElement, track mkvSubtitleTrack, scale float64, points []mkvCuePoint) ([]cue, error) {
	byCluster := make(map[uint64][]int)
	for i, point := range points {
		if point.cluster >= uint64(segment.end-segment.data) || point.relative >= uint64(segment.end-segment.data) {
			return nil, errCaptions
		}
		byCluster[point.cluster] = append(byCluster[point.cluster], i)
	}
	cues := make([]cue, len(points))
	var payload atomic.Int64
	group, ctx := errgroup.WithContext(parent)
	group.SetLimit(12)
	for cluster, indexes := range byCluster {
		if ctx.Err() != nil {
			break
		}
		group.Go(func() error {
			element, err := file.element(ctx, segment.data+int64(cluster), segment.end)
			if err != nil || element.id != mkvCluster {
				return errCaptions
			}
			for _, i := range indexes {
				point := points[i]
				if point.relative >= uint64(element.end-element.data) {
					return errCaptions
				}
				frame, err := file.indexedFrame(ctx, element.data+int64(point.relative), element.end, track, &payload)
				if err != nil {
					return err
				}
				start := float64(point.at)*scale - float64(track.delay)
				duration := float64(point.duration) * scale
				cues[i] = frame.cue(start, duration, scale, track)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	var visible []cue
	for _, line := range cues {
		if line.text != "" {
			visible = append(visible, line)
		}
	}
	return visible, parent.Err()
}

func (file mkvCaptionFile) indexedFrame(ctx context.Context, offset, end int64, track mkvSubtitleTrack, budget *atomic.Int64) (mkvSubtitleFrame, error) {
	// Most text Blocks fit with their EBML headers in one short request.
	prefix, err := file.bytes(ctx, offset, min(160, end-offset))
	if err != nil {
		return mkvSubtitleFrame{}, err
	}
	block, err := mkvHeader(prefix, offset, end)
	size := block.end - block.data
	if err != nil || (block.id != mkvGroup && block.id != mkvSimple) || size > maxMKVBlock ||
		budget.Add(size) > captionLimit {
		return mkvSubtitleFrame{}, errCaptions
	}
	var data []byte
	if block.end-offset <= int64(len(prefix)) {
		data = prefix[block.data-offset : block.end-offset]
	} else {
		data, err = file.bytes(ctx, block.data, size)
		if err != nil {
			return mkvSubtitleFrame{}, err
		}
	}
	frame, err := mkvSubtitleBlock(block.id, data, track)
	if err != nil || !frame.selected {
		return mkvSubtitleFrame{}, errCaptions
	}
	return frame, nil
}

type mkvSubtitleFrame struct {
	selected bool
	relative int16
	duration uint64
	text     string
}

func mkvSubtitleBlock(id uint64, body []byte, track mkvSubtitleTrack) (mkvSubtitleFrame, error) {
	block := body
	var frame mkvSubtitleFrame
	if id == mkvGroup {
		block = nil
		err := mkvChildren(body, func(id uint64, value []byte) error {
			switch id {
			case mkvBlock:
				block = value
			case mkvDuration:
				frame.duration = mkvUint(value)
			}
			return nil
		})
		if err != nil {
			return frame, err
		}
	}
	number, width := mkvVint(block)
	if width == 0 || width+3 > len(block) {
		return frame, errCaptions
	}
	if number != track.number {
		return frame, nil
	}
	frame.selected = true
	frame.relative = int16(binary.BigEndian.Uint16(block[width:]))
	flags, text := block[width+2], block[width+3:]
	if flags&0x06 != 0 || len(text) > maxMKVBlock || !utf8.Valid(text) {
		return frame, errCaptions
	}
	if flags&0x08 == 0 {
		value := strings.TrimPrefix(string(text), "\uFEFF")
		if strings.HasPrefix(track.codec, "D_WEBVTT/") {
			// FFmpeg's WebM mapping prefixes the cue identifier and settings.
			parts := strings.SplitN(value, "\n", 3)
			if len(parts) != 3 {
				return frame, errCaptions
			}
			value = parts[2]
		}
		frame.text = plainCaptionText(value)
	}
	return frame, nil
}

func (frame mkvSubtitleFrame) cue(start, duration, scale float64, track mkvSubtitleTrack) cue {
	if duration == 0 {
		duration = float64(frame.duration) * scale * track.scale
	}
	if duration == 0 {
		duration = float64(track.defaultDuration)
	}
	end := start + duration
	if frame.text == "" || duration <= 0 || end <= 0 || end >= float64(maxCaptionTime) {
		return cue{}
	}
	return cue{time.Duration(max(0, start)), time.Duration(end), frame.text}
}

func mkvScanCues(data []byte, track mkvSubtitleTrack, scale float64) ([]cue, error) {
	var cues []cue
	var payload int
	err := mkvChildren(data, func(id uint64, data []byte) error {
		if id != mkvCluster {
			return nil
		}
		var timestamp uint64
		var frames []mkvSubtitleFrame
		err := mkvChildren(data, func(id uint64, body []byte) error {
			switch id {
			case mkvTimestamp:
				timestamp = mkvUint(body)
			case mkvGroup, mkvSimple:
				frame, err := mkvSubtitleBlock(id, body, track)
				if err != nil {
					return err
				}
				if frame.text != "" {
					frames = append(frames, frame)
					payload += len(frame.text)
					if len(cues)+len(frames) > maxCaptionSamples || payload > captionLimit {
						return errCaptions
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, frame := range frames {
			start := (float64(timestamp)+float64(frame.relative)*track.scale)*scale - float64(track.delay)
			if line := frame.cue(start, 0, scale, track); line.text != "" {
				cues = append(cues, line)
			}
		}
		return nil
	})
	return cues, err
}
