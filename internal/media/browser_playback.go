package media

import (
	"context"
	"crypto/rand"
	"net/url"
	"strconv"
	"strings"
)

type browserStream struct {
	Index        int
	Type         string
	Codec        string
	Language     string
	DisplayTitle string
	DeliveryUrl  string
	IsExternal   bool
	IsDefault    bool
	IsForced     bool
}

type browserSource struct {
	ID                   string
	Container            string
	RunTimeTicks         int64
	Bitrate              int
	SupportsDirectPlay   bool
	SupportsDirectStream bool
	TranscodingUrl       string
	MediaStreams         []browserStream
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
	}
	if options.SubtitleIndex != nil {
		params.Set("SubtitleStreamIndex", strconv.Itoa(*options.SubtitleIndex))
		body["SubtitleStreamIndex"] = *options.SubtitleIndex
	}
	var dto struct {
		PlaySessionId string
		MediaSources  []browserSource
	}
	if err := m.post(ctx, "Items/"+url.PathEscape(item.ID)+"/PlaybackInfo", params, body, &dto); err != nil {
		return Stream{}, err
	}
	for _, source := range dto.MediaSources {
		if options.SourceID != "" && options.SourceID != source.ID {
			continue
		}
		if stream, ok := m.browserStream(item, source, dto.PlaySessionId, bitrate); ok {
			return stream, nil
		}
	}
	return Stream{}, ErrNoStream
}

func (m *mediaBrowser) browserStream(item Item, source browserSource, sessionID string, bitrate int) (Stream, bool) {
	if source.ID == "" {
		return Stream{}, false
	}
	if sessionID == "" {
		sessionID = rand.Text()
	}
	stream := Stream{ItemID: item.ID, SourceID: source.ID, SessionID: sessionID,
		Position: item.Position, Duration: max(item.Duration, fromTicks(source.RunTimeTicks))}
	if source.SupportsDirectPlay && source.Bitrate <= bitrate {
		params := url.Values{"Static": {"true"}, "MediaSourceId": {source.ID},
			"DeviceId": {m.device.ID}, "PlaySessionId": {sessionID}}
		stream.URL = m.mediaURL("Videos/"+url.PathEscape(item.ID)+"/stream", params)
		stream.Method = DirectPlay
	} else if source.TranscodingUrl != "" {
		stream.URL = m.mediaURL(source.TranscodingUrl, nil)
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
	for _, entry := range source.MediaStreams {
		kind := strings.ToLower(entry.Type)
		if kind != "audio" && kind != "subtitle" {
			continue
		}
		track := Track{ID: strconv.Itoa(entry.Index), Type: kind, Language: entry.Language,
			Title: entry.DisplayTitle, Codec: entry.Codec, Default: entry.IsDefault, Forced: entry.IsForced}
		if entry.DeliveryUrl != "" {
			track.URL = m.mediaURL(entry.DeliveryUrl, nil)
		}
		if track.URL == "" && kind == "subtitle" && entry.IsExternal {
			track.URL = m.mediaURL("Videos/"+url.PathEscape(item.ID)+"/"+url.PathEscape(source.ID)+
				"/Subtitles/"+track.ID+"/Stream.vtt", nil)
		}
		stream.Tracks = append(stream.Tracks, track)
	}
	return stream, true
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
