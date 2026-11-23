package media

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlexTitleRatings(t *testing.T) {
	for _, variant := range []struct {
		name, wire string
		want       TitleRatings
	}{
		{"array", `"Rating":[
			{"image":"imdb://image.rating","type":"audience","value":8.5},
			{"image":"themoviedb://image.rating","type":"audience","value":"8.23"},
			{"image":"imdb://image.rating","type":"critic","value":2},
			{"image":"themoviedb://image.rating","type":"audience","value":80}
		],"audienceRating":7,"audienceRatingImage":"imdb://image.rating"`, TitleRatings{IMDb: 8.5, TMDB: 8.23}},
		{"legacy", `"ratingImage":"themoviedb://image.rating","audienceRating":"7.9","audienceRatingImage":"imdb://image.rating"`, TitleRatings{IMDb: 7.9, TMDB: 9.2}},
		{"unknown", `"audienceRating":7.9,"Guid":[{"id":"imdb://tt1234567"}]`, TitleRatings{}},
	} {
		t.Run(variant.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/library/metadata/film" {
					http.NotFound(w, r)
					return
				}
				fmt.Fprintf(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"film","type":"movie","title":"Test film","rating":9.2,%s}]}}`, variant.wire)
			}))
			defer server.Close()
			client, err := NewClient(Plex, server.URL, NewDevice("ratings test"), Session{Token: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			movie, err := client.Detail(t.Context(), Item{ID: "film", Kind: Movie})
			if err != nil || movie.Rating != 9.2 || movie.Ratings != variant.want {
				t.Fatalf("Detail returned generic %v and named %+v, want 9.2 and %+v: %v", movie.Rating, movie.Ratings, variant.want, err)
			}
		})
	}
}
