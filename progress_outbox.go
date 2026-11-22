package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

const progressKeyPrefix = "pending-playback-v1:"

type pendingProgress struct {
	Account   string
	Revision  string
	SourceID  string `json:",omitempty"`
	LocalOnly bool   `json:",omitempty"`
	Watched   *bool  `json:",omitempty"`
	Item      media.Item
	Update    media.OfflineProgress
}

func (p pendingProgress) key() string {
	return progressKeyPrefix + p.Account + ":" + fmt.Sprintf("%x", sha256.Sum256([]byte(p.Update.ItemID)))
}

type progressOutbox struct {
	entries map[string]pendingProgress
	live    map[*progressWatch]struct{}
	changed *core.Signal[uint64]
	synced  *core.Signal[uint64]
	active  int
	replay  context.CancelFunc
	timer   func() bool
	// Live reports and offline recovery never write to a server concurrently.
	transport   sync.Mutex
	allowServer atomic.Bool
	sources     atomic.Pointer[map[string]progressDestination]
}

type progressDestination struct {
	account  string
	disabled bool
}

func (p *progressOutbox) setSources(available []source) {
	destinations := make(map[string]progressDestination, len(available))
	for _, entry := range available {
		destinations[entry.ID] = progressDestination{mediaAccountKey(entry), entry.ProgressDisabled}
	}
	if previous := p.sources.Load(); previous != nil && maps.Equal(*previous, destinations) {
		return
	}
	p.sources.Store(&destinations)
	if p.replay != nil {
		p.replay()
	}
}

func (p *progressOutbox) sendsFor(sourceID, account string) bool {
	if !p.allowServer.Load() {
		return false
	}
	if destinations := p.sources.Load(); destinations != nil {
		destination, present := (*destinations)[sourceID]
		return present && !destination.disabled && destination.account == account
	}
	return true
}

func newProgressOutbox(cx *core.Scope, storage core.Storage) *progressOutbox {
	outbox := &progressOutbox{entries: make(map[string]pendingProgress), live: make(map[*progressWatch]struct{}), changed: core.NewSignal(cx, uint64(0)), synced: core.NewSignal(cx, uint64(0))}
	outbox.allowServer.Store(true)
	for _, key := range storage.Keys() {
		if !strings.HasPrefix(key, progressKeyPrefix) {
			continue
		}
		data, _ := storage.GetItem(key)
		var entry pendingProgress
		if json.Unmarshal([]byte(data), &entry) == nil && entry.Update.ItemID != "" && entry.Revision != "" && entry.key() == key {
			outbox.entries[key] = entry
		}
	}
	return outbox
}

func (a *application) savePendingProgress(entry pendingProgress) {
	key := entry.key()
	a.progress.entries[key] = entry
	if data, err := json.Marshal(entry); err == nil {
		_ = a.storage.SetItem(key, string(data))
	}
	a.progress.changed.Update(func(value uint64) uint64 { return value + 1 })
}

func (a *application) dropPendingProgress(entry pendingProgress) bool {
	key := entry.key()
	if current := a.progress.entries[key]; entry.Revision == "" || current.Revision != entry.Revision {
		return false
	}
	delete(a.progress.entries, key)
	a.storage.RemoveItem(key)
	a.progress.changed.Update(func(value uint64) uint64 { return value + 1 })
	return true
}

func (a *application) localCheckpoint(source source, item media.Item) pendingProgress {
	key := (pendingProgress{Account: mediaAccountKey(source), Update: media.OfflineProgress{ItemID: item.ID}}).key()
	entry := a.progress.entries[key]
	if entry.LocalOnly && (item.LastPlayed.IsZero() || !item.LastPlayed.After(entry.Update.At)) {
		return entry
	}
	return pendingProgress{}
}

func applyProgressCheckpoint(item media.Item, entry pendingProgress) media.Item {
	item.Position, item.LastPlayed = entry.Update.Position, entry.Update.At
	if entry.Update.Duration > 0 {
		item.Duration = entry.Update.Duration
	}
	if entry.Watched != nil {
		item.Watched = *entry.Watched
	}
	if entry.Update.Completed {
		item.Position, item.Watched = 0, true
	}
	return item
}

