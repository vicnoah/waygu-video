package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/nsgotest"

	"waygu-video/internal/downloads"
	"waygu-video/internal/media"
	"waygu-video/internal/mediahttp"
	"waygu-video/internal/mediatls"
	"waygu-video/internal/playback"
)

type certificateGateway struct {
	server   *httptest.Server
	leaf     atomic.Pointer[tls.Certificate]
	requests atomic.Int64
	artwork  atomic.Int64
	video    atomic.Int64
}

func selfSignedFixture(t *testing.T) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	leaf := &x509.Certificate{SerialNumber: serial, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func newCertificateGateway(t *testing.T, backend string) *certificateGateway {
	t.Helper()
	endpoint, err := url.Parse(backend)
	if err != nil {
		t.Fatal(err)
	}
	gate := new(certificateGateway)
	proxy := httputil.NewSingleHostReverseProxy(endpoint)
	direct := proxy.Director
	proxy.Director = func(req *http.Request) {
		direct(req)
		req.Host = endpoint.Host
		req.Header["X-Forwarded-For"] = nil
	}
	gate.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gate.requests.Add(1)
		if strings.Contains(r.URL.Path, "/Images/") {
			gate.artwork.Add(1)
		}
		if r.Method == http.MethodGet && (strings.Contains(r.URL.Path, "/stream") || strings.Contains(r.URL.Path, "/library/parts/")) {
			gate.video.Add(1)
		}
		proxy.ServeHTTP(w, r)
	}))
	leaf := selfSignedFixture(t)
	gate.leaf.Store(leaf)
	gate.server.TLS = &tls.Config{Certificates: []tls.Certificate{*leaf}, GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
		return &tls.Config{Certificates: []tls.Certificate{*gate.leaf.Load()}}, nil
	}}
	gate.server.StartTLS()
	t.Cleanup(gate.server.Close)
	return gate
}

