package media

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlexSearchCreditsFiltersHubsBySection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hubs/search" || r.URL.Query().Get("sectionId") != "4" ||
			r.URL.Query().Get("query") != "Test Person" || r.Header.Get("X-Plex-Token") != "fixture-token" {
			t.Error("unexpected Plex credit search request")
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"MediaContainer":{"Hub":[
			{"Metadata":[
				{"ratingKey":"11","type":"movie","librarySectionID":4,"title":"Visible movie","reason":"actor","reasonTitle":"Test Person"},
				{"ratingKey":"12","type":"movie","librarySectionID":5,"title":"Hidden movie","reason":"actor","reasonTitle":"Test Person"},
				{"ratingKey":"13","type":"movie","title":"Unscoped movie","reason":"actor","reasonTitle":"Test Person"},
				{"ratingKey":"14","type":"movie","librarySectionID":4,"title":"Other reason","reason":"genre"}
			]},
			{"Directory":[
				{"ratingKey":"15","type":"show","librarySectionID":4,"title":"Visible series","reason":"director","reasonTitle":"Test Person"}
			]}
		]}}`)
	}))
	defer server.Close()
	client, err := NewClient(Plex, server.URL, NewDevice("Waygu test"), Session{Token: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.(CreditSearchClient).SearchCreditTitles(t.Context(), Query{LibraryID: "4", Search: "Test Person"})
	if err != nil || len(page.Items) != 2 || page.Total != 2 {
		t.Fatalf("scoped credit search returned %d items: %v", len(page.Items), err)
	}
	if page.Items[0].ID != "11" || page.Items[0].People[0].Kind != "actor" || page.Items[1].ID != "15" ||
		len(page.Items[1].People) != 1 || !page.Items[1].People[0].Director {
		t.Fatalf("credit results are missing a scoped movie or series: %+v", page.Items)
	}
}
