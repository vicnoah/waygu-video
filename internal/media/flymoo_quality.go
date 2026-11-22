package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type flymooPlayback struct {
	info, result map[string]any
	stream       Stream
	original     flymooQuality
}

type flymooQuality struct {
	Version
	link       *url.URL
	resolution string
}

func (f *flymooClient) Versions(ctx context.Context, item Item) ([]Version, error) {
	media, err := f.loadPlayback(ctx, item)
	if err != nil {
		return nil, err
	}
	qualities, err := f.playbackQualities(ctx, media)
	return flymooVersions(qualities), err
}

func (f *flymooClient) Playback(ctx context.Context, item Item, options PlaybackOptions) (Stream, error) {
	media, err := f.loadPlayback(ctx, item)
	if err != nil {
		return Stream{}, err
	}
	if options.SourceID == "original" || (options.SourceID == "" && options.VersionPolicy == VersionHighest) {
		return f.openQuality(ctx, media, media.original)
	}
	if options.SourceID == "" && (options.VersionPolicy != VersionFitBitrate || options.MaxBitrate <= 0) {
		quality, err := f.referenceQuality(media)
		if err != nil {
			return Stream{}, err
		}
		return f.openQuality(ctx, media, quality)
	}
	qualities, err := f.playbackQualities(ctx, media)
	if err != nil {
		return Stream{}, err
	}
	for _, index := range versionOrder(flymooVersions(qualities), options) {
		quality := qualities[index]
		if options.SourceID == "" || quality.ID == options.SourceID {
			stream, err := f.openQuality(ctx, media, quality)
			if options.SourceID == "" && (errors.Is(err, ErrNoStream) || errors.Is(err, StatusError(http.StatusNotFound))) {
				continue
			}
			return stream, err
		}
	}
	return Stream{}, ErrNoStream
}

func (f *flymooClient) referenceQuality(media flymooPlayback) (flymooQuality, error) {
	rows, _ := media.result["direct_link_qualities"].([]any)
	if len(rows) == 0 {
		rows, _ = media.result["qualities"].([]any)
	}
	var best map[string]any
	for _, entry := range rows {
		row := flymooObject(entry)
		if flymooText(row, "url") != "" && (best == nil || flymooNumber(row, "bitrate") > flymooNumber(best, "bitrate")) {
			best = row
		}
	}
	if best != nil {
		quality, err := f.directQuality(best)
		if err != nil || quality.link != nil {
			return quality, err
		}
	}
	return media.original, nil
}

func (f *flymooClient) playbackQualities(ctx context.Context, media flymooPlayback) ([]flymooQuality, error) {
	qualities := []flymooQuality{media.original}
	seen := map[string]bool{media.original.ID: true}
	appendQuality := func(quality flymooQuality) {
		if quality.ID != "" && !seen[quality.ID] {
			qualities = append(qualities, quality)
			seen[quality.ID] = true
		}
	}
	for _, row := range flymooRows(media.result["direct_link_qualities"]) {
		if quality, err := f.directQuality(row); err == nil && quality.link != nil {
			appendQuality(quality)
		}
	}
	rows := flymooRows(media.result["qualities"])
	if len(rows) == 0 {
		data, err := f.call(ctx, http.MethodPost, "play/quality", nil, struct {
			MediaGUID string `json:"media_guid"`
		}{media.stream.SourceID}, f.session.Token)
		if err != nil && !errors.Is(err, StatusError(http.StatusNotFound)) && !errors.Is(err, StatusError(http.StatusMethodNotAllowed)) {
			return nil, err
		}
		rows = flymooRows(data, "list", "qualities")
	}
	for _, row := range rows {
		if flymooText(row, "url") != "" {
			if quality, err := f.directQuality(row); err == nil && quality.link != nil {
				appendQuality(quality)
			}
			continue
		}
		quality := flymooRendition(row)
		if quality.resolution != "" && quality.Bitrate > 0 {
			quality.ID = "transcode:" + quality.resolution + ":" + strconv.Itoa(quality.Bitrate)
			quality.Method = Transcode
			appendQuality(quality)
		}
	}
	return qualities, nil
}

func (f *flymooClient) directQuality(row map[string]any) (flymooQuality, error) {
	address := flymooText(row, "url")
	if address == "" {
		return flymooQuality{}, nil
	}
	link, err := f.qualityLink(address)
	if err != nil || link == nil {
		return flymooQuality{}, err
	}
	quality := flymooRendition(row)
	// Use the media path and rendition metadata so refreshed URL tokens do not change the selection.
	key := fmt.Sprintf("%s\n%s\n%d", link.EscapedPath(), quality.resolution, quality.Bitrate)
	quality.ID = fmt.Sprintf("direct:%x", sha256.Sum256([]byte(key)))
	quality.Method = DirectPlay
	quality.link = link
	return quality, nil
}

func (f *flymooClient) qualityLink(address string) (*url.URL, error) {
	link, err := url.Parse(address)
	if err != nil || link.User != nil || link.Fragment != "" {
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

func flymooRendition(row map[string]any) flymooQuality {
	resolution := flymooText(row, "resolution")
	version := Version{Bitrate: flymooInt(row, "bitrate"), VideoCodec: flymooText(row, "codec_name", "codec"),
		Width: flymooInt(row, "width"), Height: flymooInt(row, "height")}
	if version.Height == 0 {
		label := strings.ToLower(resolution)
		switch label {
		case "4k":
			version.Height = 2160
		case "8k":
			version.Height = 4320
		default:
			width, height, dimensions := strings.Cut(label, "x")
			if dimensions {
				version.Width, _ = strconv.Atoi(width)
			} else {
				height = strings.TrimSuffix(label, "p")
			}
			version.Height, _ = strconv.Atoi(height)
		}
	}
	if version.Height <= 0 {
		version.Name = resolution
	}
	return flymooQuality{Version: version, resolution: resolution}
}

func flymooVersions(qualities []flymooQuality) []Version {
	versions := make([]Version, 0, len(qualities))
	for _, quality := range qualities {
		versions = append(versions, quality.Version)
	}
	return versions
}

func (f *flymooClient) openQuality(ctx context.Context, media flymooPlayback, quality flymooQuality) (Stream, error) {
	link := quality.link
	if link == nil {
		body := struct {
			MediaGUID      string `json:"media_guid"`
			VideoGUID      string `json:"video_guid,omitempty"`
			AudioGUID      string `json:"audio_guid,omitempty"`
			Resolution     string `json:"resolution"`
			Bitrate        int    `json:"bitrate"`
			StartTimestamp int    `json:"startTimestamp"`
			ForcedSDR      int    `json:"forced_sdr"`
			WithoutStream  bool   `json:"without_stream"`
		}{MediaGUID: media.stream.SourceID, Resolution: quality.resolution, Bitrate: quality.Bitrate,
			VideoGUID: flymooText(media.info, "video_guid"), AudioGUID: flymooText(media.info, "audio_guid")}
		data, err := f.call(ctx, http.MethodPost, "play/play", nil, body, f.session.Token)
		if err != nil {
			return Stream{}, err
		}
		if address := flymooText(flymooObject(data), "play_link"); address != "" {
			link, err = f.qualityLink(address)
			if err != nil {
				return Stream{}, err
			}
		}
		if link == nil {
			return Stream{}, ErrNoStream
		}
	}
	stream := media.stream
	stream.URL = link.String()
	stream.Method = quality.Method
	stream.Rendition = quality.Version
	return stream, nil
}
