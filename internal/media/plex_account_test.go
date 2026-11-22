package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlexAccountAuthorization(t *testing.T) {
	wrongServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "" || r.URL.Path != "/identity" {
			t.Error("sent a token or library request to an unverified server")
		}
		fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"wrong-server"}}`)
	}))
	defer wrongServer.Close()
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/identity":
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("sent a token during identity verification")
			}
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"fixture-server","version":"1.43.0"}}`)
		case "/library/sections":
			if r.Header.Get("X-Plex-Token") != "server-only-token" {
				t.Error("library request did not use the selected server token")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`)
		default:
			t.Error("unexpected PMS request")
			http.NotFound(w, r)
		}
	}))
	defer pms.Close()

	device := Device{ID: "test-client + &"}
	var polls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Plex-Token")
		if r.Header.Get("X-Plex-Client-Identifier") != device.ID || r.Header.Get("Accept") != "application/json" {
			t.Error("missing client identity or JSON preference")
		}
		switch r.URL.Path {
		case "/api/v2/pins":
			if r.Method != http.MethodPost || r.URL.Query().Get("strong") != "true" || token != "" {
				t.Error("incorrect PIN request")
			}
			fmt.Fprint(w, `{"id":27,"code":"a+B&c","expiresIn":120,"authToken":null}`)
		case "/api/v2/pins/27":
			if r.Method != http.MethodGet || token != "" {
				t.Error("incorrect PIN polling request")
			}
			if polls.Add(1) == 1 {
				fmt.Fprint(w, `{"id":27,"authToken":null}`)
				return
			}
			fmt.Fprint(w, `{"id":27,"authToken":"cloud-only-token"}`)
		case "/api/v2/resources":
			query := r.URL.Query()
			if token != "cloud-only-token" || query.Get("includeHttps") != "1" ||
				query.Get("includeRelay") != "1" || query.Get("includeIPv6") != "1" {
				t.Error("incorrect resources request")
			}
			fmt.Fprintf(w, `[
				{"clientIdentifier":"ignored","name":"Client","provides":"player"},
				{"clientIdentifier":"unshared","name":"Private","provides":"server","connections":[{"uri":%q,"local":true}]},
				{"clientIdentifier":"fixture-server","name":"Home Plex","provides":"client, server","accessToken":"server-only-token",
				 "connections":[{"uri":"http://example.com:32400","local":true},
				                {"uri":%q,"local":true},{"uri":%q,"local":true}]}
			]`, pms.URL, wrongServer.URL, pms.URL)
		default:
			t.Error("unexpected Plex cloud request")
			http.NotFound(w, r)
		}
	}))
	defer cloud.Close()
	account := plexTestAccount(t, cloud.URL, device)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	pin, err := account.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const signIn = "https://app.plex.tv/auth#?"
	values, err := url.ParseQuery(strings.TrimPrefix(pin.URL, signIn))
	if err != nil || !strings.HasPrefix(pin.URL, signIn) || values.Get("clientID") != device.ID ||
		values.Get("code") != "a+B&c" || values.Get("context[device][product]") != "Waygu Video" {
		t.Fatal("Plex authorization page or fragment parameters were incorrect")
	}
	servers, err := account.Servers(ctx, pin)
	if err != nil || len(servers) != 1 || polls.Load() != 2 {
		t.Fatalf("authorize and discover: %d servers, %d polls, %v", len(servers), polls.Load(), err)
	}
	connection, err := account.Connect(ctx, servers[0])
	if err != nil {
		t.Fatal(err)
	}
	if connection.Address != pms.URL || connection.Info != (ServerInfo{ID: "fixture-server", Name: "Home Plex", Version: "1.43.0"}) ||
		connection.Session.Token != "server-only-token" || len(connection.Libraries) != 1 || connection.Libraries[0].Name != "Movies" {
		t.Fatal("linked to an unexpected server, session or library")
	}
	if encoded, _ := json.Marshal(connection); strings.Contains(string(encoded), "server-only-token") {
		t.Fatal("serialized a Plex token")
	}
}

func TestPlexAuthorizationExpiresAndCancels(t *testing.T) {
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer expired.Close()
	account := plexTestAccount(t, expired.URL, Device{ID: "device"})
	pin := PlexPIN{id: 1, expires: time.Now().Add(time.Minute)}
	if _, err := account.Servers(t.Context(), pin); !errors.Is(err, ErrPlexPINExpired) {
		t.Fatalf("expired PIN error: %v", err)
	}

	started := make(chan struct{})
	pending := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer pending.Close()
	account = plexTestAccount(t, pending.URL, Device{ID: "device"})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-started:
			cancel()
		case <-ctx.Done():
		}
	}()
	if _, err := account.Servers(ctx, pin); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled authorization error: %v", err)
	}
}

func plexTestAccount(t *testing.T, endpoint string, device Device) *PlexAccount {
	t.Helper()
	base, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	return &PlexAccount{device: device, pins: newAPI(base), resources: newAPI(base)}
}
