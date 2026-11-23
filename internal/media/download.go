package media

import (
	"context"
	"net/http"
	"net/url"
)

// TranscodedDownload requests a complete server-encoded representation of
// the selected version, starting at the beginning of the item.
func TranscodedDownload(ctx context.Context, client Client, item Item, version PlaybackOptions, height, bitrate int) (Stream, error) {
	if height <= 0 || bitrate <= 128_000 {
		return Stream{}, ErrNoStream
	}
	switch client.(type) {
	case *mediaBrowser, *plexClient:
	default:
		return Stream{}, ErrNoStream
	}
	subtitleOff := -1
	version.MaxHeight, version.MaxBitrate = height, bitrate
	version.ForceTranscode, version.download = true, true
	version.AudioIndex, version.SubtitleIndex = nil, &subtitleOff
	return client.Playback(ctx, item, version)
}

// StopPrivateTranscode releases a preview or download's encoding session
// without reporting a playback event.
func StopPrivateTranscode(ctx context.Context, client Client, stream Stream) error {
	if stream.SessionID == "" || (stream.Method != Transcode && stream.Method != DirectStream) {
		return nil
	}
	switch server := client.(type) {
	case *mediaBrowser:
		query := url.Values{"DeviceId": {server.device.ID}, "PlaySessionId": {stream.SessionID}}
		return server.request(ctx, http.MethodDelete, "Videos/ActiveEncodings", query, server.auth(), nil, nil)
	case *plexClient:
		query := url.Values{"session": {stream.SessionID}}
		return server.request(ctx, http.MethodGet, "/video/:/transcode/universal/stop", query, server.auth(), nil, nil)
	}
	return nil
}
