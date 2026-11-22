package main

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

	"waygu-video/internal/media"
)

type titleIdentity struct {
	kind       media.Kind
	imdb, tmdb string
}

func identityOf(item media.Item) titleIdentity {
	if item.Kind != media.Movie && item.Kind != media.Series {
		return titleIdentity{}
	}
	return identityWithIDs(item.Kind, item.ExternalIDs)
}

func identityWithIDs(kind media.Kind, externalIDs map[string]string) titleIdentity {
	var ids titleIdentity
	for provider, id := range externalIDs {
		id = strings.ToLower(strings.TrimSpace(id))
		switch strings.ToLower(provider) {
		case "imdb":
			id = strings.TrimPrefix(id, "imdb://")
			if digits, ok := strings.CutPrefix(id, "tt"); ok && validProviderNumber(digits) {
				ids.imdb = id
			}
		case "tmdb":
			id = strings.TrimPrefix(id, "tmdb://")
			if validProviderNumber(id) {
				ids.tmdb = strings.TrimLeft(id, "0")
			}
		}
	}
	if ids.imdb != "" || ids.tmdb != "" {
		ids.kind = kind
	}
	return ids
}

func validProviderNumber(id string) bool {
	return strings.TrimLeft(id, "0") != "" && strings.IndexFunc(id, func(c rune) bool { return c < '0' || c > '9' }) == -1
}

func (ids titleIdentity) matches(candidate titleIdentity) bool {
	if ids.kind == "" || ids.kind != candidate.kind ||
		(ids.imdb != "" && candidate.imdb != "" && ids.imdb != candidate.imdb) ||
		(ids.tmdb != "" && candidate.tmdb != "" && ids.tmdb != candidate.tmdb) {
		return false
	}
	return (ids.imdb != "" && ids.imdb == candidate.imdb) || (ids.tmdb != "" && ids.tmdb == candidate.tmdb)
}

type titleAliases struct {
	imdbToTMDB map[titleIdentity]string
	tmdbToIMDb map[titleIdentity]string
}

func newTitleAliases() titleAliases {
	return titleAliases{imdbToTMDB: make(map[titleIdentity]string), tmdbToIMDb: make(map[titleIdentity]string)}
}

func (aliases titleAliases) add(ids titleIdentity) {
	if ids.imdb != "" && ids.tmdb != "" {
		putAlias(aliases.imdbToTMDB, titleIdentity{kind: ids.kind, imdb: ids.imdb}, ids.tmdb)
		putAlias(aliases.tmdbToIMDb, titleIdentity{kind: ids.kind, tmdb: ids.tmdb}, ids.imdb)
	}
}

func aliasesFor(entries []catalogItem) titleAliases {
	aliases := newTitleAliases()
	for _, entry := range entries {
		aliases.add(identityOf(entry.item))
	}
	return aliases
}

func putAlias(aliases map[titleIdentity]string, id titleIdentity, corresponding string) {
	if previous, exists := aliases[id]; exists && previous != corresponding {
		// The empty value marks a conflicting association. It stays ambiguous.
		aliases[id] = ""
		return
	}
	aliases[id] = corresponding
}

func (aliases titleAliases) canonical(ids titleIdentity) titleIdentity {
	switch {
	case ids.imdb == "" && ids.tmdb != "":
		if imdb, exists := aliases.tmdbToIMDb[ids]; exists {
			if imdb == "" {
				return titleIdentity{}
			}
			ids.imdb = imdb
		}
	case ids.imdb != "" && ids.tmdb == "":
		if tmdb, exists := aliases.imdbToTMDB[ids]; exists {
			if tmdb == "" {
				return titleIdentity{}
			}
			ids.tmdb = tmdb
		}
	}
	return ids
}

type catalogMatch struct {
	ids      titleIdentity
	copies   []catalogCopy
	home     bool
	category libraryCategory
}

type catalogCopy struct {
	source source
	item   media.Item
}

type copyKey struct{ sourceID, itemID string }

func (c catalogCopy) key() copyKey { return copyKey{c.source.ID, c.item.ID} }

func (c catalogCopy) asItem() catalogItem { return catalogItem{source: c.source, item: c.item} }

func mergeCatalogItems(entries []catalogItem, home bool, category libraryCategory) []catalogItem {
	aliases := aliasesFor(entries)
	groups := make(map[titleIdentity]int)
	merged := make([]catalogItem, 0, len(entries))
	for _, entry := range entries {
		ids := aliases.canonical(identityOf(entry.item))
		if ids.kind == "" {
			// Details may provide identifiers that are missing from catalog cards.
			if entry.item.Kind == media.Movie || entry.item.Kind == media.Series {
				entry.match = &catalogMatch{home: home, category: category}
			}
			merged = append(merged, entry)
			continue
		}
		if index, found := groups[ids]; found {
			merged[index].match.copies = append(merged[index].match.copies, catalogCopy{entry.source, entry.item})
			continue
		}
		groups[ids] = len(merged)
		entry.match = &catalogMatch{ids: ids, home: home, category: category}
		merged = append(merged, entry)
	}
	return merged
}

