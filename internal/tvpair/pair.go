// Package tvpair sends a setup request from the phone to a one-time TV endpoint.
package tvpair

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const Lifetime = 5 * time.Minute
const Scheme = "waygu-pair"

var ErrAddress = errors.New("invalid TV pairing address")
var ErrRejected = errors.New("TV rejected the setup details")

type Setup struct {
	Provider string
	Address  string
	Username string
	Password string
	AppID    string
	Secret   string
}

type Target struct {
	host        string
	fingerprint [32]byte
	token       [16]byte
}

func (t Target) Link() string {
	format := base64.RawURLEncoding
	return Scheme + "://" + t.host + "/" + format.EncodeToString(t.fingerprint[:]) + "/" + format.EncodeToString(t.token[:])
}

func (t Target) Code() string {
	return fmt.Sprintf("%04d", binary.BigEndian.Uint32(t.token[:4])%10000)
}

func Parse(link string) (Target, error) {
	var out Target
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err != nil || parsed.Scheme != Scheme || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return out, ErrAddress
	}
	ip, err := netip.ParseAddr(parsed.Hostname())
	port, portErr := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil || portErr != nil || port == 0 || !usableIP(ip) {
		return out, ErrAddress
	}
	fingerprint, token, ok := strings.Cut(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/")
	if !ok {
		return out, ErrAddress
	}
	a, errA := base64.RawURLEncoding.DecodeString(fingerprint)
	b, errB := base64.RawURLEncoding.DecodeString(token)
	if errA != nil || errB != nil || len(a) != len(out.fingerprint) || len(b) != len(out.token) {
		return out, ErrAddress
	}
	copy(out.fingerprint[:], a)
	copy(out.token[:], b)
	out.host = net.JoinHostPort(ip.String(), strconv.FormatUint(port, 10))
	return out, nil
}

func usableIP(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback()
}

func LocalIP() (netip.Addr, error) {
	// A UDP route lookup selects the main interface without sending any traffic.
	if connection, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		local := connection.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap()
		_ = connection.Close()
		if local.IsPrivate() {
			return local, nil
		}
	}
	addresses, err := net.InterfaceAddrs()
	if err == nil {
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil && prefix.Addr().Is4() && prefix.Addr().IsPrivate() {
				return prefix.Addr(), nil
			}
		}
	}
	return netip.Addr{}, ErrAddress
}

// Listen creates an expiring HTTPS endpoint and accepts exactly one successful setup.
// The listener and all outstanding handlers share the lifetime of ctx.
func Listen(ctx context.Context, ip netip.Addr, apply func(context.Context, Setup) error) (Target, error) {
	if !usableIP(ip) || ctx.Err() != nil {
		return Target{}, ErrAddress
	}
	ctx, cancel := context.WithTimeout(ctx, Lifetime)
	key, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		cancel()
		return Target{}, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(Lifetime),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key, private)
	if err != nil {
		cancel()
		return Target{}, err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		cancel()
		return Target{}, err
	}
	target := Target{host: listener.Addr().String(), fingerprint: sha256.Sum256(der)}
	rand.Read(target.token[:])
	var busy, used atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if req.Method != http.MethodPost || req.URL.Path != "/setup" {
			http.NotFound(w, req)
			return
		}
		expected := "Bearer " + base64.RawURLEncoding.EncodeToString(target.token[:])
		if subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte(expected)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if ctx.Err() != nil || used.Load() || !busy.CompareAndSwap(false, true) {
			http.Error(w, "Setup unavailable", http.StatusConflict)
			return
		}
		defer busy.Store(false)
		data := json.NewDecoder(http.MaxBytesReader(w, req.Body, 8192))
		data.DisallowUnknownFields()
		var setup Setup
		if data.Decode(&setup) != nil || data.Decode(new(any)) != io.EOF {
			http.Error(w, "Invalid setup", http.StatusBadRequest)
			return
		}
		if err := apply(req.Context(), setup); err != nil {
			http.Error(w, "Could not apply setup", http.StatusUnprocessableEntity)
			return
		}
		used.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{
		Handler:           handler,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}},
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 8 * time.Second, WriteTimeout: 35 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		defer cancel()
		_ = server.ServeTLS(listener, "", "")
	}()
	context.AfterFunc(ctx, func() { _ = server.Close() })
	return target, nil
}

// Send pins the connection to the exact certificate scanned on the TV.
// A local network intermediary cannot read the setup or impersonate the TV.
func (t Target) Send(ctx context.Context, setup Setup) error {
	body, err := json.Marshal(setup)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+t.host+"/setup", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(t.token[:]))
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // This ephemeral server uses the QR fingerprint in place of a CA.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) != 1 || sha256.Sum256(state.PeerCertificates[0].Raw) != t.fingerprint {
				return ErrAddress
			}
			return nil
		},
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 32 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode == http.StatusUnprocessableEntity {
		return ErrRejected
	}
	if response.StatusCode != http.StatusNoContent {
		return ErrAddress
	}
	return nil
}
