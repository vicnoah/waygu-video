//go:build js && wasm && nsgoharmony

package platformnet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

var verifications sync.Map

func dialTLS(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	conn, err := dial(ctx, network, address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := tls.Client(conn, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
		// Wasm has no CA bundle. VerifyConnection checks the hostname in Go
		// and requires the HarmonyOS certificate service to trust the chain.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("TLS peer returned no certificates")
			}
			if err := state.PeerCertificates[0].VerifyHostname(host); err != nil {
				return err
			}
			chain := make([]string, 0, len(state.PeerCertificates))
			for _, cert := range state.PeerCertificates {
				chain = append(chain, base64.StdEncoding.EncodeToString(cert.Raw))
			}
			data, err := json.Marshal(chain)
			if err != nil {
				return err
			}
			id := int(nextID.Add(1))
			reply := make(chan int, 1)
			verifications.Store(id, reply)
			defer verifications.Delete(id)
			if !call("wayguTCPVerify", id, host, string(data)) {
				return errors.New("HarmonyOS TLS verification is unavailable")
			}
			select {
			case code := <-reply:
				switch code {
				case 0:
					return nil
				case 19030005, 19030009:
					return &tls.CertificateVerificationError{
						UnverifiedCertificates: state.PeerCertificates,
						Err:                    x509.UnknownAuthorityError{Cert: state.PeerCertificates[0]},
					}
				default:
					return fmt.Errorf("HarmonyOS rejected the TLS certificate chain (%d)", code)
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	if err := client.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}
