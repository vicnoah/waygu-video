package main

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"waygu-video/internal/casting"
	"waygu-video/internal/media"
)

func rendererSource(stream media.Stream) casting.Stream {
	address, headers := stream.File.URL, stream.File.Headers
	if address == "" && stream.Method == media.DirectPlay {
		address, headers = stream.URL, stream.Headers
	}
	container := strings.ToLower(strings.TrimPrefix(path.Ext(stream.File.Name), "."))
	if container == "" {
		container = strings.ToLower(stream.Rendition.Container)
	}
	kind := ""
	switch container {
	case "mp4", "m4v":
		kind = "video/mp4"
	case "mov":
		kind = "video/quicktime"
	case "matroska", "mkv":
		kind = "video/x-matroska"
	case "ts", "m2ts", "mpegts":
		kind = "video/mp2t"
	case "webm":
		kind = "video/webm"
	case "avi":
		kind = "video/x-msvideo"
	case "mpeg", "mpg", "vob":
		kind = "video/mpeg"
	case "asf", "wmv":
		kind = "video/x-ms-wmv"
	}
	if kind == "" && address != "" && stream.Method == media.DirectPlay && strings.Contains(strings.ToLower(address), ".mp4") {
		kind = "video/mp4"
	}
	return casting.Stream{URL: address, Headers: headers, MIME: kind, Trust: stream.Trust}
}

func startRenderer(ctx context.Context, item media.Item, medium playerMedia, device casting.Renderer, at time.Duration, paused bool) (*casting.Session, media.Stream, error) {
	formats, err := device.Formats(ctx)
	if err != nil && ctx.Err() != nil {
		return nil, media.Stream{}, ctx.Err()
	}
	file := rendererSource(medium.stream)
	if file.URL != "" && file.MIME != "" && (len(formats) == 0 || formats.Supports(file.MIME)) {
		session, err := casting.Start(ctx, device, file, item.Title, at, paused)
		if err == nil || !errors.Is(err, casting.ErrFormat) {
			return session, medium.stream, err
		}
	}
	var profile string
	for _, kind := range []string{"application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl"} {
		if formats.Supports(kind) {
			profile = kind
			break
		}
	}
	if profile == "" || medium.client == nil {
		return nil, media.Stream{}, casting.ErrFormat
	}
	noSubtitle := -1
	stream, err := medium.client.Playback(ctx, item, media.PlaybackOptions{
		SourceID: medium.stream.VersionID(), ForceTranscode: true, MaxBitrate: 8_000_000, SubtitleIndex: &noSubtitle,
	})
	if err != nil {
		return nil, media.Stream{}, err
	}
	if stream.Method != media.Transcode || stream.URL == "" {
		return nil, media.Stream{}, casting.ErrFormat
	}
	session, err := casting.Start(ctx, device, casting.Stream{URL: stream.URL, Headers: stream.Headers, MIME: profile, Trust: stream.Trust}, item.Title, at, paused)
	return session, stream, err
}
