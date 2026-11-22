package media

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestPlaybackMarkersUseProviderRangesForSelectedCut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body string
		switch r.URL.Path {
		case "/Items/clip/PlaybackInfo":
			if r.URL.Query().Get("MediaSourceId") != "alternate" {
				t.Error("Emby did not request the selected source")
			}
			body = `{"MediaSources":[{"ID":"primary","ItemID":"clip"},{"ID":"alternate","ItemID":"second","Chapters":[
{"Name":"Intro","StartPositionTicks":0},
{"MarkerType":"IntroEnd","StartPositionTicks":95000000},
{"MarkerType":"IntroStart","StartPositionTicks":42500000},
{"MarkerType":"CreditsStart","StartPositionTicks":510000000}]}]}`
		case "/MediaSegments/second":
			if got := r.URL.Query()["includeSegmentTypes"]; !slices.Equal(got, []string{"Intro", "Outro"}) {
				t.Errorf("Jellyfin segment types = %v", got)
			}
			body = `{"Items":[{"Type":"Intro","StartTicks":42500000,"EndTicks":95000000},
{"Type":"Recap","StartTicks":0,"EndTicks":10000000},
{"Type":"Outro","StartTicks":650000000,"EndTicks":700000000},
{"Type":"Outro","StartTicks":510000000,"EndTicks":600000000}]}`
		case "/library/metadata/clip":
			if r.URL.Query().Get("includeMarkers") != "1" || r.URL.Query().Get("includeChapters") != "1" {
				t.Error("Plex did not request markers and chapters")
			}
			body = `{"MediaContainer":{"Metadata":[{"RatingKey":"clip","Marker":[{"type":"intro","startTimeOffset":4250,"endTimeOffset":9500},
{"type":"credits","startTimeOffset":51000,"endTimeOffset":54000}],
"Media":[{"ID":12,"Part":[{"ID":44}]},{"ID":13,"Part":[{"ID":45,"Chapter":[{"Tag":"Opening","StartTimeOffset":1250},
{"Tag":"Act 1","StartTimeOffset":6500},{"Tag":"Ending","StartTimeOffset":56000},{"Tag":"Post-credits","StartTimeOffset":59000}]}]}]}]}}`
		default:
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	for _, test := range []struct {
		name     string
		provider Provider
		source   string
		want     []PlaybackMarker
	}{
		{"Emby selected source", Emby, "alternate", []PlaybackMarker{
			{MarkerIntro, 4250 * time.Millisecond, 9500 * time.Millisecond}, {MarkerCredits, 51 * time.Second, time.Minute}}},
		{"Jellyfin selected item", Jellyfin, "second", []PlaybackMarker{
			{MarkerIntro, 4250 * time.Millisecond, 9500 * time.Millisecond}, {MarkerCredits, 51 * time.Second, time.Minute}}},
		{"Plex primary", Plex, "12", []PlaybackMarker{
			{MarkerIntro, 4250 * time.Millisecond, 9500 * time.Millisecond}, {MarkerCredits, 51 * time.Second, 54 * time.Second}}},
		{"Plex alternate", Plex, "13", []PlaybackMarker{
			{MarkerIntro, 1250 * time.Millisecond, 6500 * time.Millisecond}, {MarkerCredits, 56 * time.Second, 59 * time.Second}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(test.provider, server.URL, NewDevice("Marker tests"), Session{UserID: "viewer", Token: "test-token"})
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.(MarkerClient).PlaybackMarkers(t.Context(), Item{ID: "clip"},
				Stream{SourceID: test.source, Duration: time.Minute})
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("markers: %v (%v), want %v", got, err, test.want)
			}
		})
	}
}
