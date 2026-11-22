package media

import (
	"context"
	"net/url"
)

// MovieFeaturesClient lists the playable extras and recommendations a server
// exposes for a movie. Each method can succeed independently.
type MovieFeaturesClient interface {
	MovieExtras(context.Context, Item) ([]Item, error)
	RelatedMovies(context.Context, Item) ([]Item, error)
}

func (m *mediaBrowser) MovieExtras(ctx context.Context, movie Item) ([]Item, error) {
	route := "Items/" + url.PathEscape(movie.ID)
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
			if entry.ID != movie.ID && !seen[entry.ID] {
				seen[entry.ID] = true
				entry.Kind, entry.LibraryID = Video, movie.LibraryID
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

func (p *plexClient) MovieExtras(ctx context.Context, movie Item) ([]Item, error) {
	container, err := p.get(ctx, plexItemRoute(movie.ID)+"/extras", nil)
	if err != nil {
		return nil, err
	}
	extras := p.items(container.Metadata)
	for i := range extras {
		extras[i].Kind, extras[i].LibraryID = Video, movie.LibraryID
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
