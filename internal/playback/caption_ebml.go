package playback

import (
	"context"
	"math/bits"
)

const (
	mkvEBML      = 0x1a45dfa3
	mkvSegment   = 0x18538067
	mkvSeekHead  = 0x114d9b74
	mkvInfo      = 0x1549a966
	mkvTracks    = 0x1654ae6b
	mkvCues      = 0x1c53bb6b
	mkvCluster   = 0x1f43b675
	mkvTimestamp = 0xe7
	mkvGroup     = 0xa0
	mkvBlock     = 0xa1
	mkvSimple    = 0xa3
	mkvDuration  = 0x9b
	mkvHeaderMax = 12 // Four ID bytes and eight size bytes.
)

// mkvElement offsets are absolute when read from the file, and relative
// to their parent's payload when parsed from memory.
type mkvElement struct {
	id        uint64
	data, end int64
	unknown   bool
}

func mkvVint(data []byte) (value uint64, width int) {
	if len(data) == 0 || data[0] == 0 {
		return 0, 0
	}
	width = bits.LeadingZeros8(data[0]) + 1
	if len(data) < width {
		return 0, 0
	}
	value = uint64(data[0] & (0xff >> width))
	for _, b := range data[1:width] {
		value = value<<8 | uint64(b)
	}
	return value, width
}

func mkvHeader(data []byte, offset, limit int64) (mkvElement, error) {
	value, idWidth := mkvVint(data)
	if idWidth == 0 || idWidth > 4 {
		return mkvElement{}, errCaptions
	}
	id := value | 1<<(7*idWidth)
	length, sizeWidth := mkvVint(data[idWidth:])
	start := offset + int64(idWidth+sizeWidth)
	if sizeWidth == 0 || start < offset || start > limit {
		return mkvElement{}, errCaptions
	}
	unknown := length == (1<<(7*sizeWidth))-1
	if unknown {
		if id != mkvSegment && id != mkvCluster {
			return mkvElement{}, errCaptions
		}
		length = uint64(limit - start)
	}
	if length > uint64(limit-start) {
		return mkvElement{}, errCaptions
	}
	return mkvElement{id: id, data: start, end: start + int64(length), unknown: unknown}, nil
}

func mkvChildren(data []byte, visit func(uint64, []byte) error) error {
	limit := int64(len(data))
	for offset := int64(0); offset < limit; {
		child, err := mkvHeader(data[offset:], offset, limit)
		if err != nil {
			return err
		}
		if child.id == mkvCluster && child.unknown {
			child.end, err = mkvClusterEnd(data, child.data)
			if err != nil {
				return err
			}
		}
		if err := visit(child.id, data[child.data:child.end]); err != nil {
			return err
		}
		offset = child.end
	}
	return nil
}

// An unknown-size Cluster ends where another Segment child begins. Skip
// whole Blocks, since their encoded contents can contain Element IDs.
func mkvClusterEnd(data []byte, start int64) (int64, error) {
	limit := int64(len(data))
	for offset := start; offset < limit; {
		child, err := mkvHeader(data[offset:], offset, limit)
		if err != nil {
			return 0, err
		}
		switch child.id {
		case mkvCluster, mkvInfo, mkvTracks, mkvCues, mkvSeekHead,
			0x1941a469, 0x1043a770, 0x1254c367: // Attachments, Chapters, Tags
			return offset, nil
		}
		offset = child.end
	}
	return limit, nil
}

func mkvUint(data []byte) uint64 {
	if len(data) > 8 {
		return ^uint64(0)
	}
	var value uint64
	for _, b := range data {
		value = value<<8 | uint64(b)
	}
	return value
}

type mkvCaptionFile struct {
	captionFile
	head []byte
}

func (file mkvCaptionFile) element(ctx context.Context, offset, limit int64) (mkvElement, error) {
	if offset < 0 || offset >= limit || limit > file.size {
		return mkvElement{}, errCaptions
	}
	length := min(int64(mkvHeaderMax), limit-offset)
	data, err := file.bytes(ctx, offset, length)
	if err != nil {
		return mkvElement{}, err
	}
	return mkvHeader(data, offset, limit)
}

func (file mkvCaptionFile) bytes(ctx context.Context, offset, length int64) ([]byte, error) {
	if offset >= 0 && length >= 0 && offset <= int64(len(file.head)) && length <= int64(len(file.head))-offset {
		return file.head[offset : offset+length], nil
	}
	return file.read(ctx, offset, length)
}

func (file mkvCaptionFile) segment(ctx context.Context) (mkvElement, error) {
	header, err := file.element(ctx, 0, file.size)
	if err != nil || header.id != mkvEBML || header.end-header.data > 4096 {
		return mkvElement{}, errCaptions
	}
	data, err := file.bytes(ctx, header.data, header.end-header.data)
	if err != nil {
		return mkvElement{}, err
	}
	var docType string
	err = mkvChildren(data, func(id uint64, body []byte) error {
		if id == 0x4282 {
			docType = string(body)
		}
		return nil
	})
	if err != nil || (docType != "matroska" && docType != "webm") {
		return mkvElement{}, errCaptions
	}
	segment, err := file.element(ctx, header.end, file.size)
	if err != nil || segment.id != mkvSegment {
		return mkvElement{}, errCaptions
	}
	return segment, nil
}

func (file mkvCaptionFile) tables(ctx context.Context, segment mkvElement) (map[uint64]mkvElement, error) {
	tables := make(map[uint64]mkvElement)
	seekHeads := make(map[int64]bool)
	firstCluster := false
	var readSeek func(mkvElement) error
	readSeek = func(head mkvElement) error {
		if seekHeads[head.data] {
			return nil
		}
		if len(seekHeads) >= 4 || head.end-head.data > 32<<10 {
			return errCaptions
		}
		seekHeads[head.data] = true
		data, err := file.bytes(ctx, head.data, head.end-head.data)
		if err != nil {
			return err
		}
		return mkvChildren(data, func(id uint64, entry []byte) error {
			if id != 0x4dbb { // Seek
				return nil
			}
			var target uint64
			position := ^uint64(0)
			err := mkvChildren(entry, func(id uint64, value []byte) error {
				switch id {
				case 0x53ab: // SeekID
					target = mkvUint(value)
				case 0x53ac: // SeekPosition
					position = mkvUint(value)
				}
				return nil
			})
			if err != nil || position >= uint64(segment.end-segment.data) {
				return errCaptions
			}
			switch target {
			case mkvSeekHead, mkvInfo, mkvTracks, mkvCues:
				element, err := file.element(ctx, segment.data+int64(position), segment.end)
				if err != nil || element.id != target {
					return errCaptions
				}
				if target == mkvSeekHead {
					return readSeek(element)
				}
				tables[target] = element
			}
			return nil
		})
	}
	for offset, count := segment.data, 0; offset < segment.end && count < 128; count++ {
		element, err := file.element(ctx, offset, segment.end)
		if err != nil {
			return nil, err
		}
		switch element.id {
		case mkvSeekHead:
			if err := readSeek(element); err != nil {
				return nil, err
			}
		case mkvInfo, mkvTracks, mkvCues:
			tables[element.id] = element
		case mkvCluster:
			firstCluster = true
		}
		if tables[mkvTracks].id != 0 && tables[mkvInfo].id != 0 && (tables[mkvCues].id != 0 || firstCluster && file.size <= maxMKVScan) {
			break
		}
		offset = element.end
	}
	return tables, nil
}
