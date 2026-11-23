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

// StopDownloadTranscode releases a segmented download's private session
// without reporting it as a playback event.
func StopDownloadTranscode(ctx context.Context, client Client, stream Stream) {
	if plex, ok := client.(*plexClient); ok {
		query := url.Values{"session": {stream.SessionID}}
		_ = plex.request(ctx, http.MethodGet, "/video/:/transcode/universal/stop", query, plex.auth(), nil, nil)
	}
}
