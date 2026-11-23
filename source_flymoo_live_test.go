package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"waygu-video/internal/media"
)

func TestFlymooLiveConnection(t *testing.T) {
	address := os.Getenv("WAYGU_FLYMOO_LIVE_URL")
	credentials := media.Credentials{Username: os.Getenv("WAYGU_FLYMOO_LIVE_USER"), Password: os.Getenv("WAYGU_FLYMOO_LIVE_PASSWORD")}
	if address == "" || credentials.Username == "" || credentials.Password == "" {
		t.Skip("requires local credentials for a live Flymoo server")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	a := application{device: media.NewDevice("Waygu Video read-only integration test"), newMedia: media.NewClient}
	form := source{Provider: media.Flymoo, Address: address}
	gateway, err := a.probeFlymooGateway(ctx, form, address)
	assertFlymooLive(t, "server identity", err)
	session, err := gateway.client.SignIn(ctx, credentials)
	assertFlymooLive(t, "sign in", err)
	client, err := a.sourceClient(form, gateway.address, session, false)
	assertFlymooLive(t, "authenticated client", err)
	libraries, err := client.Libraries(ctx)
	assertFlymooLive(t, "libraries", err)
	if len(libraries) == 0 {
		t.Fatal("the test account has no visible libraries")
	}
	t.Logf("Authenticated LAN account with %d visible libraries", len(libraries))

	var episode media.Item
	for _, library := range libraries {
		page, err := client.Catalog(ctx, media.Query{LibraryID: library.ID, Limit: 48})
		assertFlymooLive(t, "library catalog", err)
		for _, item := range page.Items {
			if item.Kind != media.Series {
				continue
			}
			seasons, err := client.Children(ctx, item)
			assertFlymooLive(t, "seasons", err)
			for _, season := range seasons {
				episodes, err := client.Children(ctx, season)
				assertFlymooLive(t, "episodes", err)
				for _, candidate := range episodes {
					if candidate.Kind == media.Episode && candidate.ID != "" {
						episode = candidate
						break
					}
				}
				if episode.ID != "" {
					break
				}
			}
			if episode.ID != "" {
				break
			}
		}
		if episode.ID != "" {
			break
		}
	}
	if episode.ID == "" {
		t.Fatal("the sampled libraries contained no playable episode")
	}
	stream, err := client.Playback(ctx, episode, media.PlaybackOptions{})
	assertFlymooLive(t, "episode playback", err)
	original := stream.Original()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, original.URL, nil)
	assertFlymooLive(t, "original media request", err)
	origin, err := url.Parse(gateway.address)
	assertFlymooLive(t, "LAN endpoint", err)
	if request.URL.Host != origin.Host || request.URL.Scheme != origin.Scheme {
		t.Fatal("the original did not resolve to the LAN media server")
	}
	request.Header = original.Headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Range", "bytes=0-1023")
	httpClient := http.Client{Transport: original.Authorize.Transport(nil), CheckRedirect: func(next *http.Request, _ []*http.Request) error {
		if next.URL.Host != origin.Host || next.URL.Scheme != origin.Scheme {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	response, err := httpClient.Do(request)
	assertFlymooLive(t, "signed episode range", err)
	defer response.Body.Close()
	if response.StatusCode != http.StatusPartialContent {
		t.Fatalf("signed episode range returned HTTP %d", response.StatusCode)
	}
	sample, err := io.ReadAll(io.LimitReader(response.Body, 1025))
	assertFlymooLive(t, "episode bytes", err)
	if len(sample) != 1024 {
		t.Fatalf("expected 1024 bytes from the signed episode range, got %d", len(sample))
	}
	t.Log("Read seasons, episodes and a signed original-file byte range from the LAN server")
}

func TestFlymooLiveFNConnect(t *testing.T) {
	address, id := os.Getenv("WAYGU_FLYMOO_LIVE_URL"), os.Getenv("WAYGU_FLYMOO_LIVE_FNID")
	if address == "" || id == "" {
		t.Skip("requires a Flymoo server and its FN Connect identifier")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	a := application{device: media.NewDevice("Waygu Video read-only integration test"), newMedia: media.NewClient}
	gateway, err := a.probeFlymooGateway(ctx, source{Provider: media.Flymoo, Address: address}, address)
	assertFlymooLive(t, "server identity", err)
	resolved, err := (media.FNConnectClient{}).Resolve(ctx, id, "")
	assertFlymooLive(t, "FN Connect resolution", err)
	if gateway.info.ID == "" || resolved.ServerID != gateway.info.ID {
		t.Fatal("FN Connect did not resolve to the same media server identity")
	}
	t.Log("FN Connect resolved the same verified media server identity")
}

func assertFlymooLive(t *testing.T, stage string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	// Client errors may contain an address or a signed request URL.
	var status media.StatusError
	if errors.As(err, &status) {
		t.Fatalf("%s returned HTTP %d", stage, status)
	}
	if suffix, ok := strings.CutPrefix(err.Error(), "Flymoo returned code "); ok {
		if code, err := strconv.Atoi(suffix); err == nil {
			t.Fatalf("%s returned Flymoo code %d", stage, code)
		}
	}
	switch {
	case errors.Is(err, media.ErrAuthentication):
		t.Fatalf("%s was rejected by the server", stage)
	case errors.Is(err, context.DeadlineExceeded):
		t.Fatalf("%s timed out", stage)
	case errors.Is(err, media.ErrFNConnectRoute):
		t.Fatal("FN Connect found no available Flymoo route")
	case errors.Is(err, media.ErrFNConnectLookup):
		t.Fatal("FN Connect could not complete the official portal lookup")
	default:
		t.Fatalf("%s failed (%T)", stage, err)
	}
}
