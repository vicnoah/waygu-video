package media

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDetailUsesTaggedLogo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/Items/movie") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"Id":"movie","Name":"Fixture","Type":"Movie","ImageTags":{"Primary":"poster-tag","Logo":"logo-tag"}}`)
	}))
	t.Cleanup(server.Close)
	for _, provider := range []Provider{Emby, Jellyfin} {
		t.Run(string(provider), func(t *testing.T) {
			client, err := NewClient(provider, server.URL, NewDevice("Waygu artwork test"), Session{UserID: "viewer", Token: "fixture-token"})
			if err != nil {
				t.Fatal(err)
			}
			detail, err := client.Detail(t.Context(), Item{ID: "movie"})
			if err != nil {
				t.Fatal(err)
			}
			logo, err := url.Parse(detail.Logo)
			if err != nil || !strings.HasSuffix(logo.Path, "/Items/movie/Images/Logo") || logo.Query().Get("tag") != "logo-tag" {
				t.Fatalf("logo = %q, want the tagged title logo", detail.Logo)
			}
		})
	}
}
