package media

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// LibraryCounter reads the number of movie, series and video titles in an
// individual library, as visible to the authenticated account.
type LibraryCounter interface {
	LibraryItemCount(ctx context.Context, libraryID string) (int, error)
}

func (m *mediaBrowser) LibraryItemCount(ctx context.Context, libraryID string) (int, error) {
	if libraryID == "" {
		return 0, ErrResponse
	}
	params := m.userQuery()
	params.Set("ParentId", libraryID)
	params.Set("IncludeItemTypes", "Movie,Series,Video")
	params.Set("Recursive", "true")
	params.Set("EnableImages", "false")
	params.Set("EnableUserData", "false")
	params.Set("EnableTotalRecordCount", "true")
	params.Set("Limit", "1")
	var page struct {
		TotalRecordCount *int
	}
	if err := m.get(ctx, "Items", params, &page); err != nil {
		return 0, err
	}
	if page.TotalRecordCount == nil || *page.TotalRecordCount < 0 {
		return 0, ErrResponse
	}
	return *page.TotalRecordCount, nil
}

func (p *plexClient) LibraryItemCount(ctx context.Context, libraryID string) (int, error) {
	if libraryID == "" {
		return 0, ErrResponse
	}
	params := url.Values{"X-Plex-Container-Start": {"0"}, "X-Plex-Container-Size": {"0"}}
	route := "/library/sections/" + url.PathEscape(libraryID) + "/all"
	var envelope struct {
		MediaContainer struct {
			TotalSize *int
		}
	}
	if err := p.request(ctx, http.MethodGet, route, params, p.auth(), nil, &envelope); err != nil {
		return 0, err
	}
	total := envelope.MediaContainer.TotalSize
	if total == nil || *total < 0 {
		return 0, ErrResponse
	}
	return *total, nil
}

func (f *flymooClient) LibraryItemCount(ctx context.Context, libraryID string) (int, error) {
	if libraryID == "" {
		return 0, ErrResponse
	}
	body := map[string]any{"ancestor_guid": libraryID, "page": 1, "page_size": 1,
		"tags": map[string][]string{"type": {"Movie", "TV", "Video"}}}
	data, err := f.call(ctx, http.MethodPost, "item/list", nil, body, f.session.Token)
	if err != nil {
		return 0, err
	}
	total, err := strconv.Atoi(flymooText(flymooObject(data), "total"))
	if err != nil || total < 0 {
		return 0, ErrResponse
	}
	rows := flymooRows(data, "list", "items", "data", "results", "medias", "movies", "tv", "rows")
	if total < len(rows) {
		return 0, ErrResponse
	}
	for _, row := range rows {
		if id, tagged := flymooLibraryID(row); tagged && id != libraryID {
			return 0, ErrResponse
		}
		switch strings.ToLower(flymooText(row, "type")) {
		case "directory", "livechannel":
			return 0, ErrResponse
		}
	}
	return total, nil
}
