package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"waygu-video/internal/media"
)

type resumeCacheUpdate struct {
	source source
	items  []catalogItem
}

func resumeCacheKey(source source) string {
	return "continue-watching-v1:" + mediaAccountKey(source)
}

func mediaAccountKey(source source) string {
	identity, _ := json.Marshal([]string{source.ID, string(source.Provider), source.ServerID, source.Address, source.AccountID})
	return fmt.Sprintf("%x", sha256.Sum256(identity))
}

func (a *application) readResumeCache(sources []source) map[string][]media.Item {
	cached := make(map[string][]media.Item)
	for _, source := range sources {
		if data, found := a.storage.GetItem(resumeCacheKey(source)); found {
			var rows []media.Item
			if json.Unmarshal([]byte(data), &rows) == nil {
				cached[source.ID] = rows[:min(len(rows), continueWatchingLimit)]
			}
		}
	}
	return cached
}

func (a *application) saveResumeCache(update resumeCacheUpdate) {
	items := make([]media.Item, 0, len(update.items))
	for _, entry := range update.items {
		item := entry.item
		// Some server artwork URLs include the account token.
		item.Poster, item.Backdrop = "", ""
		items = append(items, item)
	}
	key := resumeCacheKey(update.source)
	if len(items) == 0 {
		a.storage.RemoveItem(key)
		return
	}
	if data, err := json.Marshal(items); err == nil {
		_ = a.storage.SetItem(key, string(data))
	}
}

func visibleResumeItem(item media.Item, source source, visible []sourceLibrary) bool {
	if item.LibraryID == "" {
		return len(visible) == len(source.Libraries)
	}
	return slices.ContainsFunc(visible, func(library sourceLibrary) bool { return library.ID == item.LibraryID })
}

func cachedResumeShelf(source source, visible []sourceLibrary, cached []media.Item) []catalogItem {
	var shelf []catalogItem
	for _, item := range cached {
		if canResume(item) && visibleResumeItem(item, source, visible) {
			shelf = append(shelf, catalogItem{source: source, item: item})
		}
	}
	return shelf
}

func resumeShelf(ctx context.Context, client media.Client, source source, visible []sourceLibrary, cached []media.Item) ([]catalogItem, bool, error) {
	var shelf []catalogItem
	seen := make(map[string]bool)
	add := func(item media.Item) {
		if item.ID == "" || seen[item.ID] || !visibleResumeItem(item, source, visible) {
			return
		}
		seen[item.ID] = true
		if canResume(item) {
			shelf = append(shelf, catalogItem{source: source, item: item})
		}
	}
	refreshed := false
	var failure error
	if scoped, ok := client.(media.LibraryResumeClient); ok && len(visible) < len(source.Libraries) {
		for _, library := range visible {
			if ctx.Err() != nil {
				break
			}
			items, err := scoped.ResumeLibrary(ctx, library.ID)
			failure = errors.Join(failure, err)
			refreshed = refreshed || err == nil || len(items) > 0
			for _, item := range items {
				// The endpoint is scoped even when it omits each row's library ID.
				if item.LibraryID == "" {
					item.LibraryID = library.ID
				}
				if item.LibraryID == library.ID {
					add(item)
				}
			}
			if err != nil {
				for _, item := range cached {
					if item.LibraryID == library.ID {
						add(item)
					}
				}
			}
		}
	} else {
		items, err := client.Resume(ctx)
		failure = err
		refreshed = err == nil || len(items) > 0
		for _, item := range items {
			add(item)
		}
		if err != nil {
			for _, item := range cached {
				add(item)
			}
		}
	}
	sortRecent(shelf)
	return shelf[:min(len(shelf), continueWatchingLimit)], refreshed && ctx.Err() == nil, failure
}
