package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// LibraryScanClient asks a media server to start its own asynchronous library scan.
type LibraryScanClient interface {
	ScanLibraries(context.Context) error
}

func (m *mediaBrowser) ScanLibraries(ctx context.Context) error {
	return m.post(ctx, "Library/Refresh", nil, nil, nil)
}

func (p *plexClient) ScanLibraries(ctx context.Context) error {
	libraries, err := p.Libraries(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, library := range libraries {
		route := "/library/sections/" + url.PathEscape(library.ID) + "/refresh"
		err := p.request(ctx, http.MethodPost, route, nil, p.auth(), nil, nil)
		if errors.Is(err, StatusError(http.StatusNotFound)) || errors.Is(err, StatusError(http.StatusMethodNotAllowed)) {
			// Older Plex versions expose the scan operation as GET.
			err = p.request(ctx, http.MethodGet, route, nil, p.auth(), nil, nil)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("refresh Plex library %s: %w", library.ID, err))
		}
	}
	return errors.Join(failures...)
}
