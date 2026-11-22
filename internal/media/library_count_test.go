package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestLibraryCountWireContracts(t *testing.T) {
	for _, provider := range []Provider{Emby, Jellyfin, Plex, Flymoo} {
		t.Run(string(provider), func(t *testing.T) {
			var response atomic.Value
			response.Store("{}")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				switch provider {
				case Emby, Jellyfin:
					if r.Method != http.MethodGet || r.URL.Path != "/proxy/Items" || q.Get("UserId") != "viewer" ||
						q.Get("ParentId") != "test-library" || q.Get("IncludeItemTypes") != "Movie,Series,Video" ||
						q.Get("Recursive") != "true" || q.Get("EnableTotalRecordCount") != "true" || q.Get("Limit") != "1" ||
						q.Get("EnableImages") != "false" || q.Get("EnableUserData") != "false" {
						t.Errorf("unexpected MediaBrowser count request")
					}
				case Plex:
					if r.Method != http.MethodGet || r.URL.Path != "/proxy/library/sections/test-library/all" ||
						q.Get("X-Plex-Container-Start") != "0" || q.Get("X-Plex-Container-Size") != "0" ||
						r.Header.Get("X-Plex-Token") != "fixture-token" {
						t.Errorf("unexpected Plex count request")
					}
				case Flymoo:
					var body struct {
						Ancestor string `json:"ancestor_guid"`
						Page     int
						Size     int `json:"page_size"`
						Tags     map[string][]string
					}
					if r.Method != http.MethodPost || r.URL.Path != "/proxy/api/v1/item/list" ||
						r.Header.Get("Authorization") != "fixture-token" || r.Header.Get("authx") == "" ||
						json.NewDecoder(r.Body).Decode(&body) != nil || body.Ancestor != "test-library" || body.Page != 1 || body.Size != 1 ||
						!reflect.DeepEqual(body.Tags["type"], []string{"Movie", "TV", "Video"}) {
						t.Errorf("unexpected Flymoo count request")
					}
				}
				_, _ = w.Write([]byte(response.Load().(string)))
			}))
			defer server.Close()
			client, err := NewClient(provider, server.URL+"/proxy", NewDevice("Library count fixture"),
				Session{UserID: "viewer", Token: "fixture-token"})
			if err != nil {
				t.Fatal(err)
			}
			count := client.(LibraryCounter)
			if _, err := count.LibraryItemCount(t.Context(), "test-library"); !errors.Is(err, ErrResponse) {
				t.Fatalf("missing total returned %v", err)
			}
			switch provider {
			case Emby, Jellyfin:
				response.Store(`{"Items":[],"TotalRecordCount":0}`)
			case Plex:
				response.Store(`{"MediaContainer":{"size":0,"totalSize":0}}`)
			case Flymoo:
				response.Store(`{"code":0,"data":{"list":[],"total":"0"}}`)
			}
			if got, err := count.LibraryItemCount(t.Context(), "test-library"); err != nil || got != 0 {
				t.Fatalf("empty library count = %d, %v", got, err)
			}
			switch provider {
			case Emby, Jellyfin:
				response.Store(`{"Items":[{"Id":"title"}],"TotalRecordCount":1248}`)
			case Plex:
				response.Store(`{"MediaContainer":{"size":0,"totalSize":1248}}`)
			case Flymoo:
				response.Store(`{"code":0,"data":{"list":[{"guid":"title","mdb_guid":"test-library","type":"Movie"}],"total":1248}}`)
			}
			if got, err := count.LibraryItemCount(t.Context(), "test-library"); err != nil || got != 1248 {
				t.Fatalf("large library count = %d, %v", got, err)
			}
			if provider == Flymoo {
				response.Store(`{"code":0,"data":{"list":[{"guid":"title","mdb_guid":"different-library","type":"Movie"}],"total":1248}}`)
				if _, err := count.LibraryItemCount(t.Context(), "test-library"); !errors.Is(err, ErrResponse) {
					t.Fatalf("unscoped Flymoo count returned %v", err)
				}
			}
		})
	}
}

func TestDockerLibraryCounts(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires the local media server fixtures")
	}
	password := os.Getenv("WAYGU_TEST_PASSWORD")
	if password == "" {
		password = "waygu-fixture-local"
	}
	for _, server := range []struct {
		provider Provider
		address  string
	}{
		{Emby, "http://127.0.0.1:28096"},
		{Jellyfin, "http://127.0.0.1:18096"},
		{Plex, "http://127.0.0.1:32400"},
	} {
		t.Run(string(server.provider), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			device := NewDevice("Docker library count")
			guest, err := NewClient(server.provider, server.address, device, Session{})
			if err != nil {
				t.Fatal(err)
			}
			session, err := guest.SignIn(ctx, Credentials{Username: "waygu-test", Password: password})
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewClient(server.provider, server.address, device, session)
			if err != nil {
				t.Fatal(err)
			}
			libraries, err := client.Libraries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var verified int
			for _, library := range libraries {
				if library.Name != "Movies" && library.Name != "Shows" {
					continue
				}
				want, err := client.Catalog(ctx, Query{LibraryID: library.ID, Limit: 100})
				if err != nil || len(want.Items) == 0 || len(want.Items) != want.Total {
					t.Fatalf("fixture catalog %q: %d items of %d, %v", library.Name, len(want.Items), want.Total, err)
				}
				got, err := client.(LibraryCounter).LibraryItemCount(ctx, library.ID)
				if err != nil || got != len(want.Items) {
					t.Fatalf("%s count = %d, catalog = %d, %v", library.Name, got, len(want.Items), err)
				}
				verified++
			}
			if verified != 2 {
				t.Fatal("expected the Movies and Shows libraries")
			}
		})
	}
}
