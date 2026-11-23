package downloads

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// A clean HTTP EOF alone cannot distinguish a complete transcode from an
// encoder that stopped at a fragment boundary. Check the video timeline and
// make sure every top-level box fits within the file.
func transcodedMP4Height(file io.ReaderAt, size int64, maxHeight int, duration time.Duration) int {
	var trackID, timescale, defaultDuration uint32
	var end uint64
	var height int
	var firstBox, movie, media, pendingFragment bool
	for offset := int64(0); offset < size; {
		body := io.NewSectionReader(file, offset, size-offset)
		header, err := mp4.DecodeHeader(body)
		if err != nil || header.Size < uint64(header.Hdrlen) || header.Size > uint64(size-offset) {
			return 0
		}
		if pendingFragment && header.Name != "mdat" {
			return 0
		}
		switch header.Name {
		case "ftyp":
			firstBox = offset == 0
		case "moov":
			if !firstBox || movie || header.Size > 32<<20 {
				return 0
			}
			box, err := mp4.DecodeMoov(header, uint64(offset), body)
			if err != nil {
				return 0
			}
			moov := box.(*mp4.MoovBox)
			for _, track := range moov.Traks {
				if track.Tkhd == nil || track.Mdia == nil || track.Mdia.Hdlr == nil ||
					track.Mdia.Hdlr.HandlerType != "vide" || track.Mdia.Mdhd == nil {
					continue
				}
				height = int(track.Tkhd.Height >> 16)
				if height <= 0 || height > maxHeight || trackID != 0 {
					return 0
				}
				trackID = track.Tkhd.TrackID
				timescale, end = track.Mdia.Mdhd.Timescale, track.Mdia.Mdhd.Duration
				if moov.Mvex != nil {
					if trex, ok := moov.Mvex.GetTrex(trackID); ok {
						defaultDuration = trex.DefaultSampleDuration
					}
				}
			}
			movie = trackID > 0 && timescale > 0
		case "moof":
			if !movie || header.Size > 8<<20 {
				return 0
			}
			box, err := mp4.DecodeMoof(header, uint64(offset), body)
			if err != nil {
				return 0
			}
			for _, fragment := range box.(*mp4.MoofBox).Trafs {
				if fragment.Tfhd == nil || fragment.Tfhd.TrackID != trackID {
					continue
				}
				if fragment.Tfdt != nil {
					start := fragment.Tfdt.BaseMediaDecodeTime()
					if start < end || start-end > uint64(timescale) {
						return 0
					}
					end = start
				}
				sampleLength := fragmentSampleDuration(fragment, defaultDuration)
				for _, run := range fragment.Truns {
					end += run.Duration(sampleLength)
					if end/uint64(timescale) > 365*24*60*60 {
						return 0
					}
				}
			}
			pendingFragment = true
		case "mdat":
			if header.Size == uint64(header.Hdrlen) {
				return 0
			}
			media, pendingFragment = true, false
		}
		offset += int64(header.Size)
	}
	if !movie || !media || pendingFragment || end == 0 {
		return 0
	}
	seconds := float64(end) / float64(timescale)
	tolerance := min(1, duration.Seconds()/10)
	if duration > 0 && (seconds+tolerance < duration.Seconds() || seconds > duration.Seconds()+tolerance) {
		return 0
	}
	return height
}

