package metadata

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"waygu-video/internal/media"
)

type testTransport func(*http.Request) (*http.Response, error)

func (t testTransport) RoundTrip(req *http.Request) (*http.Response, error) { return t(req) }

func TestEnrichChecksTitleIDsLanguageAndCache(t *testing.T) {
	calls := 0
	cache := NewCache(testTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "api.themoviedb.org" || req.Header.Get("Authorization") != "Bearer example-read-token" ||
			req.URL.Query().Get("language") != "ja-JP" || req.URL.Query().Has("api_key") {
			t.Fatal("unexpected authentication or locale for lookup")
		}
		var body string
		switch req.URL.Path {
		case "/3/movie/42":
			body = `{"title":"TMDB film","overview":"TMDB overview","poster_path":"/art.jpg","backdrop_path":"https://other.invalid/example.jpg","vote_average":8.23,"vote_count":10}`
		case "/3/find/tt1234567":
			if req.URL.Query().Get("external_source") != "imdb_id" {
				t.Fatal("the IMDb lookup did not request an IMDb match")
			}
			body = `{"movie_results":[{"title":"Other kind"}],"tv_results":[{"name":"TMDB series","backdrop_path":"/cover.png","vote_average":5,"vote_count":0}]}`
		default:
			t.Fatalf("unexpected route %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}))
	item := media.Item{ID: "fixture", LibraryID: "movies", Kind: media.Movie, Title: "Server film", ContentRating: "PG", Rating: 8.4,
		ExternalIDs: map[string]string{"tmdb": "42"}}
	enrich := func(item media.Item, overwrite bool) media.Item {
		t.Helper()
		result, err := cache.Enrich(t.Context(), item, "example-read-token", "ja-JP", overwrite)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	complete := item
	complete.Overview, complete.Poster, complete.Backdrop = "Server overview", "server-poster", "server-backdrop"
	if got := enrich(complete, false); got.Ratings.TMDB != 8.23 || got.Poster != complete.Poster || calls != 1 {
		t.Fatalf("score supplement for complete server metadata = %+v, requests = %d", got, calls)
	}
	fill := enrich(item, false)
	if fill.Title != "Server film" || fill.Overview != "TMDB overview" || fill.Poster != imageOrigin+"w500/art.jpg" || fill.Backdrop != "" {
		t.Fatalf("server-first fill = %+v", fill)
	}
	item.Ratings = media.TitleRatings{IMDb: 8.7, TMDB: 7.3}
	if got := enrich(item, false); got.Ratings != item.Ratings {
		t.Fatalf("server-first scores = %+v", got.Ratings)
	}
	preferred := enrich(item, true)
	if preferred.Title != "TMDB film" || preferred.ID != "fixture" || preferred.ContentRating != "PG" || preferred.Rating != 8.4 ||
		preferred.Ratings != (media.TitleRatings{IMDb: 8.7, TMDB: 8.23}) || preferred.LibraryID != "movies" || calls != 1 {
		t.Fatalf("TMDB-first result = %+v, requests = %d", preferred, calls)
	}
	series := enrich(media.Item{Kind: media.Series, ExternalIDs: map[string]string{"imdb": "tt1234567"}}, false)
	if series.Title != "TMDB series" || series.Backdrop != imageOrigin+"w1280/cover.png" || series.Ratings.TMDB != 0 {
		t.Fatalf("series = %+v", series)
	}
	enrich(media.Item{Kind: media.Movie, ExternalIDs: map[string]string{"imdb": "../../private"}}, true)
	if calls != 2 {
		t.Fatal("an invalid identifier reached the network")
	}
	cache.Clear()
	enrich(item, false)
	if calls != 3 {
		t.Fatal("cleared lookup was served from the cache")
	}
}
