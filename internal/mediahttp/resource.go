// Package mediahttp describes HTTP resources shared by media adapters and readers.
package mediahttp

import (
	"net/http"

	"waygu-video/internal/mediatls"
)

type Resource struct {
	URL       string
	Headers   http.Header
	Trust     mediatls.Trust
	Authorize Authorize
}

// Authorize prepares credentials that depend on the final request. The
// caller's redirect policy still decides which upstreams it may contact.
type Authorize func(*http.Request)

// Transport applies authorization to every request, including redirects,
// without changing the input request reused by http.Client.
func (authorize Authorize) Transport(base http.RoundTripper) http.RoundTripper {
	if authorize == nil {
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return authorizedTransport{base, authorize}
}

type authorizedTransport struct {
	base      http.RoundTripper
	authorize Authorize
}

func (t authorizedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	outgoing := request.Clone(request.Context())
	if outgoing.Header == nil {
		outgoing.Header = make(http.Header)
	}
	t.authorize(outgoing)
	return t.base.RoundTrip(outgoing)
}
