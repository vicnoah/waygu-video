package media

import (
	"context"
	"net/url"
)

// ExtrasClient lists trailers and special features attached to an item.
type ExtrasClient interface {
	Extras(context.Context, Item) ([]Item, error)
}

// MovieFeaturesClient exposes extras and same-provider recommendations.
type MovieFeaturesClient interface {
	ExtrasClient
	RelatedMovies(context.Context, Item) ([]Item, error)
}

func (m *mediaBrowser) Extras(ctx context.Context, item Item) ([]Item, error) {
	route := "Items/" + url.PathEscape(item.ID)
	if m.kind == Emby {
		route = m.userRoute() + "/" + route
	}
	var extras []Item
	var failure error
	seen := make(map[string]bool)
	for _, endpoint := range []string{"/LocalTrailers", "/SpecialFeatures"} {
		var entries []browserItem
		if err := m.get(ctx, route+endpoint, m.userQuery(), &entries); err != nil {
			failure = err
			continue
		}
		for _, entry := range m.items(entries) {
			if entry.ID != item.ID && !seen[entry.ID] {
				seen[entry.ID] = true
				entry.Kind, entry.LibraryID = Video, item.LibraryID
				extras = append(extras, entry)
			}
		}
	}
	return extras, failure
}

func (m *mediaBrowser) RelatedMovies(ctx context.Context, movie Item) ([]Item, error) {
	params := m.userQuery()
	params.Set("Limit", "12")
	params.Set("Fields", m.itemFields())
	var dto browserList
	if err := m.get(ctx, "Items/"+url.PathEscape(movie.ID)+"/Similar", params, &dto); err != nil {
		return nil, err
	}
	return m.items(dto.Items), nil
}

func (p *plexClient) Extras(ctx context.Context, item Item) ([]Item, error) {
	container, err := p.get(ctx, plexItemRoute(item.ID)+"/extras", nil)
	if err != nil {
		return nil, err
	}
	extras := p.items(container.Metadata)
	for i := range extras {
		extras[i].Kind, extras[i].LibraryID = Video, item.LibraryID
	}
	return extras, nil
}

func (p *plexClient) RelatedMovies(ctx context.Context, movie Item) ([]Item, error) {
	container, err := p.get(ctx, plexItemRoute(movie.ID)+"/related", url.Values{"count": {"12"}, "includeGuids": {"1"}})
	if err != nil {
		return nil, err
	}
	items := p.items(container.Metadata)
	for _, hub := range container.Hub {
		items = append(items, p.items(hub.Metadata)...)
	}
	return items, nil
}
