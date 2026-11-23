package main

import (
	"crypto/rand"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	downloadCleanupKey = "offline-download-cleanup-v1"
	downloadCleanupAge = 24 * time.Hour
)

type downloadCompletion struct {
	At     int64  // Unix nanoseconds
	Policy string // identifies the opt-in that was active at completion
}

func (q *downloadQueue) setDeleteWatched(enabled bool) {
	if enabled == (q.cleanup.GetUntracked() != "") {
		return
	}
	policy := ""
	if enabled {
		policy = rand.Text()
	}
	if q.app.storage.SetItem(downloadCleanupKey, policy) != nil {
		q.failure.Set("save")
		return
	}
	q.cleanup.Set(policy)
}

func (q *downloadQueue) finishedPlayback(current route) {
	policy := q.cleanup.GetUntracked()
	if policy == "" {
		return
	}
	id := current.downloadID
	if id == "" {
		id = downloadID(mediaAccountKey(current.source), current.item.ID)
	}
	if entry, ok := q.find(id, q.entries.GetUntracked()); ok && entry.State == downloadComplete && !q.removing[id] {
		completion := &downloadCompletion{At: q.app.scope.WallTime().UnixNano(), Policy: policy}
		q.update(id, true, func(e *downloadEntry) { e.Completion = completion })
	}
}

func (q *downloadQueue) clearCompletion(id string) {
	if entry, ok := q.find(id, q.entries.GetUntracked()); ok && entry.Completion != nil {
		q.update(id, true, func(e *downloadEntry) { e.Completion = nil })
	}
}

func (q *downloadQueue) watchCompletedDownloads(cx *core.Scope) {
	rescan := core.NewSignal(cx, uint64(0))
	var retryAt time.Time
	cx.Effect(func() {
		policy := q.cleanup.Get()
		if policy == "" || cx.Env().Lifecycle() == core.LifecycleBackground {
			return
		}
		rescan.Get()
		_, next := q.completedDownloads(policy, q.entries.Get(), q.app.routes.Get(), cx.WallTime())
		if next.IsZero() {
			return
		}
		delay := max(next.Sub(cx.WallTime()), retryAt.Sub(cx.Now()), 0)
		cx.AfterFunc(delay, func() {
			now := cx.WallTime()
			due, next := q.completedDownloads(q.cleanup.GetUntracked(), q.entries.GetUntracked(), q.app.routes.GetUntracked(), now)
			if !next.IsZero() && !now.Before(next) {
				// Failed removals or queue writes can retry without spinning the engine.
				retryAt = cx.Now().Add(time.Minute)
			}
			q.remove(due)
			rescan.Update(func(count uint64) uint64 { return count + 1 })
		})
	})
}

func (q *downloadQueue) completedDownloads(policy string, entries []downloadEntry, stack []route, now time.Time) ([]string, time.Time) {
	if policy == "" {
		return nil, time.Time{}
	}
	var due []string
	var next time.Time
	for _, entry := range entries {
		if entry.State != downloadComplete || entry.Completion == nil || entry.Completion.Policy != policy || entry.Completion.At <= 0 {
			continue
		}
		if downloadInPlayer(stack, entry.ID) {
			continue
		}
		expires := time.Unix(0, entry.Completion.At).Add(downloadCleanupAge)
		if !now.Before(expires) && !q.removing[entry.ID] {
			due = append(due, entry.ID)
		}
		if next.IsZero() || expires.Before(next) {
			next = expires
		}
	}
	return due, next
}

func downloadInPlayer(stack []route, id string) bool {
	for _, current := range stack {
		if current.screen == screenPlayer && current.downloadID == id {
			return true
		}
	}
	return false
}
