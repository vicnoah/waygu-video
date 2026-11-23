package media

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStopPrivateTranscodeTargetsOnlyItsSession(t *testing.T) {
	device := Device{ID: "fixture-device"}
	for _, provider := range []Provider{Emby, Jellyfin, Plex} {
		t.Run(string(provider), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				method, path := http.MethodDelete, "/Videos/ActiveEncodings"
				query := url.Values{"DeviceId": {device.ID}, "PlaySessionId": {"preview-session"}}
				if provider == Plex {
					method, path = http.MethodGet, "/video/:/transcode/universal/stop"
					query = url.Values{"session": {"preview-session"}}
				}
				if r.Method != method || !strings.HasSuffix(r.URL.Path, path) || r.URL.Query().Encode() != query.Encode() {
					t.Errorf("got %s %s, want %s %s with %v", r.Method, r.URL, method, path, query)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client, err := NewClient(provider, server.URL, device, Session{Token: "fixture-token"})
			if err != nil {
				t.Fatal(err)
			}
			for _, stream := range []Stream{
				{Method: DirectPlay, SessionID: "original-session"},
				{Method: Transcode},
				{Method: Transcode, SessionID: "preview-session"},
			} {
				if err := StopPrivateTranscode(t.Context(), client, stream); err != nil {
					t.Fatal(err)
				}
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("got %d stop requests, want 1", got)
			}
		})
	}
}