func (a *application) relatedCopies(ctx context.Context, current route, sources []source) []catalogCopy {
	match := current.match
	anchor := catalogCopy{current.source, current.item}
	ids := match.ids
	if ids.kind == "" {
		if identityOf(anchor.item).kind != "" {
			// A catalog identity made ambiguous by conflicting metadata stays separate.
			return []catalogCopy{anchor}
		}
		if client, err := a.client(ctx, anchor.source); err == nil {
			if item, err := client.Detail(ctx, anchor.item); err == nil {
				ids = identityOf(item)
				anchor.item = item
			}
		}
		if ids.kind == "" {
			return []catalogCopy{anchor}
		}
	}
	known := append([]catalogCopy{anchor}, match.copies...)
	covered := make(map[string]bool)
	for _, copy := range known {
		covered[copy.source.ID] = true
	}
	var workers sync.WaitGroup
	discovered := make([][]catalogCopy, len(sources))
	for i, endpoint := range sources {
		if !covered[endpoint.ID] {
			workers.Go(func() { discovered[i] = a.findCopiesOnSource(ctx, match, anchor.item.Title, ids, endpoint) })
		}
	}
	workers.Wait()
	entries := make([]catalogItem, 0, len(known))
	for _, copy := range known {
		entries = append(entries, copy.asItem())
	}
	for _, copies := range discovered {
		for _, copy := range copies {
			entries = append(entries, copy.asItem())
		}
	}
	// Reevaluate partial identifiers with metadata from the other endpoints.
	merged := mergeCatalogItems(entries, match.home, match.category)
	return append([]catalogCopy{anchor}, merged[0].match.copies...)
}

func (a *application) findCopiesOnSource(ctx context.Context, match *catalogMatch, title string, ids titleIdentity, endpoint source) []catalogCopy {
	request := catalogRequest{home: match.home, category: match.category}
	var libraries []sourceLibrary
	for _, library := range endpoint.Libraries {
		if request.includesLibrary(endpoint.ID, library) && library.matchesCategory(match.category, media.Item{Kind: ids.kind}) {
			libraries = append(libraries, library)
		}
	}
	if len(libraries) == 0 {
		return nil
	}
	client, err := a.client(ctx, endpoint)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var copies []catalogCopy
	for _, library := range libraries {
		for _, item := range findLibraryCopies(ctx, client, library.ID, title, ids) {
			if !seen[item.ID] {
				seen[item.ID] = true
				copies = append(copies, catalogCopy{endpoint, item})
			}
		}
	}
	return copies
}

func findLibraryCopies(ctx context.Context, client media.Client, libraryID, title string, ids titleIdentity) []media.Item {
	const pageSize = 100
	var matches []media.Item
	if title != "" {
		page, err := client.Catalog(ctx, media.Query{LibraryID: libraryID, Search: title, Limit: pageSize})
		if err == nil {
			for _, item := range page.Items {
				if item.Kind == ids.kind && identityOf(item).kind == "" && strings.EqualFold(item.Title, title) {
					if detail, err := client.Detail(ctx, item); err == nil {
						item = detail
					}
				}
				if ids.matches(identityOf(item)) {
					matches = append(matches, item)
				}
			}
		}
	}
	if len(matches) > 0 {
		return matches
	}
	// Localized names can differ across providers. Search the imported library by identity.
	seen := make(map[string]bool)
	for offset := 0; ctx.Err() == nil; {
		page, err := client.Catalog(ctx, media.Query{LibraryID: libraryID, Limit: pageSize, Offset: offset})
		if err != nil || len(page.Items) == 0 {
			break
		}
		before := len(seen)
		for _, item := range page.Items {
			if item.ID != "" && !seen[item.ID] && ids.matches(identityOf(item)) {
				matches = append(matches, item)
			}
			seen[item.ID] = true
		}
		offset += len(page.Items)
		if len(matches) > 0 || len(seen) == before || (page.Total > 0 && offset >= page.Total) || (page.Total == 0 && len(page.Items) < pageSize) {
			break
		}
	}
	return matches
}

func prioritizeCopies(copies []catalogCopy, sources []source) []catalogCopy {
	rank := make(map[string]int, len(sources))
	for i, entry := range sources {
		rank[entry.ID] = i
	}
	copies = slices.Clone(copies)
	slices.SortStableFunc(copies, func(left, right catalogCopy) int {
		return rank[left.source.ID] - rank[right.source.ID]
	})
	return copies
}

func groupCopyKey(ids titleIdentity) string {
	return "last-title-source-v1:" + string(ids.kind) + ":" + ids.imdb + ":" + ids.tmdb
}

func encodeCopyKey(key copyKey) string {
	return strconv.Itoa(len(key.sourceID)) + ":" + key.sourceID + key.itemID
}

func (a *application) rememberedCopy(current route) string {
	if current.match == nil || current.match.ids.kind == "" || a.versionPolicy.GetUntracked() != media.VersionRemember {
		return ""
	}
	previous, _ := a.storage.GetItem(groupCopyKey(current.match.ids))
	return previous
}

func (a *application) rememberPlayedCopy(current route) {
	if current.match == nil || current.match.ids.kind == "" {
		return
	}
	key := groupCopyKey(current.match.ids)
	played := encodeCopyKey(copyKey{current.source.ID, current.item.ID})
	if previous, _ := a.storage.GetItem(key); previous != played {
		_ = a.storage.SetItem(key, played)
	}
}
