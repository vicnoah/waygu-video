package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SeekPreviewClient exposes optional per-file images for the playback timeline.
type SeekPreviewClient interface {
	SeekPreviews(context.Context, Item, string) (SeekPreview, error)
}

type SeekPreview func(context.Context, time.Duration) ([]byte, error)

func (a api) previewBytes(ctx context.Context, route string, params url.Values, headers http.Header, limit int64) ([]byte, error) {
	endpoint, err := a.resolve(route, params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	req.Header.Set("Accept", "image/jpeg, application/octet-stream")
	response, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, StatusError(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = ErrResponse
	}
	return data, err
}

func (m *mediaBrowser) SeekPreviews(ctx context.Context, item Item, streamID string) (SeekPreview, error) {
	if m.kind == Jellyfin {
		return m.trickplayPreview(ctx, item, streamID)
	}
	id := item.ID
	if streamID != "" {
		var playback browserPlaybackInfo
		query := m.userQuery()
		query.Set("MediaSourceId", streamID)
		if err := m.get(ctx, "Items/"+url.PathEscape(item.ID)+"/PlaybackInfo", query, &playback); err != nil {
			return nil, err
		}
		index := slices.IndexFunc(playback.MediaSources, func(source browserSource) bool { return source.ID == streamID })
		if index < 0 {
			return nil, ErrNoStream
		}
		if selected := playback.MediaSources[index].ItemID; selected != "" {
			id = selected
		} else if index > 0 {
			return nil, ErrNoStream
		}
	}
	data, err := m.previewBytes(ctx, "Videos/"+url.PathEscape(id)+"/index.bif",
		url.Values{"Width": {"320"}}, m.auth(), 32<<20)
	if err != nil {
		return nil, err
	}
	return bifPreview(data)
}

func bifPreview(data []byte) (SeekPreview, error) {
	const header, entrySize = 64, 8
	if len(data) < header || string(data[:8]) != "\x89BIF\r\n\x1a\n" || binary.LittleEndian.Uint32(data[8:]) != 0 {
		return nil, ErrResponse
	}
	count := uint64(binary.LittleEndian.Uint32(data[12:]))
	limit := header + (count+1)*entrySize
	if limit > uint64(len(data)) {
		return nil, ErrResponse
	}
	if count == 0 {
		return nil, ErrNoStream
	}
	scale := uint64(binary.LittleEndian.Uint32(data[16:]))
	if scale == 0 {
		scale = 1000
	}
	entry := func(i int) (stamp, offset uint32) {
		from := header + i*entrySize
		return binary.LittleEndian.Uint32(data[from:]), binary.LittleEndian.Uint32(data[from+4:])
	}
	stamp, offset := entry(0)
	if uint64(offset) < limit {
		return nil, ErrResponse
	}
	for i := 1; i <= int(count); i++ {
		nextStamp, nextOffset := entry(i)
		if nextStamp <= stamp || nextOffset <= offset || uint64(nextOffset) > uint64(len(data)) {
			return nil, ErrResponse
		}
		stamp, offset = nextStamp, nextOffset
	}
	if stamp != ^uint32(0) {
		return nil, ErrResponse
	}
	return func(_ context.Context, at time.Duration) ([]byte, error) {
		// Select the frame at or immediately before the requested timestamp.
		index := max(0, previewIndex(int(count), uint64(max(0, at.Milliseconds())), func(i int) uint64 {
			time, _ := entry(i)
			return uint64(time) * scale
		}))
		_, start := entry(index)
		_, end := entry(index + 1)
		return jpegPreview(data[start:end])
	}, nil
}

func previewIndex(count int, at uint64, stamp func(int) uint64) int {
	return sort.Search(count, func(i int) bool { return stamp(i) > at }) - 1
}

type trickplayInfo struct {
	Width, Height, TileWidth, TileHeight, ThumbnailCount, Interval int
}

func (t trickplayInfo) valid() bool {
	return t.Width > 0 && t.Width <= 1024 && t.Height > 0 && t.Height <= 1024 &&
		t.TileWidth > 0 && t.TileWidth <= 25 && t.TileHeight > 0 && t.TileHeight <= 25 &&
		t.ThumbnailCount > 0 && t.Interval > 0 && t.Width*t.TileWidth*t.Height*t.TileHeight <= 32_000_000
}

func (m *mediaBrowser) trickplayPreview(ctx context.Context, item Item, streamID string) (SeekPreview, error) {
	var detail struct {
		Trickplay map[string]map[string]trickplayInfo
	}
	params := m.userQuery()
	params.Set("Fields", "Trickplay")
	if err := m.get(ctx, "Items/"+url.PathEscape(item.ID), params, &detail); err != nil {
		return nil, err
	}
	choices, ok := detail.Trickplay[streamID]
	if !ok {
		for id, items := range detail.Trickplay {
			if strings.EqualFold(strings.ReplaceAll(id, "-", ""), strings.ReplaceAll(streamID, "-", "")) {
				choices = items
				break
			}
		}
	}
	var best trickplayInfo
	for _, info := range choices {
		if !info.valid() {
			continue
		}
		if best.Width == 0 || (info.Width >= 284 && (best.Width < 284 || info.Width < best.Width)) ||
			(info.Width < 284 && best.Width < 284 && info.Width > best.Width) {
			best = info
		}
	}
	if best.Width == 0 {
		return nil, ErrNoStream
	}
	var cache struct {
		sync.Mutex
		index int
		image image.Image
	}
	perTile := best.TileWidth * best.TileHeight
	route := "Videos/" + url.PathEscape(item.ID) + "/Trickplay/" + strconv.Itoa(best.Width) + "/"
	query := url.Values{"MediaSourceId": {streamID}}
	return func(ctx context.Context, at time.Duration) ([]byte, error) {
		index := int(min(int64(best.ThumbnailCount-1), max(0, at.Milliseconds()/int64(best.Interval))))
		tileIndex, slot := index/perTile, index%perTile
		cache.Lock()
		tile := cache.image
		if cache.index != tileIndex {
			tile = nil
		}
		cache.Unlock()
		if tile == nil {
			raw, err := m.previewBytes(ctx, route+strconv.Itoa(tileIndex)+".jpg", query, m.auth(), 8<<20)
			if err != nil {
				return nil, err
			}
			bounds, err := jpeg.DecodeConfig(bytes.NewReader(raw))
			if err != nil || bounds.Width != best.Width*best.TileWidth || bounds.Height > best.Height*best.TileHeight {
				return nil, ErrResponse
			}
			tile, err = jpeg.Decode(bytes.NewReader(raw))
			if err != nil {
				return nil, err
			}
			cache.Lock()
			cache.index, cache.image = tileIndex, tile
			cache.Unlock()
		}
		x, y := (slot%best.TileWidth)*best.Width, (slot/best.TileWidth)*best.Height
		rect := image.Rect(x, y, x+best.Width, y+best.Height)
		if !rect.In(tile.Bounds()) {
			return nil, ErrResponse
		}
		frame := image.NewRGBA(image.Rect(0, 0, best.Width, best.Height))
		draw.Draw(frame, frame.Bounds(), tile, rect.Min, draw.Src)
		var output bytes.Buffer
		err := jpeg.Encode(&output, frame, nil)
		return output.Bytes(), err
	}, nil
}

func (p *plexClient) SeekPreviews(ctx context.Context, item Item, streamID string) (SeekPreview, error) {
	entry, err := p.metadata(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	for _, variant := range entry.Media {
		if strconv.Itoa(variant.ID) == streamID && len(variant.Part) != 0 && variant.Part[0].ID > 0 {
			base := "/library/parts/" + strconv.Itoa(variant.Part[0].ID) + "/indexes/sd/"
			return func(ctx context.Context, at time.Duration) ([]byte, error) {
				data, err := p.previewBytes(ctx, base+strconv.FormatInt(max(0, at.Milliseconds()), 10), nil, p.auth(), 2<<20)
				if err != nil {
					return nil, err
				}
				return jpegPreview(data)
			}, nil
		}
	}
	return nil, ErrNoStream
}

func jpegPreview(data []byte) ([]byte, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Width > 4096 || cfg.Height <= 0 || cfg.Height > 4096 {
		return nil, ErrResponse
	}
	return data, nil
}
