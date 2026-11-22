package media

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlexHLSStartsWithTranscodeDecision(t *testing.T) {
	const transcode = "/video/:/transcode/universal/"
	decisions := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		switch r.URL.Path {
		case "/library/metadata/7":
			_, _ = io.WriteString(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"7","duration":18000,
				"Media":[{"id":11,"width":1280,"height":720,"bitrate":1500,"container":"mp4","videoCodec":"h264","audioCodec":"aac",
					"Part":[{"id":13,"key":"/library/parts/13/file.mp4","duration":18000}]}]}]}}`)
		case transcode + "decision":
			if r.Header.Get("X-Plex-Token") != "fixture-token" || query.Get("session") == "" ||
				query.Get("X-Plex-Session-Identifier") != query.Get("session") ||
				query.Get("videoResolution") != "854x480" || query.Get("maxVideoBitrate") != "4000" {
				http.Error(w, "invalid transcode decision", http.StatusBadRequest)
				return
			}
			decisions <- query.Get("session")
			_, _ = io.WriteString(w, `{"MediaContainer":{"generalDecisionCode":1001}}`)
		case transcode + "start.m3u8":
			select {
			case decided := <-decisions:
				if query.Get("session") == decided && query.Get("videoResolution") == "854x480" &&
					query.Get("maxVideoBitrate") == "4000" && query.Get("directStream") == "0" {
					_, _ = io.WriteString(w, "#EXTM3U\n")
					return
				}
			default:
			}
			http.Error(w, "missing transcode decision", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(Plex, server.URL, NewDevice("Plex transcode test"), Session{Token: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Playback(t.Context(), Item{ID: "7"}, PlaybackOptions{MaxHeight: 480, MaxBitrate: 4_000_000})
	if err != nil || stream.Method != Transcode {
		t.Fatalf("expected a negotiated HLS stream, got %s, %v", stream.Method, err)
	}
	response, err := server.Client().Get(stream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HLS start after negotiation returned HTTP %d", response.StatusCode)
	}
}
