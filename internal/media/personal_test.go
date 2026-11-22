package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// FnMovie's favorite/list and item/favorite and Feiniu TV Lite's item/watched
// use the same signed media API and the item_guid field.
func TestFlymooPersonalProtocol(t *testing.T) {
	requests := []struct {
		method, route, body, response string
	}{
		{"PUT", "item/favorite", `{"item_guid":"film"}`, `{"code":0}`},
		{"POST", "favorite/list", `{"ancestor_guid":"movies","page":1,"page_size":20}`,
			`{"code":0,"data":{"total":1,"list":[{"guid":"film","title":"Film","type":"Movie","mdb_guid":"movies"}]}}`},
		{"DELETE", "item/favorite", `{"item_guid":"film"}`, `{"code":0}`},
		{"POST", "item/watched", `{"item_guid":"film"}`, `{"code":0}`},
		{"POST", "item/list", `{"ancestor_guid":"movies","page":1,"page_size":20,"tags":{"watched":"1"}}`,
			`{"code":0,"data":{"total":1,"list":[{"guid":"film","title":"Film","type":"Movie","mdb_guid":"movies"}]}}`},
		{"DELETE", "item/watched", `{"item_guid":"film"}`, `{"code":0}`},
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(calls.Add(1)) - 1
		if i >= len(requests) {
			t.Error("unexpected additional personal API request")
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		want := requests[i]
		if got := strings.TrimPrefix(r.URL.Path, "/media/api/v1/"); got != want.route || r.Method != want.method {
			t.Errorf("request %d was %s %s, want %s %s", i, r.Method, r.URL.Path, want.method, want.route)
		}
		if r.Header.Get("authx") == "" || r.Header.Get("Authorization") == "" {
			t.Error("missing signed account headers")
		}
		var expected, actual any
		_ = json.Unmarshal([]byte(want.body), &expected)
		if err := json.NewDecoder(r.Body).Decode(&actual); err != nil || !reflect.DeepEqual(actual, expected) {
			t.Errorf("request %d body = %#v, want %#v", i, actual, expected)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(want.response))
	}))
	defer server.Close()
	c, err := NewClient(Flymoo, server.URL+"/media", NewDevice("Personal review"), Session{Token: "local-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	client := c.(*flymooClient)
	item, query := Item{ID: "film"}, Query{LibraryID: "movies", Limit: 20}
	if err := client.SetFavorite(ctx, item, true); err != nil {
		t.Fatal(err)
	}
	favorites, err := client.Favorites(ctx, query)
	if err != nil || len(favorites.Items) != 1 || !favorites.Items[0].Favorite || favorites.Items[0].ID != item.ID {
		t.Fatalf("favorite page = %+v, %v", favorites, err)
	}
	if err := client.SetFavorite(ctx, item, false); err != nil {
		t.Fatal(err)
	}
	if err := client.SetWatched(ctx, item, true); err != nil {
		t.Fatal(err)
	}
	history, err := client.History(ctx, Library{ID: "movies"}, query)
	if err != nil || len(history.Items) != 1 || !history.Items[0].Watched || history.Items[0].ID != item.ID {
		t.Fatalf("watched page = %+v, %v", history, err)
	}
	if err := client.SetWatched(ctx, item, false); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != int32(len(requests)) {
		t.Fatalf("personal requests = %d", calls.Load())
	}
}
