package media

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// CreditSearchClient finds titles credited to people matching a search term
// inside a library the caller has selected.
type CreditSearchClient interface {
	SearchCreditTitles(context.Context, Query) (Page, error)
}

func (m *mediaBrowser) SearchCreditTitles(ctx context.Context, q Query) (Page, error) {
	if q.LibraryID == "" || strings.TrimSpace(q.Search) == "" {
		return Page{}, nil
	}
	params := m.userQuery()
	params.Set("ParentId", q.LibraryID)
	params.Set("SearchTerm", q.Search)
	params.Set("Limit", "12")
	var people browserList
	if err := m.get(ctx, "Persons", params, &people); err != nil {
		return Page{}, err
	}
	ids := make([]string, 0, len(people.Items))
	for _, person := range people.Items {
		if person.ID != "" {
			ids = append(ids, person.ID)
		}
	}
	if len(ids) == 0 {
		return Page{}, nil
	}
	q.Search, q.CatalogFacts = "", true
	return m.catalog(ctx, q, ids, "Movie,Series,Video")
}

func (p *plexClient) SearchCreditTitles(ctx context.Context, q Query) (Page, error) {
	if q.LibraryID == "" || strings.TrimSpace(q.Search) == "" {
		return Page{}, nil
	}
	_, limit := q.bounds()
	container, err := p.get(ctx, "/hubs/search", url.Values{
		"query": {q.Search}, "sectionId": {q.LibraryID}, "limit": {strconv.Itoa(limit)}, "includeGuids": {"1"},
	})
	if err != nil {
		return Page{}, err
	}
	seen := make(map[string]bool)
	var page Page
	for _, hub := range container.Hub {
		for _, entries := range [][]plexMetadata{hub.Metadata, hub.Directory} {
			for _, entry := range entries {
				if entry.RatingKey == "" || entry.LibrarySectionID <= 0 || strconv.Itoa(entry.LibrarySectionID) != q.LibraryID ||
					(entry.Type != "movie" && entry.Type != "show") || (entry.Reason != "actor" && entry.Reason != "director") || seen[entry.RatingKey] {
					continue
				}
				seen[entry.RatingKey] = true
				item := p.item(entry)
				if entry.ReasonTitle != "" {
					var found bool
					for _, person := range item.People {
						found = found || strings.EqualFold(person.Name, entry.ReasonTitle)
					}
					if !found {
						item.People = append(item.People, Person{Name: entry.ReasonTitle, Kind: entry.Reason, Director: entry.Reason == "director"})
					}
				}
				page.Items = append(page.Items, item)
			}
		}
	}
	page.Total = len(page.Items)
	return page, nil
}