// Write one index for the complete movie and normalize its track timestamps.
// Playlists have a reservation beside the init segment. Progressive encoders
// supply no space there, so insert the index and relocate absolute offsets.
func indexMP4Timeline(ctx context.Context, file *os.File, size, indexOffset int64, indexSize uint64,
	reserve func(int64) error) (int64, error) {
	if indexSize > 0 {
		if indexOffset <= 0 || indexOffset >= size {
			return 0, ErrInvalidFile
		}
		padding, err := mp4.DecodeHeader(io.NewSectionReader(file, indexOffset, size-indexOffset))
		if err != nil || padding.Name != "free" || padding.Size != indexSize {
			return 0, ErrInvalidFile
		}
	}
	scales := make(map[uint32]uint32)
	var videoTrack, defaultDuration uint32
	var index mp4.SidxBox
	var firstFragment, previousFragment int64
	var absoluteOffsets []int64
	var baseSeconds float64
	for offset := int64(0); offset < size; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		body := io.NewSectionReader(file, offset, size-offset)
		header, err := mp4.DecodeHeader(body)
		if err != nil || header.Size < uint64(header.Hdrlen) || header.Size > uint64(size-offset) {
			return 0, ErrInvalidFile
		}
		switch header.Name {
		case "moov":
			if header.Size > 32<<20 || len(scales) != 0 {
				return 0, ErrInvalidFile
			}
			box, err := mp4.DecodeMoov(header, uint64(offset), body)
			if err != nil {
				return 0, ErrInvalidFile
			}
			for _, track := range box.(*mp4.MoovBox).Traks {
				if track.Tkhd != nil && track.Mdia != nil && track.Mdia.Mdhd != nil {
					id, scale := track.Tkhd.TrackID, track.Mdia.Mdhd.Timescale
					scales[id] = scale
					if track.Mdia.Hdlr != nil && track.Mdia.Hdlr.HandlerType == "vide" {
						videoTrack, index.ReferenceID, index.Timescale = id, id, scale
					}
				}
			}
			if mvex := box.(*mp4.MoovBox).Mvex; mvex != nil {
				if trex, ok := mvex.GetTrex(videoTrack); ok {
					defaultDuration = trex.DefaultSampleDuration
				}
			}
		case "sidx", "mfra":
			// Keep the original space and byte offsets while preventing a
			// decoder from treating a single segment as the entire movie.
			if _, err := file.WriteAt([]byte("free"), offset+4); err != nil {
				return 0, err
			}
		case "moof":
			if header.Size > 8<<20 || videoTrack == 0 || index.Timescale == 0 || len(index.SidxRefs) >= 65535 {
				return 0, ErrInvalidFile
			}
			box, err := mp4.DecodeMoof(header, uint64(offset), body)
			if err != nil {
				return 0, ErrInvalidFile
			}
			fragment := box.(*mp4.MoofBox)
			if len(index.SidxRefs) == 0 {
				baseSeconds, err = fragmentStart(fragment, scales)
				if err != nil {
					return 0, err
				}
				firstFragment = offset
			} else if err := setSegmentSize(&index, offset-previousFragment); err != nil {
				return 0, err
			}
			var duration uint64
			var absolute bool
			for _, track := range fragment.Trafs {
				if track.Tfhd == nil || track.Tfdt == nil || scales[track.Tfhd.TrackID] == 0 {
					return 0, ErrInvalidFile
				}
				absolute = absolute || track.Tfhd.HasBaseDataOffset()
				version := track.Tfdt.Version
				at := track.Tfdt.BaseMediaDecodeTime()
				track.Tfdt.SetBaseMediaDecodeTime(at - min(at, mp4Ticks(baseSeconds, scales[track.Tfhd.TrackID])))
				track.Tfdt.Version = version
				if track.Tfhd.TrackID == videoTrack {
					for _, run := range track.Truns {
						duration += run.Duration(fragmentSampleDuration(track, defaultDuration))
					}
				}
			}
			if err := replaceMP4Box(file, fragment, offset, header.Size); err != nil {
				return 0, err
			}
			if absolute {
				absoluteOffsets = append(absoluteOffsets, offset)
			}
			if duration == 0 || duration > 1<<32-1 {
				return 0, ErrInvalidFile
			}
			index.SidxRefs = append(index.SidxRefs, mp4.SidxRef{
				SubSegmentDuration: uint32(duration), StartsWithSAP: 1, SAPType: 1})
			previousFragment = offset
		}
		offset += int64(header.Size)
	}
	if previousFragment == 0 {
		if indexSize != 0 {
			return 0, ErrInvalidFile
		}
		return size, nil
	}
	if err := setSegmentSize(&index, size-previousFragment); err != nil {
		return 0, err
	}
	if indexSize == 0 {
		growth := int64(index.Size())
		if err := reserve(size + growth); err != nil {
			return 0, err
		}
		if err := relocateFragments(ctx, file, size, firstFragment, growth, absoluteOffsets); err != nil {
			return 0, err
		}
		indexOffset, indexSize, size = firstFragment, index.Size(), size+growth
	} else {
		gap := firstFragment - indexOffset - int64(indexSize)
		if index.Size() != indexSize || gap < 0 || gap > 1<<32-1 {
			return 0, ErrInvalidFile
		}
		index.FirstOffset = uint64(gap)
	}
	return size, replaceMP4Box(file, &index, indexOffset, indexSize)
}

func relocateFragments(ctx context.Context, file *os.File, size, start, gap int64, offsets []int64) error {
	for _, offset := range offsets {
		box, err := mp4.DecodeBox(uint64(offset), io.NewSectionReader(file, offset, size-offset))
		if err != nil {
			return ErrInvalidFile
		}
		fragment := box.(*mp4.MoofBox)
		for _, track := range fragment.Trafs {
			if track.Tfhd.HasBaseDataOffset() {
				address := track.Tfhd.BaseDataOffset
				if address < uint64(start) || address >= uint64(size) {
					return ErrInvalidFile
				}
				track.Tfhd.BaseDataOffset = address + uint64(gap)
			}
		}
		if err := replaceMP4Box(file, fragment, offset, fragment.Size()); err != nil {
			return err
		}
	}
	buffer := make([]byte, 1<<20)
	for end := size; end > start; {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := buffer[:min(end-start, int64(len(buffer)))]
		end -= int64(len(chunk))
		if _, err := file.ReadAt(chunk, end); err != nil {
			return err
		}
		if _, err := file.WriteAt(chunk, end+gap); err != nil {
			return err
		}
	}
	return nil
}

func setSegmentSize(index *mp4.SidxBox, size int64) error {
	if size <= 0 || size > 1<<31-1 {
		return ErrInvalidFile
	}
	index.SidxRefs[len(index.SidxRefs)-1].ReferencedSize = uint32(size)
	return nil
}

func fragmentSampleDuration(track *mp4.TrafBox, fallback uint32) uint32 {
	if duration := track.Tfhd.DefaultSampleDuration; duration != 0 {
		return duration
	}
	return fallback
}

func fragmentStart(fragment *mp4.MoofBox, scales map[uint32]uint32) (float64, error) {
	var first float64
	for i, track := range fragment.Trafs {
		if track.Tfhd == nil || track.Tfdt == nil || scales[track.Tfhd.TrackID] == 0 {
			return 0, ErrInvalidFile
		}
		at := float64(track.Tfdt.BaseMediaDecodeTime()) / float64(scales[track.Tfhd.TrackID])
		if i == 0 || at < first {
			first = at
		}
	}
	if len(fragment.Trafs) == 0 || first > 60 {
		return 0, ErrInvalidFile
	}
	return first, nil
}

func mp4Ticks(seconds float64, timescale uint32) uint64 {
	return uint64(seconds*float64(timescale) + 0.000001)
}

func replaceMP4Box(file io.WriterAt, box mp4.Box, offset int64, size uint64) error {
	var encoded bytes.Buffer
	if err := box.Encode(&encoded); err != nil || uint64(encoded.Len()) != size {
		return ErrInvalidFile
	}
	_, err := file.WriteAt(encoded.Bytes(), offset)
	return err
}