func tlsFixtureBackend(t *testing.T, config map[string]string, id string) source {
	t.Helper()
	var entries []source
	if err := json.Unmarshal([]byte(config[sourcesKey]), &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("fixture source %q is missing", id)
	return source{}
}

func TestDockerApprovedServerCertificates(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires Docker fixture media")
	}
	config, secrets := homeReviewSources(t)
	for _, id := range []string{"emby", "jellyfin", "plex"} {
		t.Run(id, func(t *testing.T) {
			entry := tlsFixtureBackend(t, config, id)
			gate := newCertificateGateway(t, entry.Address)
			_, movie := reviewFixtureItem(t, config, secrets, id, "Waygu Track Test")
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			device := media.NewDevice("TLS fixture")
			address := gate.server.URL
			guest, err := media.NewClient(entry.Provider, address, device, media.Session{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = guest.Probe(ctx)
			pin, ok := mediatls.Candidate(address, err)
			if !ok || gate.requests.Load() != 0 {
				t.Fatalf("the unapproved certificate did not stop the HTTP request: %v", err)
			}
			client, err := media.NewClient(entry.Provider, address, device, media.Session{UserID: entry.AccountID, Token: secrets[id]})
			if err != nil {
				t.Fatal(err)
			}
			artwork, err := playback.OpenArtwork(mediahttp.Resource{URL: address, Trust: pin})
			if err != nil {
				t.Fatal(err)
			}
			defer artwork.Close()
			if err := media.ConfigureEndpoint(client, pin, nil, artwork.Resolve); err != nil {
				t.Fatal(err)
			}
			item, err := client.Detail(ctx, movie)
			if err != nil {
				t.Fatal(err)
			}
			if item.Poster != "" {
				checkTLSFixtureResponse(t, ctx, item.Poster, "", http.StatusOK)
			}
			stream, err := client.Playback(ctx, item, media.PlaybackOptions{})
			if err != nil || stream.Trust != pin {
				t.Fatalf("TLS playback metadata: %v", err)
			}
			video, closeVideo, err := playback.Open(stream.Original())
			if err != nil {
				t.Fatal(err)
			}
			defer closeVideo()
			data := checkTLSFixtureResponse(t, ctx, video, "bytes=0-31", http.StatusPartialContent)
			if !strings.Contains(string(data), "ftyp") {
				t.Fatal("the proxied range is missing the MP4 signature")
			}
			if id == "emby" {
				store := downloads.Store{Dir: t.TempDir()}
				file, err := store.Transfer(ctx, strings.Repeat("a", 64), downloads.Source{Resource: stream.Original(), Name: stream.File.Name, MediaID: stream.SourceID, Size: stream.File.Size}, downloads.Limits{}, nil)
				if err != nil || file.Size == 0 {
					t.Fatalf("download through pinned transport: %v", err)
				}
				var testedCaptions bool
				for _, track := range stream.Tracks {
					if track.Type == "subtitle" && track.URL != "" && track.External {
						if _, err := playback.FetchCaptions(ctx, track.URL, stream.SubtitleHeaders, stream.Trust); err != nil {
							t.Fatalf("subtitles through pinned transport: %v", err)
						}
						testedCaptions = true
						break
					}
				}
				if !testedCaptions {
					t.Fatal("missing external subtitle in the Emby fixture")
				}
			}
			gate.leaf.Store(selfSignedFixture(t))
			gate.server.CloseClientConnections()
			requests := gate.requests.Load()
			if _, err := client.Detail(ctx, movie); err == nil {
				t.Fatal("the authenticated client accepted a replacement certificate")
			}
			checkTLSFixtureResponse(t, ctx, video, "bytes=0-31", http.StatusBadGateway)
			if gate.requests.Load() != requests {
				t.Fatal("the replacement certificate received an HTTP request")
			}
		})
	}
}

func checkTLSFixtureResponse(t *testing.T, ctx context.Context, address, bytes string, expected int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		t.Fatal("invalid fixture resource URL")
	}
	if bytes != "" {
		req.Header.Set("Range", bytes)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal("read fixture resource through the app gateway")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode != expected || err != nil {
		t.Fatalf("fixture resource HTTP %d, wanted %d, read error %v", response.StatusCode, expected, err)
	}
	return data
}

func TestDockerPhoneTrustsCertificateForLoginAndPlayback(t *testing.T) {
	if os.Getenv("WAYGU_TEST_SERVERS") != "1" {
		t.Skip("requires Docker fixture media")
	}
	target := nsgotest.Target()
	if target != "" && target != "linux" {
		t.Skip("native review uses Linux GTK")
	}
	config, secrets := homeReviewSources(t)
	entry := tlsFixtureBackend(t, config, "emby")
	_, movie := reviewFixtureItem(t, config, secrets, "emby", "Waygu Track Test")
	gate := newCertificateGateway(t, entry.Address)
	for _, variant := range reviewAppearances {
		t.Run(variant.name, func(t *testing.T) {
			app := startReview(t, variant.appearance, 1030)
			videoBefore, artworkBefore := gate.video.Load(), gate.artwork.Load()
			app.Press("id:add-source")
			app.Press("id:choose-emby")
			host, port, _ := splitServerAddress(gate.server.URL)
			app.Type("id:server-address", host)
			app.Type("id:server-port", port)
			app.Press("id:server-https")
			before := gate.requests.Load()
			app.Press("id:test-connection")
			app.WaitFor("id:approve-source-certificate", 5*time.Second)
			if gate.requests.Load() != before {
				t.Fatal("the TLS handshake allowed an HTTP request before approval")
			}
			if target == "linux" {
				app.Screenshot("certificate-confirmation-" + variant.name)
			}
			app.Press("id:approve-source-certificate")
			app.WaitFor("id:connection-verified", 6*time.Second)
			if !app.Find("id:source-trust-certificate").Props.Checked {
				t.Fatal("certificate approval did not update the settings control")
			}
			if target == "linux" {
				app.Screenshot("certificate-approved-" + variant.name)
			}
			app.Type("id:server-username", "waygu-test")
			password := os.Getenv("WAYGU_TEST_PASSWORD")
			if password == "" {
				password = "waygu-fixture-local"
			}
			app.Type("id:server-password", password)
			app.Press("id:sign-in")
			app.WaitFor("id:import-libraries", 6*time.Second)
			app.Press("id:import-libraries")
			app.WaitFor("id:tab-search", 5*time.Second)
			var saved []source
			raw, _ := app.Storage().GetItem(sourcesKey)
			if err := json.Unmarshal([]byte(raw), &saved); err != nil || len(saved) != 1 ||
				len(saved[0].Certificates) != 1 || !saved[0].Certificates[0].Matches(gate.server.URL) {
				t.Fatal("the approved certificate was not stored for the source address")
			}
			app.Press("id:tab-search")
			app.Type("id:search-query", movie.Title)
			app.Press("id:search-submit")
			card := "id:title-" + movie.ID
			app.WaitFor(card, 8*time.Second)
			app.Press(card)
			app.WaitFor("id:detail-play", 6*time.Second)
			app.Press("id:detail-play")
			app.Resize(844, 390)
			app.WaitFor("id:video-player", 8*time.Second)
			if target == "linux" {
				app.WaitUntil(8*time.Second, func() bool {
					return gate.video.Load() > videoBefore && gate.artwork.Load() > artworkBefore && app.Find("id:player-position").Text > "00:00"
				})
			}
			app.Press("id:close-player")
		})
	}
}
