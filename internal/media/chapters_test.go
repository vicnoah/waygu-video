package media

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestChaptersUseSelectedVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/clip/PlaybackInfo":
			if r.URL.Query().Get("MediaSourceId") != "alternate" {
				t.Error("Emby did not request the selected version")
			}
			io.WriteString(w, `{"MediaSources":[{"ID":"primary","ItemID":"clip"},{"ID":"alternate","ItemID":"second"}]}`)
		case "/Users/viewer/Items/second", "/Items/second":
			if r.URL.Query().Get("Fields") != "Chapters" {
				t.Error("chapter response fields were omitted")
			}
			io.WriteString(w, `{"Chapters":[
{"Name":"Late","StartPositionTicks":134500000},
{"Name":"Title","StartPositionTicks":0},
{"Name":"Intro skip","MarkerType":"IntroStart","StartPositionTicks":100000000},
{"Name":"Repeated","StartPositionTicks":134500000},
{"Name":"Invalid","StartPositionTicks":-1},
{"Name":"Overflow","StartPositionTicks":9223372036854775807}]}`)
		case "/library/metadata/clip":
			if r.URL.Query().Get("includeChapters") != "1" {
				t.Error("Plex did not request chapter metadata")
			}
			io.WriteString(w, `{"MediaContainer":{"Metadata":[{"RatingKey":"clip","Chapter":[{"Tag":"Primary only","StartTimeOffset":3000}],
"Media":[{"ID":12,"Part":[{"ID":44}]},{"ID":13,"Part":[{"ID":45,"Chapter":[{"Tag":"Title","StartTimeOffset":0},{"Tag":"Late","StartTimeOffset":13450}]}]},
{"ID":14,"Part":[{"ID":46}]}]}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	for _, test := range []struct {
		provider Provider
		source   string
	}{
		{Emby, "alternate"}, {Jellyfin, "second"}, {Plex, "13"},
	} {
		t.Run(string(test.provider), func(t *testing.T) {
			client, err := NewClient(test.provider, server.URL, NewDevice("Chapter test"), Session{UserID: "viewer", Token: "test-token"})
			if err != nil {
				t.Fatal(err)
			}
			provider := client.(ChapterClient)
			item := Item{ID: "clip"}
			got, err := provider.Chapters(t.Context(), item, test.source)
			want := []Chapter{{"Title", 0}, {"Late", 13450 * time.Millisecond}}
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("chapters: %v, %v, want %v", got, err, want)
			}
			if test.provider == Plex {
				got, err := provider.Chapters(t.Context(), item, "14")
				if err != nil || len(got) != 0 {
					t.Fatalf("primary chapters leaked into another version: %v, %v", got, err)
				}
			}
		})
	}
}
