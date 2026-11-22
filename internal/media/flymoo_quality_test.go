package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlymooSelectableQualities(t *testing.T) {
	for _, separate := range []bool{false, true} {
		name := "embedded qualities"
		if separate {
			name = "separate quality endpoint"
		}
		t.Run(name, func(t *testing.T) {
			var streamCalls, qualityCalls, transcodes atomic.Int32
			const presets = `[{"resolution":"1080","bitrate":8000000},{"resolution":"720","bitrate":3000000}]`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil ||
					r.Header.Get("Authorization") != "fixture-token" || r.Header.Get("authx") == "" {
					t.Error("unexpected signed Flymoo request")
				}
				if r.URL.Path != "/v/api/v1/play/info" && body["media_guid"] != "media-1" {
					t.Errorf("invalid media GUID for %s", r.URL.Path)
				}
				switch r.URL.Path {
				case "/v/api/v1/play/info":
					fmt.Fprint(w, `{"code":0,"data":{"media_guid":"media-1","video_guid":"video-1","audio_guid":"audio-1","ts":37}}`)
				case "/v/api/v1/stream":
					data := `{"video_stream":{"duration":125.5,"width":3840,"height":2160,"bitrate":20000000},` +
						`"direct_link_qualities":[{"resolution":"1080p","bitrate":6000000,"url":"/v/file.m3u8?v=%d"},` +
						`{"resolution":"4k","bitrate":24000000,"url":"https://external.invalid/file.m3u8"}]`
					if !separate {
						data += `,"qualities":` + presets
					}
					fmt.Fprintf(w, `{"code":0,"data":`+data+`}}`, streamCalls.Add(1))
				case "/v/api/v1/play/quality":
					qualityCalls.Add(1)
					fmt.Fprint(w, `{"code":0,"data":`+presets+`}`)
				case "/v/api/v1/play/play":
					transcodes.Add(1)
					if body["resolution"] != "720" || body["bitrate"] != float64(3000000) || body["video_guid"] != "video-1" ||
						body["audio_guid"] != "audio-1" || body["startTimestamp"] != float64(0) || body["without_stream"] != false {
						t.Errorf("unexpected play/play input: %v", body)
					}
					fmt.Fprint(w, `{"code":0,"data":{"play_link":"/v/hls/720.m3u8"}}`)
				case "/v/api/v1/play/record":
					link, _ := body["play_link"].(string)
					if body["item_guid"] != "item-1" || body["ts"] != float64(24) || link != "http://"+r.Host+"/v/hls/720.m3u8" {
						t.Errorf("unexpected transcoded playback report: %v", body)
					}
					fmt.Fprint(w, `{"code":0,"data":{}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client, err := NewClient(Flymoo, server.URL+"/v", Device{}, Session{Token: "fixture-token"})
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			item := Item{ID: "item-1", Kind: Movie}
			defaultStream, err := client.Playback(ctx, item, PlaybackOptions{})
			if err != nil || defaultStream.URL != server.URL+"/v/api/v1/media/range/media-1" ||
				defaultStream.Position != 37*time.Second || qualityCalls.Load() != 0 || transcodes.Load() != 0 {
				t.Fatalf("reference playback fallback changed: %v", err)
			}
			versions, err := client.(VersionClient).Versions(ctx, item)
			if err != nil || len(versions) != 4 || !versions[0].Original || versions[1].Method != DirectPlay ||
				versions[3].Method != Transcode || versions[3].Height != 720 {
				t.Fatalf("unexpected advertised versions: %+v, %v", versions, err)
			}
			direct, err := client.Playback(ctx, item, PlaybackOptions{SourceID: versions[1].ID})
			if err != nil || direct.VersionID() != versions[1].ID || direct.URL != server.URL+"/v/file.m3u8?v=3" {
				t.Fatalf("signed URLs changed the selected rendition ID: %v", err)
			}
			for _, options := range []PlaybackOptions{{SourceID: versions[3].ID}, {VersionPolicy: VersionFitBitrate, MaxBitrate: 4_000_000}} {
				stream, err := client.Playback(ctx, item, options)
				if err != nil || stream.URL != server.URL+"/v/hls/720.m3u8" || stream.Method != Transcode ||
					stream.VersionID() != versions[3].ID || stream.SourceID != "media-1" || stream.Position != 37*time.Second {
					t.Fatalf("open 720p: %v", err)
				}
				if err := client.Report(ctx, PlaybackState{Event: Progress, Stream: stream, Position: 24 * time.Second}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := client.Playback(ctx, item, PlaybackOptions{SourceID: "removed"}); !errors.Is(err, ErrNoStream) {
				t.Fatalf("unavailable version: %v", err)
			}
			if (qualityCalls.Load() > 0) != separate || transcodes.Load() != 2 {
				t.Errorf("unexpected optional endpoint calls: qualities=%d transcodes=%d", qualityCalls.Load(), transcodes.Load())
			}
		})
	}
}
