package media

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// FavoriteClient exposes a media server's account favorites.
type FavoriteClient interface {
	Favorites(context.Context, Query) (Page, error)
	SetFavorite(context.Context, Item, bool) error
}

type WatchedClient interface {
	SetWatched(context.Context, Item, bool) error
}

// HistoryClient lists the current account's viewed titles in a library.
type HistoryClient interface {
	History(context.Context, Library, Query) (Page, error)
}

func (m *mediaBrowser) Favorites(ctx context.Context, query Query) (Page, error) {
	return m.personalPage(ctx, query, true)
}

func (m *mediaBrowser) History(ctx context.Context, _ Library, query Query) (Page, error) {
	return m.personalPage(ctx, query, false)
}

func (m *mediaBrowser) personalPage(ctx context.Context, query Query, favorite bool) (Page, error) {
	offset, limit := query.bounds()
	params := m.userQuery()
	params.Set("ParentId", query.LibraryID)
	params.Set("Recursive", "true")
	params.Set("Fields", m.itemFields())
	params.Set("EnableUserData", "true")
	params.Set("StartIndex", strconv.Itoa(offset))
	params.Set("Limit", strconv.Itoa(limit))
	params.Set("SortOrder", "Descending")
	if favorite {
		params.Set("IsFavorite", "true")
		params.Set("IncludeItemTypes", "Movie,Series,Episode,Video")
		params.Set("SortBy", "DateCreated")
	} else {
		params.Set("IncludeItemTypes", "Movie,Episode,Video")
		params.Set("SortBy", "DatePlayed")
	}
	var dto browserList
	if err := m.get(ctx, "Items", params, &dto); err != nil {
		return Page{}, err
	}
	page := Page{Items: m.items(dto.Items), Total: dto.TotalRecordCount}
	if !favorite {
		return viewedPage(page, offset), nil
	}
	return page, nil
}

func (m *mediaBrowser) SetFavorite(ctx context.Context, item Item, favorite bool) error {
	return m.setUserItemFlag(ctx, item.ID, "FavoriteItems", favorite, nil)
}

func (m *mediaBrowser) SetWatched(ctx context.Context, item Item, watched bool) error {
	if !watched {
		return m.setUserItemFlag(ctx, item.ID, "PlayedItems", false, nil)
	}
	var data struct{ PlaybackPositionTicks int64 }
	if err := m.setUserItemFlag(ctx, item.ID, "PlayedItems", true, &data); err != nil {
		return err
	}
	if data.PlaybackPositionTicks > 0 {
		return m.clearWatchedPosition(ctx, item.ID)
	}
	return nil
}

func (m *mediaBrowser) setUserItemFlag(ctx context.Context, id, flag string, enabled bool, result any) error {
	item := url.PathEscape(id)
	method := http.MethodPost
	if !enabled {
		method = http.MethodDelete
	}
	legacy := m.userRoute() + "/" + flag + "/" + item
	if m.kind == Jellyfin {
		err := m.request(ctx, method, "User"+flag+"/"+item, m.userQuery(), m.auth(), nil, result)
		if !errors.Is(err, StatusError(http.StatusNotFound)) {
			return err
		}
	}
	return m.request(ctx, method, legacy, nil, m.auth(), nil, result)
}

func (p *plexClient) SetWatched(ctx context.Context, item Item, watched bool) error {
	route := "/:/unscrobble"
	if watched {
		route = "/:/scrobble"
	}
	params := url.Values{"key": {item.ID}, "identifier": {"com.plexapp.plugins.library"}}
	return p.request(ctx, http.MethodGet, route, params, p.auth(), nil, nil)
}

func (p *plexClient) History(ctx context.Context, library Library, query Query) (Page, error) {
	offset, limit := query.bounds()
	kind := "1"
	if strings.EqualFold(library.Kind, "show") {
		kind = "4"
	}
	params := url.Values{"type": {kind}, "sort": {"lastViewedAt:desc"},
		"includeGuids": {"1"}, "X-Plex-Container-Start": {strconv.Itoa(offset)}, "X-Plex-Container-Size": {strconv.Itoa(limit)}}
	container, err := p.get(ctx, "/library/sections/"+url.PathEscape(library.ID)+"/all", params)
	if err != nil {
		return Page{}, err
	}
	return viewedPage(Page{Items: p.items(container.Metadata), Total: max(container.TotalSize, container.Size)}, offset), nil
}

func viewedPage(page Page, offset int) Page {
	var items []Item
	for _, item := range page.Items {
		if !item.LastPlayed.IsZero() || item.Position > 0 || item.Watched {
			items = append(items, item)
		}
	}
	// Unplayed entries are at the end of the server's descending history.
	if len(items) != len(page.Items) {
		page.Total = offset + len(items)
	}
	page.Items = items
	return page
}

func (f *flymooClient) Favorites(ctx context.Context, query Query) (Page, error) {
	page, err := f.itemPage(ctx, query, "favorite/list", nil)
	for i := range page.Items {
		page.Items[i].Favorite = true
	}
	return page, err
}

func (f *flymooClient) History(ctx context.Context, _ Library, query Query) (Page, error) {
	page, err := f.itemPage(ctx, query, "item/list", map[string]string{"watched": "1"})
	for i := range page.Items {
		page.Items[i].Watched = true
	}
	return page, err
}

func (f *flymooClient) SetFavorite(ctx context.Context, item Item, favorite bool) error {
	return f.setItemFlag(ctx, item.ID, "item/favorite", http.MethodPut, favorite)
}

func (f *flymooClient) SetWatched(ctx context.Context, item Item, watched bool) error {
	return f.setItemFlag(ctx, item.ID, "item/watched", http.MethodPost, watched)
}

func (f *flymooClient) setItemFlag(ctx context.Context, id, route, method string, enabled bool) error {
	if !enabled {
		method = http.MethodDelete
	}
	_, err := f.call(ctx, method, route, nil, map[string]string{"item_guid": id}, f.session.Token)
	return err
}
