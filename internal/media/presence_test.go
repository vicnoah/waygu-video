package media

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlymooItemPresence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "fixture-session" || r.Header.Get("authx") == "" {
			t.Error("item lookup did not use the authenticated, signed request")
		}
		switch r.URL.Path {
		case "/v/api/v1/item/present":
			fmt.Fprint(w, `{"code":0,"data":{"item":{"guid":"present"}}}`)
		case "/v/api/v1/item/deleted":
			fmt.Fprint(w, `{"code":-6,"msg":"not found"}`)
		case "/v/api/v1/item/file-removed":
			fmt.Fprint(w, `{"code":-11,"msg":"file does not exist"}`)
		case "/v/api/v1/item/permission":
			w.WriteHeader(http.StatusForbidden)
		case "/v/api/v1/item/wrong-item":
			fmt.Fprint(w, `{"code":0,"data":{"item":{"guid":"different"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(Flymoo, server.URL+"/v", Device{}, Session{Token: "fixture-session"})
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		id     string
		exists bool
		err    error
	}{
		{id: "present", exists: true},
		{id: "deleted"},
		{id: "file-removed"},
		{id: "unsupported", err: StatusError(http.StatusNotFound)},
		{id: "permission", err: ErrAuthentication},
		{id: "wrong-item", err: ErrResponse},
	} {
		t.Run(sample.id, func(t *testing.T) {
			exists, err := ItemExists(t.Context(), client, Item{ID: sample.id, Kind: Episode})
			if exists != sample.exists || !errors.Is(err, sample.err) {
				t.Fatalf("lookup = %t, %v, want %t, %v", exists, err, sample.exists, sample.err)
			}
		})
	}
}
