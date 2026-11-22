package media

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// AudioSampleClient opens mono, 16 kHz PCM WAV from a position in the selected cut.
type AudioSampleClient interface {
	OpenAudioSample(context.Context, Item, Stream, time.Duration) (io.ReadCloser, error)
}

func (m *mediaBrowser) OpenAudioSample(ctx context.Context, item Item, stream Stream, start time.Duration) (io.ReadCloser, error) {
	query := m.userQuery()
	query.Set("MediaSourceId", stream.SourceID)
	query.Set("StartTimeTicks", strconv.FormatInt(int64(start/100), 10))
	query.Set("Static", "false")
	query.Set("AudioCodec", "pcm_s16le")
	query.Set("AudioChannels", "1")
	query.Set("AudioSampleRate", "16000")
	query.Set("AudioBitRate", "256000")
	query.Set("DeviceId", m.device.ID)
	// Progressive transcodes are keyed by session, so each offset needs its own file.
	query.Set("PlaySessionId", rand.Text())
	endpoint, err := m.resolve("Audio/"+url.PathEscape(item.ID)+"/stream.wav", query)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header = m.auth()
	request.Header.Set("Accept", "audio/wav")
	response, err := m.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, StatusError(response.StatusCode)
	}
	return response.Body, nil
}
