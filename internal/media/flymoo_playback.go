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

func (f *flymooClient) Playback(ctx context.Context, item Item, _ PlaybackOptions) (Stream, error) {
	info, err := f.playInfo(ctx, item.ID)
	if err != nil {
		return Stream{}, err
	}
	source := flymooText(info, "media_guid")
	if source == "" {
		return Stream{}, ErrNoStream
	}
	body := struct {
		MediaGUID string              `json:"media_guid"`
		IP        string              `json:"ip"`
		Header    map[string][]string `json:"header"`
	}{source, "fnmovie-windows", map[string][]string{"User-Agent": {"FnMovie/0.1"}}}
	data, err := f.call(ctx, http.MethodPost, "stream", nil, body, f.session.Token)
	if err != nil {
		return Stream{}, err
	}
	result := flymooObject(data)
	video := flymooObject(result["video_stream"])
	duration := flymooDuration(flymooNumber(video, "duration"))
	position := flymooResume(flymooDuration(flymooNumber(info, "ts")), duration)
	direct, err := f.flymooDirectURL(result)
	if err != nil {
		return Stream{}, err
	}
	if direct == nil {
		direct, err = f.resolve("api/v1/media/range/"+url.PathEscape(source), nil)
		if err != nil {
			return Stream{}, err
		}
	}
	headers := f.auth(f.session.Token)
	headers.Set("User-Agent", "FnMovie/0.1")
	return Stream{ItemID: item.ID, SourceID: source, Duration: duration, Position: position,
		Method: DirectPlay, URL: direct.String(), Headers: headers}, nil
}

func (f *flymooClient) flymooDirectURL(result map[string]any) (*url.URL, error) {
	qualities, _ := result["direct_link_qualities"].([]any)
	if len(qualities) == 0 {
		qualities, _ = result["qualities"].([]any)
	}
	var chosen string
	var bitrate float64
	for _, entry := range qualities {
		quality := flymooObject(entry)
		link := flymooText(quality, "url")
		rate := flymooNumber(quality, "bitrate")
		if link != "" && (chosen == "" || rate > bitrate) {
			chosen, bitrate = link, rate
		}
	}
	if chosen == "" {
		return nil, nil
	}
	link, err := url.Parse(chosen)
	if err != nil || link.User != nil {
		return nil, ErrResponse
	}
	link = f.base.ResolveReference(link)
	if !isHTTP(link) {
		return nil, ErrResponse
	}
	if !sameOrigin(link, f.base) {
		return nil, nil
	}
	return link, nil
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
	}{event.Stream.SourceID, event.Stream.ItemID, int64(max(0, event.Position) / time.Second)}
	_, err := f.call(ctx, http.MethodPost, "play/record", nil, body, f.session.Token)
	return err
}
