package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/http/httpguts"

	"waygu-video/internal/media"
)

const maxSourceHeaders = 16

var (
	errSourceHeadersUnavailable = errors.New("the secure request headers are unavailable")
	errSourceProbeDenied        = errors.New("the server denied the connection probe")
)

func sourceOrigin(raw string) string {
	endpoint, err := media.NormalizeAddress(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(endpoint.Scheme + "://" + endpoint.Host)
}

func validSourceHeader(name, value string) bool {
	if len(name) > 128 || len(value) == 0 || len(value) > 4096 ||
		!httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
		return false
	}
	switch name {
	case "Accept", "Accept-Encoding", "Connection", "Content-Encoding", "Content-Length", "Content-Type",
		"Expect", "Host", "If-Range", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Proxy-Connection", "Range", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
		"X-Emby-Authorization", "X-Emby-Token", "X-Mediabrowser-Token", "X-Plex-Token":
		return false
	}
	return true
}

func (a *application) requestHeaders(entry source, address string) (http.Header, error) {
	if sourceOrigin(address) != sourceOrigin(entry.Address) {
		return nil, nil
	}
	if entry.headerDraft != nil {
		return entry.headerDraft, nil
	}
	if entry.HeaderSecret == "" {
		return nil, nil
	}
	if cached, ok := a.sourceHeaders.Load(entry.HeaderSecret); ok {
		return cached.(http.Header), nil
	}
	data, found, err := a.secrets.Get(entry.HeaderSecret)
	if err != nil || !found {
		return nil, errSourceHeadersUnavailable
	}
	var header http.Header
	if json.Unmarshal([]byte(data), &header) != nil || len(header) == 0 || len(header) > maxSourceHeaders {
		return nil, errSourceHeadersUnavailable
	}
	for name, values := range header {
		if name != http.CanonicalHeaderKey(name) || len(values) != 1 || !validSourceHeader(name, values[0]) {
			return nil, errSourceHeadersUnavailable
		}
	}
	a.sourceHeaders.Store(entry.HeaderSecret, header)
	return header, nil
}

// stageSourceHeaders keeps the original credential live until the source record
// has committed its replacement. An empty draft explicitly clears the setting.
func (a *application) stageSourceHeaders(entry *source) (string, error) {
	if entry.headerDraft == nil {
		return "", nil
	}
	header := entry.headerDraft
	entry.HeaderSecret, entry.headerDraft = "", nil
	if len(header) == 0 {
		return "", nil
	}
	key := "source-headers-" + rand.Text()
	data, err := json.Marshal(header)
	if err != nil || a.secrets.Set(key, string(data)) != nil {
		return "", errSourceHeadersUnavailable
	}
	entry.HeaderSecret = key
	a.sourceHeaders.Store(key, header)
	return key, nil
}

func (a *application) forgetSourceHeaders(key string) {
	if key != "" {
		a.sourceHeaders.Delete(key)
		go func() { _ = a.secrets.Delete(key) }()
	}
}

// Save only when the edited source still has the transport and account shown
// when the form opened. The secure-store write and probe run off the UI thread.
func (a *application) saveExistingSourceHeaders(ctx context.Context, original, draft source, done func(error)) {
	go func() {
		var err error
		if len(draft.headerDraft) != 0 && draft.ServerID != "" {
			_, err = a.verifySourceAddress(ctx, draft, draft.Address, draft.ServerID, 5*time.Second)
		}
		var staged string
		if err == nil && ctx.Err() == nil {
			staged, err = a.stageSourceHeaders(&draft)
		}
		a.scope.Post(func() {
			if ctx.Err() != nil {
				a.forgetSourceHeaders(staged)
				return
			}
			if err == nil {
				err = a.updateSourceRouting(original, func(saved *source) {
					saved.HeaderSecret = draft.HeaderSecret
					saved.Certificates = draft.Certificates
				})
			}
			if err != nil {
				a.forgetSourceHeaders(staged)
			} else {
				a.forgetSourceHeaders(original.HeaderSecret)
			}
			done(err)
		})
	}()
}