func (a *application) localCatalogItems(entries []catalogItem) []catalogItem {
	a.progress.changed.Get()
	if len(a.progress.entries) == 0 {
		return entries
	}
	entries = slices.Clone(entries)
	for i := range entries {
		entry := &entries[i]
		if local := a.localCheckpoint(entry.source, entry.item); local.Revision != "" {
			entry.item = applyProgressCheckpoint(entry.item, local)
		}
		if entry.match != nil {
			match := *entry.match
			match.copies = slices.Clone(match.copies)
			for j := range match.copies {
				copy := &match.copies[j]
				if local := a.localCheckpoint(copy.source, copy.item); local.Revision != "" {
					copy.item = applyProgressCheckpoint(copy.item, local)
				}
			}
			entry.match = &match
		}
	}
	return entries
}

func (a *application) progressResumeShelf(remote []catalogItem, sources []source, q catalogRequest) []catalogItem {
	a.progress.changed.Get()
	if len(a.progress.entries) == 0 {
		return remote
	}
	shelf := slices.Clone(remote)
	for _, source := range sources {
		if q.filter != "" && q.filter != source.ID {
			continue
		}
		var visible []sourceLibrary
		for _, library := range source.Libraries {
			if q.includesLibrary(source.ID, library) {
				visible = append(visible, library)
			}
		}
		if len(visible) == 0 {
			continue
		}
		account := mediaAccountKey(source)
		for _, entry := range a.progress.entries {
			if entry.Account != account || !visibleResumeItem(entry.Item, source, visible) {
				continue
			}
			item := entry.Item
			index := slices.IndexFunc(shelf, func(row catalogItem) bool {
				return row.source.ID == source.ID && row.item.ID == entry.Update.ItemID
			})
			if index >= 0 {
				item = shelf[index].item
				if (!entry.LocalOnly && entry.Update.SupersededBy(item)) || (entry.LocalOnly && item.LastPlayed.After(entry.Update.At)) {
					continue
				}
			}
			row := catalogItem{source: source, item: applyProgressCheckpoint(item, entry), pending: true}
			if index >= 0 {
				shelf[index] = row
			} else {
				shelf = append(shelf, row)
			}
		}
	}
	return shelf
}

func (a *application) removeSourceProgress(source source) {
	account := mediaAccountKey(source)
	if a.progress.replay != nil {
		a.progress.replay()
	}
	for _, entry := range a.progress.entries {
		if entry.Account == account {
			a.dropPendingProgress(entry)
		}
	}
}

func (a *application) setProgressSending(enabled bool) {
	p := a.progress
	if p.allowServer.Swap(enabled) == enabled {
		return
	}
	if enabled {
		a.scheduleProgressReplay(time.Second)
		return
	}
	if p.timer != nil {
		p.timer()
		p.timer = nil
	}
	if p.replay != nil {
		p.replay()
	}
}

func (a *application) scheduleProgressReplay(delay time.Duration) {
	p := a.progress
	if !p.allowServer.Load() || p.active != 0 || p.replay != nil || p.timer != nil || len(p.entries) == 0 {
		return
	}
	a.scope.Run(func() {
		p.timer = a.scope.AfterFunc(delay, func() {
			p.timer = nil
			a.replayProgress()
		})
	})
}

func (a *application) replayProgress() {
	p := a.progress
	if !p.allowServer.Load() || p.active != 0 || p.replay != nil {
		return
	}
	sources := a.sources.GetUntracked()
	type replayRequest struct {
		entry  pendingProgress
		source source
	}
	var pending []replayRequest
	instances := make(map[string]source, len(sources))
	accounts := make(map[string]source, len(sources))
	restrictedAccounts := make(map[string]bool)
	for _, entry := range sources {
		instances[entry.ID] = entry
		account := mediaAccountKey(entry)
		if !p.sendsFor(entry.ID, account) {
			restrictedAccounts[account] = true
			continue
		}
		accounts[account] = entry
	}
	for _, entry := range p.entries {
		if entry.LocalOnly {
			continue
		}
		if entry.SourceID != "" {
			if origin, found := instances[entry.SourceID]; found && p.sendsFor(origin.ID, entry.Account) {
				pending = append(pending, replayRequest{entry, origin})
			}
		} else if origin, found := accounts[entry.Account]; found && !restrictedAccounts[entry.Account] {
			pending = append(pending, replayRequest{entry, origin})
		}
	}
	if len(pending) == 0 {
		return
	}
	slices.SortStableFunc(pending, func(a, b replayRequest) int { return a.entry.Update.At.Compare(b.entry.Update.At) })
	ctx, cancel := context.WithCancel(a.scope.Context())
	p.replay = cancel
	go func() {
		defer cancel()
		var synced []pendingProgress
		clients := make(map[string]media.OfflineProgressClient)
		for _, request := range pending {
			entry, origin := request.entry, request.source
			if ctx.Err() != nil {
				break
			}
			client, known := clients[origin.ID]
			if !known {
				current, _ := a.client(ctx, origin)
				client, _ = current.(media.OfflineProgressClient)
				clients[origin.ID] = client
			}
			if client == nil {
				continue
			}
			p.transport.Lock()
			if ctx.Err() != nil || !p.sendsFor(origin.ID, entry.Account) {
				p.transport.Unlock()
				continue
			}
			deadline, stop := context.WithTimeout(ctx, 10*time.Second)
			err := client.SyncOfflineProgress(deadline, entry.Update)
			stop()
			p.transport.Unlock()
			if err == nil || errors.Is(err, media.ErrProgressTargetGone) {
				synced = append(synced, entry)
			} else if !errors.Is(err, context.Canceled) {
				// Try the other accounts even when this server is unavailable.
				clients[origin.ID] = nil
			}
		}
		a.scope.Post(func() {
			p.replay = nil
			changed := false
			for _, entry := range synced {
				changed = a.dropPendingProgress(entry) || changed
			}
			if changed {
				p.synced.Update(func(value uint64) uint64 { return value + 1 })
			}
			a.scheduleProgressReplay(30 * time.Second)
		})
	}()
}

