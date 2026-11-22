package main

import (
	"net/url"
	"path/filepath"
	"slices"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

func (a *application) openDownload(entry downloadEntry) {
	selected, _ := a.downloads.source(entry, a.sources.GetUntracked())
	a.push(route{screen: screenPlayer, source: selected, item: entry.Item, resume: true, downloadID: entry.ID})
}

func (a *application) downloadedMedia(entry downloadEntry, current source, platform core.OS) (playerMedia, error) {
	path, file, err := a.downloads.store.Open(entry.ID)
	if err != nil {
		return playerMedia{}, err
	}
	var client media.Client
	if current.ID != "" {
		// Obtaining a configured client needs only local credentials. The
		// completed file opens even if those credentials were removed.
		client, _ = a.client(current)
	}
	address := path
	if platform == core.OSAndroid {
		address = (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	}
	stream := media.Stream{ItemID: entry.Item.ID, SourceID: file.MediaID, URL: address,
		Duration: entry.Item.Duration, Position: entry.Position, Method: media.DirectPlay}
	return playerMedia{client: client, stream: stream, url: address, close: func() {}}, nil
}

func (a *application) localPlaybackReports(current route, watch *progressWatch) (func(media.PlaybackState), func(*media.PlaybackState)) {
	watch.position = current.item.Position
	enqueue := func(state media.PlaybackState) {
		if state.Event == media.Started {
			return
		}
		_ = watch.capture(state)
		position := state.Position
		if state.Stream.Duration > 0 && position >= state.Stream.Duration {
			position = 0
		}
		a.downloads.update(current.downloadID, true, func(entry *downloadEntry) { entry.Position = position })
	}
	return enqueue, func(final *media.PlaybackState) {
		if final != nil {
			enqueue(*final)
		}
		watch.close()
	}
}

func (q *downloadQueue) available() bool { return q.store.Dir != "" }

// libraryDownloads uses the same account and library filters as the live catalog.
func (a *application) libraryDownloads(query catalogRequest) []catalogItem {
	var items []catalogItem
	sources := a.sources.Get()
	query.parental = a.parentalRules()
	for _, entry := range a.downloads.entries.Get() {
		if entry.State != downloadComplete {
			continue
		}
		selected, exists := a.downloads.source(entry, sources)
		if !exists || !query.parental.visible(selected.ID, entry.Item) || (query.filter != "" && selected.ID != query.filter) {
			continue
		}
		for _, library := range selected.Libraries {
			if library.ID == entry.Item.LibraryID && query.includesLibrary(selected.ID, library) &&
				library.matchesCategory(query.category, entry.Item) {
				item := entry.Item
				item.Position = entry.Position
				items = append(items, catalogItem{source: selected, item: item, downloadID: entry.ID})
				break
			}
		}
	}
	return items
}

func addLibraryDownloads(items, local []catalogItem) []catalogItem {
	if len(local) == 0 {
		return items
	}
	combined := slices.Clone(items)
	for _, entry := range local {
		if !slices.ContainsFunc(items, func(remote catalogItem) bool {
			if remote.source.ID == entry.source.ID && remote.item.ID == entry.item.ID {
				return true
			}
			return remote.match != nil && slices.ContainsFunc(remote.match.copies, func(copy catalogCopy) bool {
				return copy.source.ID == entry.source.ID && copy.item.ID == entry.item.ID
			})
		}) {
			combined = append(combined, entry)
		}
	}
	return combined
}
