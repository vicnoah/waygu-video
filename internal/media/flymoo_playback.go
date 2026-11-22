package media

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"
)

func (f *flymooClient) playInfo(ctx context.Context, id string) (map[string]any, error) {
	body := struct {
		ID string `json:"item_guid"`
	}{id}
	data, err := f.call(ctx, http.MethodPost, "play/info", nil, body, f.session.Token)
	return flymooObject(data), err
}

func (f *flymooClient) loadPlayback(ctx context.Context, item Item) (flymooPlayback, error) {
	info, err := f.playInfo(ctx, item.ID)
	if err != nil {
		return flymooPlayback{}, err
	}
	source := flymooText(info, "media_guid")
	if source == "" {
		return flymooPlayback{}, ErrNoStream
	}
	body := struct {
		MediaGUID string              `json:"media_guid"`
		IP        string              `json:"ip"`
		Header    map[string][]string `json:"header"`
	}{source, "fnmovie-windows", map[string][]string{"User-Agent": {"FnMovie/0.1"}}}
	data, err := f.call(ctx, http.MethodPost, "stream", nil, body, f.session.Token)
	if err != nil {
		return flymooPlayback{}, err
	}
	result := flymooObject(data)
	video := flymooObject(result["video_stream"])
	duration := flymooDuration(flymooNumber(video, "duration"))
	position := flymooResume(flymooDuration(flymooNumber(info, "ts")), duration)
	original, err := f.resolve("api/v1/media/range/"+url.PathEscape(source), nil)
	if err != nil {
		return flymooPlayback{}, err
	}
	headers := f.auth(f.session.Token)
	headers.Set("User-Agent", "FnMovie/0.1")
	stream := Stream{ItemID: item.ID, SourceID: source, Duration: duration, Position: position, Headers: headers,
		File: SourceFile{Name: videoFileName(flymooText(info, "file_name", "filename")), URL: original.String(), Headers: headers}}
	return flymooPlayback{info: info, result: result, stream: stream, original: flymooQuality{
		Version: Version{ID: "original", Original: true, Method: DirectPlay, Width: flymooInt(video, "width"),
			Height: flymooInt(video, "height"), Bitrate: flymooInt(video, "bitrate"), VideoCodec: flymooText(video, "codec_name", "codec"),
			FileName: stream.File.Name},
		link: original,
	}}, nil
}

func flymooResume(position, duration time.Duration) time.Duration {
	if duration <= 0 {
		return position
	}
	threshold := min(120*time.Second, max(15*time.Second, duration/50))
	if duration-position <= threshold {
		return 0
	}
	return position
}

func (f *flymooClient) Resume(ctx context.Context) ([]Item, error) {
	data, err := f.call(ctx, http.MethodGet, "play/list", nil, nil, f.session.Token)
	if err != nil {
		return nil, err
	}
	rows := flymooRows(data, "list", "items", "results", "data")
	rows = rows[:min(len(rows), 24)]
	items := make([]Item, len(rows))
	errs := make([]error, len(rows))
	var workers sync.WaitGroup
	for start := range min(2, len(rows)) {
		workers.Go(func() {
			for i := start; i < len(rows) && ctx.Err() == nil; i += 2 {
				items[i], errs[i] = f.resumeItem(ctx, rows[i])
			}
		})
	}
	workers.Wait()
	resume := items[:0]
	for i, item := range items {
		if errs[i] == nil && item.ID != "" {
			resume = append(resume, item)
		}
	}
	return resume, errors.Join(ctx.Err(), errors.Join(errs...))
}

func (f *flymooClient) resumeItem(ctx context.Context, row map[string]any) (Item, error) {
	details := row
	if item, ok := row["item"].(map[string]any); ok {
		details = item
	}
	item := f.item(details)
	if item.LibraryID == "" {
		item.LibraryID, _ = flymooLibraryID(row)
	}
	recordID := flymooText(row, "item_guid", "guid")
	if recordID == "" {
		recordID = item.ID
	}
	if item.ID == "" {
		item.ID = recordID
	}
	if item.ID == "" || item.Title == "" {
		return Item{}, nil
	}
	info, err := f.playInfo(ctx, recordID)
	if err != nil {
		return Item{}, err
	}
	if item.Duration == 0 {
		item.Duration = flymooDuration(flymooNumber(row, "duration", "total_duration"))
	}
	if item.Duration == 0 {
		item.Duration = flymooDuration(flymooNumber(flymooObject(row["video_stream"]), "duration"))
	}
	item.Position = flymooDuration(flymooNumber(info, "ts", "position", "playback_position"))
	if item.Duration > 0 {
		item.Position = min(item.Position, item.Duration)
	}
	return item, nil
}

func (f *flymooClient) Report(ctx context.Context, event PlaybackState) error {
	body := struct {
		MediaGUID string `json:"media_guid"`
		ItemGUID  string `json:"item_guid"`
		Position  int64  `json:"ts"`
		PlayLink  string `json:"play_link,omitempty"`
	}{MediaGUID: event.Stream.SourceID, ItemGUID: event.Stream.ItemID, Position: int64(max(0, event.Position) / time.Second)}
	if event.Stream.Method == Transcode {
		body.PlayLink = event.Stream.URL
	}
	_, err := f.call(ctx, http.MethodPost, "play/record", nil, body, f.session.Token)
	return err
}
