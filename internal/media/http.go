package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"waygu-video/internal/mediatls"
)

const responseLimit = 16 << 20

type api struct {
	base       *url.URL
	client     *http.Client
	observe    func(RequestMetric)
	trust      mediatls.Trust
	artworkURL func(string) string
}

func NormalizeAddress(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidAddress
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || !isHTTP(parsed) || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, ErrInvalidAddress
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = strings.TrimRight(parsed.RawPath, "/")
	return parsed, nil
}

func isHTTP(parsed *url.URL) bool {
	return parsed != nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}

func newAPI(base *url.URL) api {
	return api{base: base, client: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, previous []*http.Request) error {
			if len(previous) >= 5 || !sameOrigin(req.URL, previous[0].URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}}
}

// ConfigureTrust applies endpoint-local HTTPS trust to a newly constructed client.
func ConfigureTrust(client Client, trust mediatls.Trust, artwork func(string) string) error {
	if trust.Certificate == "" {
		return nil
	}
	var inner *api
	switch provider := client.(type) {
	case *mediaBrowser:
		inner = &provider.api
	case *plexClient:
		inner = &provider.api
	case *flymooClient:
		inner = &provider.api
	default:
		return mediatls.ErrCertificate
	}
	address := inner.base.String()
	if !trust.Matches(address) {
		return mediatls.ErrCertificate
	}
	transport, err := mediatls.Transport(address, trust)
	if err != nil {
		return err
	}
	inner.client.Transport = transport
	inner.trust, inner.artworkURL = trust, artwork
	return nil
}

func (a api) artworkAddress(address string) string {
	if a.artworkURL != nil && a.trust.Matches(address) {
		return a.artworkURL(address)
	}
	return address
}

func sameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host)
}

func (a api) resolve(route string, params url.Values) (*url.URL, error) {
	reference, err := url.Parse(route)
	if err != nil || reference.IsAbs() || reference.Host != "" || reference.User != nil || reference.Fragment != "" {
		return nil, ErrResponse
	}
	result := *a.base
	result.Path += "/" + strings.TrimLeft(reference.Path, "/")
	result.RawPath = a.base.EscapedPath() + "/" + strings.TrimLeft(reference.EscapedPath(), "/")
	query := reference.Query()
	for key, values := range params {
		query[key] = values
	}
	result.RawQuery = query.Encode()
	return &result, nil
}

// streamURL resolves a server-returned media link without forwarding the
// account's credentials to a different origin.
func (a api) streamURL(route string, params url.Values) (*url.URL, error) {
	link, err := url.Parse(route)
	if err != nil || link.User != nil || link.Fragment != "" {
		return nil, ErrResponse
	}
	if !link.IsAbs() && link.Host == "" &&
		(a.base.Path == "" || !strings.HasPrefix(link.Path, a.base.Path+"/")) {
		return a.resolve(route, params)
	}
	if !link.IsAbs() && link.Host == "" {
		link = a.base.ResolveReference(link)
	}
	if !sameOrigin(link, a.base) {
		return nil, ErrResponse
	}
	query := link.Query()
	for key, values := range params {
		query[key] = values
	}
	link.RawQuery = query.Encode()
	return link, nil
}

// StatusError identifies a non-successful API status without logging request
// URLs, which may include playback tokens.
type StatusError int

func (s StatusError) Error() string { return fmt.Sprintf("media server returned HTTP %d", s) }

func (s StatusError) Is(err error) bool {
	return err == ErrAuthentication && (s == http.StatusUnauthorized || s == http.StatusForbidden)
}

func (a api) request(ctx context.Context, method, route string, params url.Values, header http.Header, body, result any) error {
	endpoint, err := a.resolve(route, params)
	if err != nil {
		return err
	}
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	return a.send(ctx, method, endpoint, header, payload, result)
}

// RequestMetric contains operational data for a completed API request.
type RequestMetric struct {
	Method   string
	Status   int
	Duration time.Duration
	Failed   bool
}

func (a api) send(ctx context.Context, method string, endpoint *url.URL, header http.Header, payload []byte, result any) (failure error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return ErrInvalidAddress
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, values := range header {
		request.Header[key] = values
	}
	status := 0
	if a.observe != nil {
		start := time.Now()
		defer func() { a.observe(RequestMetric{method, status, time.Since(start), failure != nil}) }()
	}
	response, err := a.client.Do(request)
	if err != nil {
		var connection *url.Error
		if errors.As(err, &connection) {
			err = connection.Err
		}
		return fmt.Errorf("media server connection: %w", err)
	}
	defer response.Body.Close()
	status = response.StatusCode
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return StatusError(response.StatusCode)
	}
	if result == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		return err
	}
	if len(data) > responseLimit || json.Unmarshal(data, result) != nil {
		return ErrResponse
	}
	return nil
}
