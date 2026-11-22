package main

import (
	"crypto/tls"
	"errors"
	"slices"
	"sync"

	"waygu-video/internal/media"
	"waygu-video/internal/mediatls"
	"waygu-video/internal/playback"
)

type sourceCertificateError struct {
	address string
	err     error
}

func (failure *sourceCertificateError) Error() string { return failure.err.Error() }
func (failure *sourceCertificateError) Unwrap() error { return failure.err }

func sourceAddressError(address string, err error) error {
	var verification *tls.CertificateVerificationError
	if errors.As(err, &verification) || errors.Is(err, mediatls.ErrCertificate) {
		return &sourceCertificateError{address, err}
	}
	return err
}

func (s source) certificateFor(address string) mediatls.Trust {
	for _, certificate := range s.Certificates {
		if certificate.Matches(address) {
			return certificate
		}
	}
	return mediatls.Trust{}
}

func routeCertificates(approved []mediatls.Trust, endpoint ...string) []mediatls.Trust {
	return slices.DeleteFunc(slices.Clone(approved), func(pin mediatls.Trust) bool {
		return !slices.ContainsFunc(endpoint, pin.Matches)
	})
}

type artworkRoute struct {
	mapURL func(string) string
	close  func()
}

type sourceArtwork struct {
	mu     sync.Mutex
	active map[mediatls.Trust]bool
	routes map[mediatls.Trust]artworkRoute
}

func (a *application) artworkSources(entries []source) {
	active := make(map[mediatls.Trust]bool)
	for _, entry := range entries {
		for _, pin := range entry.Certificates {
			active[pin] = true
		}
	}
	art := &a.sourceArtwork
	art.mu.Lock()
	defer art.mu.Unlock()
	for pin, route := range art.routes {
		if !active[pin] {
			route.close()
			delete(art.routes, pin)
		}
	}
	art.active = active
}

func (a *application) trustedArtwork(pin mediatls.Trust) func(string) string {
	art := &a.sourceArtwork
	art.mu.Lock()
	defer art.mu.Unlock()
	if !art.active[pin] {
		return nil
	}
	if route, ok := art.routes[pin]; ok {
		return route.mapURL
	}
	mapURL, close, err := playback.OpenArtwork(pin.Origin, pin)
	if err != nil {
		return nil
	}
	if art.routes == nil {
		art.routes = make(map[mediatls.Trust]artworkRoute)
	}
	art.routes[pin] = artworkRoute{mapURL, close}
	return mapURL
}

func (a *application) sourceClient(s source, address string, session media.Session, includeArtwork bool) (media.Client, error) {
	client, err := a.newMedia(s.Provider, address, a.device, session)
	if err != nil {
		return nil, err
	}
	if pin := s.certificateFor(address); pin.Certificate != "" {
		var mapper func(string) string
		if includeArtwork {
			mapper = a.trustedArtwork(pin)
		}
		if err := media.ConfigureTrust(client, pin, mapper); err != nil {
			return nil, sourceAddressError(address, err)
		}
	}
	return client, nil
}
