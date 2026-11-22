package mediatls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestTrustRequiresTheOriginalHTTPSOriginAndHostname(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	_, err := http.Get(server.URL)
	pin, ok := Candidate(server.URL, err)
	if !ok || len(pin.Fingerprint()) != 64 || requests.Load() != 0 {
		t.Fatalf("missing credential-free certificate offer: %v", err)
	}
	for _, address := range []string{"http://127.0.0.1", server.URL + ".example", "https://127.0.0.1:1"} {
		transport, err := Transport(address, pin)
		if err != nil || transport != nil {
			t.Fatalf("pin applied to another origin: %s", address)
		}
	}
	var failure *tls.CertificateVerificationError
	if !errors.As(err, &failure) {
		t.Fatal("the standard TLS handshake did not expose a certificate failure")
	}
	if _, ok := Candidate("https://server.invalid", failure); ok {
		t.Fatal("a hostname mismatch was offered for approval")
	}
	if _, ok := Candidate(server.URL, &tls.CertificateVerificationError{
		UnverifiedCertificates: failure.UnverifiedCertificates,
		Err:                    x509.CertificateInvalidError{Reason: x509.Expired},
	}); ok {
		t.Fatal("an expired certificate was offered for approval")
	}
	transport, err := Transport(server.URL, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || requests.Load() != 1 {
		t.Fatal("the approved certificate did not authorize exactly one request")
	}
}
