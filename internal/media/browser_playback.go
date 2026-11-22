package media

import (
	"context"
	"crypto/rand"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type browserStream struct {
	Index          int
	Type           string
	Codec          string
	Language       string
	DisplayTitle   string
	VideoRangeType string
	Width          int
	Height         int
	IsExternal     bool
	IsDefault      bool
	IsForced       bool
}

type browserSource struct {
	ID                      string
	ItemID                  string
	Name                    string
	Path                    string
	Container               string
	Size                    int64
	RunTimeTicks            int64
	Bitrate                 int
	DefaultAudioStreamIndex *int
	SupportsDirectPlay      bool
	SupportsDirectStream    bool
	TranscodingUrl          string
	MediaStreams            []browserStream
}

type browserPlaybackInfo struct {
	PlaySessionId string
	MediaSources  []browserSource
}

func (m *mediaBrowser) Versions(ctx context.Context, item Item) ([]Version, error) {
	var dto browserPlaybackInfo
	if err := m.get(ctx, "Items/"+url.PathEscape(item.ID)+"/PlaybackInfo", m.userQuery(), &dto); err != nil {
		return nil, err
	}
	var versions []Version
	for _, source := range dto.MediaSources {
		if source.ID != "" {
			version := source.version()
			version.Tracks = m.browserTracks(item, source, source.DefaultAudioStreamIndex)
			versions = append(versions, version)
		}
	}
	return versions, nil
}

func (source browserSource) version() Version {
	version := Version{ID: source.ID, Name: source.Name, Size: source.Size, Bitrate: source.Bitrate,
		Container: source.Container, FileName: videoFileName(source.Path)}
	for _, stream := range source.MediaStreams {
		if strings.EqualFold(stream.Type, "video") {
			version.Width, version.Height, version.VideoCodec = stream.Width, stream.Height, stream.Codec
			version.VideoRange = stream.VideoRangeType
			break
		}
	}
	return version
}

func (m *mediaBrowser) Playback(ctx context.Context, item Item, options PlaybackOptions) (Stream, error) {
	bitrate := options.MaxBitrate
	if bitrate <= 0 {
		bitrate = 100_000_000
	}
	direct := options.Direct
	if len(direct) == 0 {
		direct = []DirectProfile{{Container: "mp4,mov,m4v", VideoCodec: "h264", AudioCodec: "aac"}}
	}
	profiles := make([]map[string]string, 0, len(direct))
	for _, profile := range direct {
		profiles = append(profiles, map[string]string{
			"Type": "Video", "Container": profile.Container,
			"VideoCodec": profile.VideoCodec, "AudioCodec": profile.AudioCodec,
		})
	}
	deviceProfile := map[string]any{
		"Name": "Waygu Video", "MaxStreamingBitrate": bitrate, "DirectPlayProfiles": profiles,
		"TranscodingProfiles": []map[string]any{{
			"Type": "Video", "Context": "Streaming", "Protocol": "hls", "Container": "ts",
			"VideoCodec": "h264", "AudioCodec": "aac", "MaxAudioChannels": "2",
		}},
		"SubtitleProfiles": []map[string]string{
			{"Format": "srt", "Method": "External"}, {"Format": "vtt", "Method": "External"},
		},
	}
	params := m.userQuery()
	params.Set("MaxStreamingBitrate", strconv.Itoa(bitrate))
	body := map[string]any{
		"UserId": m.session.UserID, "DeviceProfile": deviceProfile, "MaxStreamingBitrate": bitrate,
		"EnableDirectPlay": true, "EnableDirectStream": true, "EnableTranscoding": true,
		"AllowVideoStreamCopy": true, "AllowAudioStreamCopy": true,
	}
	if options.SourceID != "" {
		params.Set("MediaSourceId", options.SourceID)
		body["MediaSourceId"] = options.SourceID
	}
	if options.AudioIndex != nil {
		params.Set("AudioStreamIndex", strconv.Itoa(*options.AudioIndex))
		body["AudioStreamIndex"] = *options.AudioIndex
		// A static file leaves audio selection to the native decoder. Request
		// a stream with the chosen audio instead.
		params.Set("EnableDirectPlay", "false")
		params.Set("EnableDirectStream", "false")
		body["EnableDirectPlay"] = false
		body["EnableDirectStream"] = false
	}
	if options.SubtitleIndex != nil {
		params.Set("SubtitleStreamIndex", strconv.Itoa(*options.SubtitleIndex))
		body["SubtitleStreamIndex"] = *options.SubtitleIndex
	}
	var dto browserPlaybackInfo
	if err := m.post(ctx, "Items/"+url.PathEscape(item.ID)+"/PlaybackInfo", params, body, &dto); err != nil {
		return Stream{}, err
	}
	versions := make([]Version, len(dto.MediaSources))
	for i, source := range dto.MediaSources {
		versions[i] = source.version()
	}
	for _, index := range versionOrder(versions, options) {
		source := dto.MediaSources[index]
		if options.SourceID != "" && options.SourceID != source.ID {
			continue
		}
		if stream, ok := m.browserStream(item, source, dto.PlaySessionId, bitrate, options.AudioIndex); ok {
			return stream, nil
		}
	}
	return Stream{}, ErrNoStream
}

func (m *mediaBrowser) browserStream(item Item, source browserSource, sessionID string, bitrate int, audio *int) (Stream, bool) {
	if source.ID == "" {
		return Stream{}, false
	}
	if sessionID == "" {
		sessionID = rand.Text()
	}
	params := url.Values{"Static": {"true"}, "MediaSourceId": {source.ID},
		"DeviceId": {m.device.ID}, "PlaySessionId": {sessionID}}
	original := m.mediaURL("Videos/"+url.PathEscape(item.ID)+"/stream", params)
	stream := Stream{ItemID: item.ID, SourceID: source.ID, SessionID: sessionID,
		Position: item.Position, Duration: max(item.Duration, fromTicks(source.RunTimeTicks)),
		Rendition: source.version(),
		File:      SourceFile{Name: videoFileName(source.Path), Size: source.Size, URL: original}}
	if audio != nil {
		if !slices.ContainsFunc(source.MediaStreams, func(stream browserStream) bool {
			return stream.Index == *audio && strings.EqualFold(stream.Type, "audio")
		}) {
			return Stream{}, false
		}
	}
	if audio == nil && source.SupportsDirectPlay && source.Bitrate <= bitrate {
		stream.URL = original
		stream.Method = DirectPlay
	} else if source.TranscodingUrl != "" {
		stream.URL = m.mediaURL(source.TranscodingUrl, nil)
		stream.Headers = m.auth()
		stream.Method = Transcode
		if source.SupportsDirectStream && source.Bitrate <= bitrate {
			play, _ := url.Parse(source.TranscodingUrl)
			if play != nil && strings.EqualFold(play.Query().Get("TranscodeReasons"), "ContainerNotSupported") {
				stream.Method = DirectStream
			}
		}
	}
	if stream.URL == "" {
		return Stream{}, false
	}
	selected := audio
	if selected == nil && stream.Method != DirectPlay {
		selected = source.DefaultAudioStreamIndex
	}
	stream.Tracks = m.browserTracks(item, source, selected)
	return stream, true
}

func (m *mediaBrowser) browserTracks(item Item, source browserSource, selected *int) []Track {
	var tracks []Track
	for _, entry := range source.MediaStreams {
		kind := strings.ToLower(entry.Type)
		if kind != "audio" && kind != "subtitle" {
			continue
		}
		track := Track{ID: strconv.Itoa(entry.Index), Type: kind, Language: entry.Language,
			Title: entry.DisplayTitle, Codec: entry.Codec, Default: entry.IsDefault, Forced: entry.IsForced, External: entry.IsExternal}
		if kind == "audio" {
			track.Selected = selected != nil && *selected == entry.Index
		} else if textSubtitle(entry.Codec) {
			track.URL = m.mediaURL("Videos/"+url.PathEscape(item.ID)+"/"+url.PathEscape(source.ID)+
				"/Subtitles/"+track.ID+"/Stream.vtt", nil)
		}
		tracks = append(tracks, track)
	}
	return tracks
}

func textSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "srt", "subrip", "vtt", "webvtt", "ass", "ssa", "mov_text", "tx3g", "text":
		return true
	}
	return false
}

func (m *mediaBrowser) mediaURL(route string, params url.Values) string {
	u, err := m.streamURL(route, params)
	if err != nil {
		return ""
	}
	query := u.Query()
	if m.session.Token != "" {
		query.Set(m.tokenKey(), m.session.Token)
	}
	u.RawQuery = query.Encode()
	return u.String()
}
