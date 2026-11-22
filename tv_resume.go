package main

import (
	"encoding/json"
	"maps"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

const tvResumeDismissedKey = "tv-resume-dismissed-v1"

type tvResumeBookmark struct {
	Position   time.Duration
	LastPlayed int64
}

func tvResumeKey(entry catalogItem) string {
	return mediaAccountKey(entry.source) + ":" + entry.item.ID
}

func tvBookmark(item media.Item) tvResumeBookmark {
	return tvResumeBookmark{Position: item.Position, LastPlayed: item.LastPlayed.UnixNano()}
}

func (a *application) tvResumeDismissals(cx *core.Scope) (*core.Signal[map[string]tvResumeBookmark], func(catalogItem)) {
	var hidden map[string]tvResumeBookmark
	if data, ok := a.storage.GetItem(tvResumeDismissedKey); ok {
		_ = json.Unmarshal([]byte(data), &hidden)
	}
	list := core.NewSignalFunc(cx, hidden, maps.Equal)
	return list, func(entry catalogItem) {
		next := maps.Clone(list.GetUntracked())
		if next == nil {
			next = make(map[string]tvResumeBookmark)
		}
		next[tvResumeKey(entry)] = tvBookmark(entry.item)
		if encoded, err := json.Marshal(next); err == nil {
			_ = a.storage.SetItem(tvResumeDismissedKey, string(encoded))
		}
		list.Set(next)
	}
}
