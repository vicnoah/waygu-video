// Package mediatls allows one approved TLS leaf on its original HTTPS origin.
package mediatls

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Trust stores the public certificate in base64 DER and the origin it belongs to.
type Trust struct {
	Origin      string
	Certificate string
}

var ErrCertificate = errors.New("the approved server certificate no longer matches")

func (pin Trust) Matches(address string) bool {
	target, err := url.Parse(address)
	return err == nil && target.User == nil && target.Scheme == "https" &&
		pin.Certificate != "" && strings.EqualFold(pin.Origin, origin(target))
}

func origin(address *url.URL) string { return address.Scheme + "://" + address.Host }

// Candidate extracts the peer certificate from an unknown-authority failure.
// Certificate expiration, hostname checks and other failures cannot be approved.
func Candidate(address string, failure error) (Trust, bool) {
	endpoint, err := url.Parse(address)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil {
		return Trust{}, false
	}
	var verification *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	if !errors.As(failure, &verification) || !errors.As(verification, &unknown) || len(verification.UnverifiedCertificates) == 0 {
		return Trust{}, false
	}
	leaf := verification.UnverifiedCertificates[0]
	if leaf.VerifyHostname(endpoint.Hostname()) != nil || time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
		return Trust{}, false
	}
	pin := Trust{Origin: origin(endpoint), Certificate: base64.StdEncoding.EncodeToString(leaf.Raw)}
	if _, err := pin.config(endpoint); err != nil {
		return Trust{}, false
	}
	return pin, true
}

func (pin Trust) Fingerprint() string {
	der, err := base64.StdEncoding.DecodeString(pin.Certificate)
	if err != nil || len(der) == 0 {
		return ""
	}
	sum := sha256.Sum256(der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func (pin Trust) config(endpoint *url.URL) (*tls.Config, error) {
	der, err := base64.StdEncoding.DecodeString(pin.Certificate)
	if err != nil || len(der) > 32<<10 {
		return nil, ErrCertificate
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, ErrCertificate
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: endpoint.Hostname()}); err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 || !bytes.Equal(state.PeerCertificates[0].Raw, leaf.Raw) {
				return ErrCertificate
			}
			return nil
		},
	}, nil
}

// Configure sets the TLS verifier on a private transport for the exact origin.
// Callers keep their original redirect rules.
func Configure(transport *http.Transport, address string, pins ...Trust) error {
	for _, pin := range pins {
		if !pin.Matches(address) {
			continue
		}
		endpoint, _ := url.Parse(address)
		config, err := pin.config(endpoint)
		if err != nil {
			return err
		}
		transport.TLSClientConfig = config
	}
	return nil
}

// Transport returns nil for unpinned URLs, preserving the default transport.
func Transport(address string, pins ...Trust) (*http.Transport, error) {
	for _, pin := range pins {
		if pin.Matches(address) {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.IdleConnTimeout = 15 * time.Second
			if err := Configure(transport, address, pin); err != nil {
				return nil, err
			}
			return transport, nil
		}
	}
	return nil, nil
}
