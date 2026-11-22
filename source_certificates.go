package main

import (
	"crypto/tls"
	"errors"
	"net/http"
	"slices"
	"sync"

	"waygu-video/internal/media"
	"waygu-video/internal/mediahttp"
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

type artworkAccess struct {
	origin, key, credential string
	trust                   mediatls.Trust
}

func (s source) artworkAccess(address string) artworkAccess {
	var key string
	origin := sourceOrigin(address)
	if origin == sourceOrigin(s.Address) {
		key = s.HeaderSecret
	}
	access := artworkAccess{origin: origin, key: key, trust: s.certificateFor(address)}
	if s.Provider == media.Flymoo && origin != "" {
		access.credential = s.credentialKey()
	}
	return access
}

type sourceArtwork struct {
	mu       sync.Mutex
	active   map[artworkAccess]bool
	routes   map[artworkAccess]playback.ArtworkProxy
	fnRoutes map[sourceConnection]*fnRouteRefresh
}

func (a *application) artworkSources(entries []source) {
	active := make(map[artworkAccess]bool)
	for _, entry := range entries {
		for _, endpoint := range []string{entry.Address, entry.RemoteAddress} {
			access := entry.artworkAccess(endpoint)
			if access.trust.Certificate != "" || access.key != "" || access.credential != "" {
				active[access] = true
			}
		}
	}
	art := &a.sourceArtwork
	art.mu.Lock()
	defer art.mu.Unlock()
	fnRoutes := make(map[sourceConnection]*fnRouteRefresh)
	for _, entry := range entries {
		if a.routesEnabled.Load() && entry.canRefreshFN() {
			key := entry.connection()
			state := art.fnRoutes[key]
			if state == nil {
				state = new(fnRouteRefresh)
			}
			fnRoutes[key] = state
		}
	}
	art.active, art.fnRoutes = active, fnRoutes
	art.retireArtwork()
}

func (art *sourceArtwork) allows(access artworkAccess) bool {
	if art.active[access] {
		return true
	}
	for _, state := range art.fnRoutes {
		if state.artwork.origin != "" && state.artwork == access {
			return true
		}
	}
	return false
}

func (art *sourceArtwork) retireArtwork() {
	for access, route := range art.routes {
		if !art.allows(access) {
			route.Close()
			delete(art.routes, access)
		}
	}
}

func (a *application) proxyArtwork(access artworkAccess, headers http.Header, authorize mediahttp.Authorize) func(string) string {
	art := &a.sourceArtwork
	art.mu.Lock()
	defer art.mu.Unlock()
	if !art.allows(access) {
		return nil
	}
	if route, ok := art.routes[access]; ok {
		return route.Resolve
	}
	route, err := playback.OpenArtwork(mediahttp.Resource{URL: access.origin, Headers: headers, Trust: access.trust, Authorize: authorize})
	if err != nil {
		return nil
	}
	if art.routes == nil {
		art.routes = make(map[artworkAccess]playback.ArtworkProxy)
	}
	art.routes[access] = route
	return route.Resolve
}

func (a *application) sourceClient(s source, address string, session media.Session, includeArtwork bool) (media.Client, error) {
	client, err := a.newMedia(s.Provider, address, a.device, session)
	if err != nil {
		return nil, err
	}
	headers, err := a.requestHeaders(s, address)
	if err != nil {
		return nil, err
	}
	access := s.artworkAccess(address)
	var mapper func(string) string
	if includeArtwork {
		authorize := media.ArtworkAuthorization(client)
		if access.trust.Certificate != "" || len(headers) > 0 || authorize != nil {
			mapper = a.proxyArtwork(access, headers, authorize)
		}
	}
	if err := media.ConfigureEndpoint(client, access.trust, headers, mapper); err != nil {
		return nil, sourceAddressError(address, err)
	}
	return client, nil
}
