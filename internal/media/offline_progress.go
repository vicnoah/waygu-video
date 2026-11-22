package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// OfflineProgress contains enough information to restore a checkpoint after
// the playback session has expired. Stream URLs and authentication are excluded.
type OfflineProgress struct {
	ItemID, MediaID              string
	Position, Duration, Previous time.Duration
	At                           time.Time
	PreviousWatched              bool
	Completed                    bool
}

type OfflineProgressClient interface {
	// SyncOfflineProgress applies the checkpoint or leaves a newer server record intact.
	SyncOfflineProgress(context.Context, OfflineProgress) error
}

var ErrProgressTargetGone = errors.New("the playback progress item is no longer available")

// SupersededBy gives a known newer server timestamp precedence. Servers
// without timestamps can only be reconciled against the last confirmed position.
func (update OfflineProgress) SupersededBy(remote Item) bool {
	if remote.LastPlayed.IsZero() {
		return (remote.Position != update.Previous && remote.Position != update.Position) ||
			(remote.Watched && !update.PreviousWatched && !update.Completed)
	}
	return remote.LastPlayed.After(update.At)
}

func offlineProgressReadError(err error) error {
	if errors.Is(err, StatusError(http.StatusNotFound)) {
		return ErrProgressTargetGone
	}
	return err
}

func (m *mediaBrowser) SyncOfflineProgress(ctx context.Context, update OfflineProgress) error {
	id := url.PathEscape(update.ItemID)
	route := "UserItems/" + id + "/UserData"
	query := m.userQuery()
	var data map[string]json.RawMessage
	if m.kind == Emby {
		// The Emby update DTO replaces flags and play count, so keep the
		// rest of the authenticated item's user data when updating progress.
		itemRoute := m.userRoute() + "/Items/" + id
		query.Set("Fields", m.itemFields())
		var item struct{ UserData map[string]json.RawMessage }
		if err := m.get(ctx, itemRoute, query, &item); err != nil {
			return offlineProgressReadError(err)
		}
		data, route, query = item.UserData, itemRoute+"/UserData", nil
	} else if err := m.get(ctx, route, query, &data); err != nil {
		return offlineProgressReadError(err)
	}
	if data == nil {
		return ErrResponse
	}
	var ticks int64
	var watched bool
	var clock string
	_ = json.Unmarshal(data["PlaybackPositionTicks"], &ticks)
	_ = json.Unmarshal(data["Played"], &watched)
	_ = json.Unmarshal(data["LastPlayedDate"], &clock)
	lastPlayed, _ := time.Parse(time.RFC3339Nano, clock)
	remote := Item{Position: fromTicks(ticks), LastPlayed: lastPlayed, Watched: watched}
	if update.SupersededBy(remote) {
		return nil
	}
	position := update.Position
	if update.Completed {
		position, watched = 0, true
	}
	if remote.Position == position && remote.Watched == watched && !lastPlayed.Before(update.At) {
		return nil
	}
	if m.kind == Jellyfin {
		data = make(map[string]json.RawMessage)
	}
	data["PlaybackPositionTicks"] = json.RawMessage(strconv.FormatInt(toTicks(position), 10))
	data["LastPlayedDate"] = json.RawMessage(strconv.Quote(update.At.UTC().Format(time.RFC3339Nano)))
	data["Played"] = json.RawMessage(strconv.FormatBool(watched))
	return m.post(ctx, route, query, data, nil)
}

func (p *plexClient) SyncOfflineProgress(ctx context.Context, update OfflineProgress) error {
	entry, err := p.metadata(ctx, update.ItemID)
	if err != nil {
		return offlineProgressReadError(err)
	}
	remote := p.item(entry)
	update.Position = update.Position.Truncate(time.Millisecond)
	update.Previous = update.Previous.Truncate(time.Millisecond)
	// Plex stores LastViewedAt with second precision. Preserve a different
	// checkpoint from the same second when it cannot be ordered reliably.
	if update.SupersededBy(remote) || (remote.LastPlayed.Equal(update.At.Truncate(time.Second)) &&
		remote.Position != update.Previous && remote.Position != update.Position) {
		return nil
	}
	if (update.Completed && remote.Watched && remote.Position == 0) || (!update.Completed && remote.Position == update.Position) {
		return nil
	}
	params := url.Values{"ratingKey": {update.ItemID}, "key": {plexItemRoute(update.ItemID)},
		"time": {strconv.FormatInt(update.Position.Milliseconds(), 10)}, "state": {"stopped"},
		"duration": {strconv.FormatInt(update.Duration.Milliseconds(), 10)}, "offline": {"1"},
		"updated": {strconv.FormatInt(update.At.Unix(), 10)}}
	return p.reportTimeline(ctx, params, p.auth())
}

func (f *flymooClient) SyncOfflineProgress(ctx context.Context, update OfflineProgress) error {
	info, err := f.playInfo(ctx, update.ItemID)
	if err != nil {
		return offlineProgressReadError(err)
	}
	mediaID := flymooText(info, "media_guid")
	if mediaID == "" {
		return ErrNoStream
	}
	if mediaID != update.MediaID {
		return nil
	}
	position := flymooDuration(flymooNumber(info, "ts", "position", "playback_position")).Truncate(time.Second)
	if position == update.Position.Truncate(time.Second) || position != update.Previous.Truncate(time.Second) {
		return nil
	}
	return f.Report(ctx, PlaybackState{Event: Stopped, Stream: Stream{ItemID: update.ItemID, SourceID: mediaID}, Position: update.Position})
}
