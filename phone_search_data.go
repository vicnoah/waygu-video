package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

const phoneSearchHistoryKey = "phone-search-history-v1"

type phoneSearchState struct {
	draft, term, source *core.Signal[string]
	history             *core.Signal[[]string]
}

func newPhoneSearchState(cx *core.Scope) phoneSearchState {
	var stored []string
	if data, ok := cx.Storage().GetItem(phoneSearchHistoryKey); ok {
		_ = json.Unmarshal([]byte(data), &stored)
	}
	var history []string
	for _, term := range stored {
		if term = strings.TrimSpace(term); term != "" && !slices.ContainsFunc(history, func(previous string) bool {
			return strings.EqualFold(previous, term)
		}) {
			history = append(history, term)
			if len(history) == 12 {
				break
			}
		}
	}
	return phoneSearchState{
		draft: core.NewSignal(cx, ""), term: core.NewSignal(cx, ""), source: core.NewSignal(cx, ""),
		history: core.NewSignalFunc(cx, history, slices.Equal),
	}
}

func (a *application) savePhoneSearch(term string) {
	if term = strings.TrimSpace(term); term == "" {
		return
	}
	history := []string{term}
	for _, previous := range a.phoneSearch.history.GetUntracked() {
		if !strings.EqualFold(previous, term) {
			history = append(history, previous)
			if len(history) == 12 {
				break
			}
		}
	}
	data, _ := json.Marshal(history)
	_ = a.storage.SetItem(phoneSearchHistoryKey, string(data))
	a.phoneSearch.history.Set(history)
}

type phoneSearchPage struct {
	request *catalogRequest
	result  catalogResult
}

func (a *application) loadSearchCredits(ctx context.Context, request catalogRequest) catalogResult {
	groups := make([]catalogResult, len(request.sources))
	var workers sync.WaitGroup
	for i, source := range request.sources {
		if !request.parental.sourceAllowed(source.ID) || request.filter != "" && request.filter != source.ID {
			continue
		}
		workers.Go(func() {
			groups[i] = a.searchSourceCredits(ctx, source, request)
		})
	}
	workers.Wait()
	var result catalogResult
	for _, group := range groups {
		result.items = append(result.items, group.items...)
		result.connected = result.connected || group.connected
		result.hasMore = result.hasMore || group.hasMore
		result.failure = errors.Join(result.failure, group.failure)
	}
	return result
}

func (a *application) searchSourceCredits(ctx context.Context, source source, request catalogRequest) catalogResult {
	client, err := a.client(source)
	if err != nil {
		return catalogResult{failure: err}
	}
	credits, ok := client.(media.CreditSearchClient)
	if !ok {
		return catalogResult{}
	}
	var result catalogResult
	for _, library := range source.Libraries {
		if ctx.Err() != nil || !request.includesLibrary(source.ID, library) {
			continue
		}
		page, err := scanCatalogPages(ctx, credits.SearchCreditTitles, media.Query{LibraryID: library.ID, Search: request.search})
		if err != nil {
			result.failure = errors.Join(result.failure, err)
			if len(page.Items) == 0 {
				continue
			}
		}
		result.connected = true
		result.hasMore = result.hasMore || len(page.Items) < page.Total
		for _, item := range page.Items {
			if item.ID != "" && (item.LibraryID == "" || item.LibraryID == library.ID) {
				item.LibraryID = library.ID
				if !request.parental.visible(source.ID, item) {
					continue
				}
				result.items = append(result.items, catalogItem{source: source, item: item})
			}
		}
	}
	return result
}

func combinePhoneSearch(request *catalogRequest, remote, credited, local []catalogItem) []catalogItem {
	var entries []catalogItem
	indices := make(map[copyKey]int)
	for _, set := range [][]catalogItem{remote, credited, local} {
		for _, entry := range set {
			key := (catalogCopy{entry.source, entry.item}).key()
			if index, seen := indices[key]; seen {
				known := &entries[index]
				if len(entry.item.People) != 0 {
					people := slices.Clone(known.item.People)
					for _, next := range entry.item.People {
						match := slices.IndexFunc(people, func(person media.Person) bool {
							return sameSearchPerson(person, next)
						})
						if match < 0 {
							people = append(people, next)
						} else if people[match].Portrait == "" && next.Portrait != "" {
							people[match].Portrait = next.Portrait
						}
					}
					known.item.People = people
				}
				continue
			}
			indices[key] = len(entries)
			entries = append(entries, entry)
		}
	}
	if request.merge && request.filter == "" && len(request.sources) > 1 {
		ordered := make(map[string]int, len(request.sources))
		for i, source := range request.sources {
			ordered[source.ID] = i
		}
		slices.SortStableFunc(entries, func(a, b catalogItem) int { return cmp.Compare(ordered[a.source.ID], ordered[b.source.ID]) })
		return mergeCatalogItems(entries, false, "")
	}
	return entries
}

func sameSearchPerson(a, b media.Person) bool {
	return strings.EqualFold(a.Name, b.Name) && searchPersonKind(a) == searchPersonKind(b)
}

func searchPersonKind(person media.Person) string {
	if person.Director {
		return "director"
	}
	return cmp.Or(person.Kind, "actor")
}

type searchPerson struct {
	person media.Person
	titles int
}

func peopleInSearch(items []catalogItem, term string) []searchPerson {
	type personKey struct{ name, kind string }
	index := make(map[personKey]int)
	var people []searchPerson
	for _, entry := range items {
		seen := make(map[personKey]bool)
		add := func(item media.Item) {
			for _, person := range item.People {
				name := strings.TrimSpace(person.Name)
				if name == "" {
					continue
				}
				key := personKey{strings.ToLower(name), searchPersonKind(person)}
				if i, found := index[key]; found {
					if people[i].person.Portrait == "" {
						people[i].person.Portrait = person.Portrait
					}
					if !seen[key] {
						people[i].titles++
					}
				} else {
					index[key] = len(people)
					people = append(people, searchPerson{person, 1})
				}
				seen[key] = true
			}
		}
		add(entry.item)
		if entry.match != nil {
			for _, copy := range entry.match.copies {
				add(copy.item)
			}
		}
	}
	needle := strings.ToLower(term)
	slices.SortStableFunc(people, func(a, b searchPerson) int {
		relevance := func(person searchPerson) int {
			name := strings.ToLower(person.person.Name)
			if name == needle {
				return 2
			}
			if strings.Contains(name, needle) {
				return 1
			}
			return 0
		}
		return cmp.Or(cmp.Compare(relevance(b), relevance(a)), cmp.Compare(b.titles, a.titles))
	})
	return people
}