type progressWatch struct {
	a            *application
	account      string
	sourceID     string
	item         media.Item
	position     time.Duration
	watched      bool
	last         pendingProgress
	enabled      bool
	resumeReplay func()
	superseded   bool // guarded by progress.transport
}

func (a *application) pauseProgressReplay() (resume func()) {
	p := a.progress
	p.active++
	if p.replay != nil {
		p.replay()
	}
	if p.timer != nil {
		p.timer()
		p.timer = nil
	}
	return func() {
		p.active--
		a.scheduleProgressReplay(time.Second)
	}
}

func (a *application) beginProgressWatch(current route, initial playerMedia) *progressWatch {
	resume := a.pauseProgressReplay()
	_, supported := initial.client.(media.OfflineProgressClient)
	watch := &progressWatch{a: a, account: mediaAccountKey(current.source), sourceID: current.source.ID, item: current.item, position: initial.stream.Position,
		watched: current.item.Watched, enabled: supported, resumeReplay: resume}
	a.progress.live[watch] = struct{}{}
	return watch
}

func (watch *progressWatch) capture(state media.PlaybackState) pendingProgress {
	local := !watch.a.progress.sendsFor(watch.sourceID, watch.account)
	if (!watch.enabled && !local) || state.Event == media.Started || state.Stream.ItemID == "" {
		return pendingProgress{}
	}
	item := offlineItem(watch.item)
	item.ID = state.Stream.ItemID
	entry := pendingProgress{Account: watch.account, SourceID: watch.sourceID, Revision: rand.Text(), Item: item, LocalOnly: local, Update: media.OfflineProgress{
		ItemID: state.Stream.ItemID, MediaID: state.Stream.SourceID, Position: state.Position,
		Duration: state.Stream.Duration, At: time.Now().UTC(), Previous: watch.position, PreviousWatched: watch.watched,
		Completed: state.Stream.Duration > 0 && state.Position >= state.Stream.Duration,
	}}
	if local && !entry.Update.Completed {
		if previous := watch.a.progress.entries[entry.key()]; previous.LocalOnly && previous.Watched != nil {
			entry.Watched = previous.Watched
		}
	}
	watch.last = entry
	watch.a.savePendingProgress(entry)
	return entry
}

func (watch *progressWatch) confirmed(state media.PlaybackState, entry pendingProgress) {
	if !watch.enabled {
		return
	}
	watch.position = state.Position
	watch.watched = watch.watched || entry.Update.Completed
	if entry.Revision != "" && watch.a.dropPendingProgress(entry) {
		if state.Event == media.Stopped {
			watch.a.progress.synced.Update(func(value uint64) uint64 { return value + 1 })
		}
		return
	}
	// A report may finish after a newer checkpoint was saved. Advance the
	// expected remote position without acknowledging the newer checkpoint.
	if latest := watch.a.progress.entries[watch.last.key()]; latest.Revision != "" && latest.Revision == watch.last.Revision && !latest.LocalOnly {
		latest.Update.Previous, latest.Update.PreviousWatched = watch.position, watch.watched
		watch.a.savePendingProgress(latest)
	}
}

func (watch *progressWatch) close() {
	delete(watch.a.progress.live, watch)
	watch.resumeReplay()
}
