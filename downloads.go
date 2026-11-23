package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/downloads"
	"waygu-video/internal/media"
)

type downloadState string

const (
	downloadQueued   downloadState = "queued"
	downloadRunning  downloadState = "running"
	downloadPaused   downloadState = "paused"
	downloadFailed   downloadState = "failed"
	downloadComplete downloadState = "complete"
)

const (
	downloadQueueKey   = "offline-downloads-v1"
	downloadSlotsKey   = "offline-download-slots-v1"
	downloadQuotaKey   = "offline-download-quota-v1"
	downloadQualityKey = "offline-download-quality-v1"
)

type downloadEntry struct {
	ID, Account, SourceName string
	Item                    media.Item
	Version                 media.PlaybackOptions
	WaitForWiFi             bool `json:",omitempty"`
	IncludeTracks           bool `json:",omitempty"`
	Height                  int  `json:",omitempty"`
	EncodedHeight           int  `json:",omitempty"`
	State                   downloadState
	Transferred, Total      int64
	Rate                    int64 `json:"-"`
	Position                time.Duration
	Failure                 string
	Completion              *downloadCompletion `json:",omitempty"`
}

type downloadQueue struct {
	app      *application
	lifetime context.Context
	store    downloads.Store
	entries  *core.Signal[[]downloadEntry]
	slots    *core.Signal[int]
	quota    *core.Signal[int64]
	height   *core.Signal[int]
	wifiOnly *core.Signal[bool]
	cleanup  *core.Signal[string]
	tracks   *core.Signal[bool]
	network  *core.Signal[downloadNetwork]
	failure  *core.Signal[string]
	running  map[string]context.CancelFunc
	removing map[string]bool
}

func newDownloadQueue(cx *core.Scope, a *application) *downloadQueue {
	dir := cx.DataDir()
	if dir != "" {
		dir = filepath.Join(dir, "downloads")
	}
	slots, _ := a.storage.GetItem(downloadSlotsKey)
	count, _ := strconv.Atoi(slots)
	if count < 1 || count > 3 {
		count = 2
	}
	rawQuota, _ := a.storage.GetItem(downloadQuotaKey)
	quota, _ := strconv.ParseInt(rawQuota, 10, 64)
	if !slices.Contains(downloadQuotaOptions, quota) {
		quota = 0
	}
	rawHeight, _ := a.storage.GetItem(downloadQualityKey)
	height, _ := strconv.Atoi(rawHeight)
	if downloadBitrate(height) == 0 {
		height = 0
	}
	wifiOnly, _ := a.storage.GetItem(downloadWiFiKey)
	cleanup, _ := a.storage.GetItem(downloadCleanupKey)
	tracks, _ := a.storage.GetItem(downloadTracksKey)
	queue := &downloadQueue{app: a, lifetime: cx.Context(), store: downloads.Store{Dir: dir}, running: make(map[string]context.CancelFunc),
		removing: make(map[string]bool), slots: core.NewSignal(cx, count), quota: core.NewSignal(cx, quota),
		height: core.NewSignal(cx, height), wifiOnly: core.NewSignal(cx, wifiOnly != "false"),
		cleanup: core.NewSignal(cx, cleanup),
		tracks:  core.NewSignal(cx, tracks != "false"),
		network: core.NewSignal(cx, downloadNetworkUnknown), failure: core.NewSignal(cx, "")}
	var entries []downloadEntry
	if dir != "" {
		data, _ := a.storage.GetItem(downloadQueueKey)
		_ = json.Unmarshal([]byte(data), &entries)
	}
	entries = slices.DeleteFunc(entries, func(entry downloadEntry) bool {
		return entry.Item.ID == "" || entry.Account == "" || entry.ID != downloadID(entry.Account, entry.Item.ID)
	})
	for i, entry := range entries {
		if _, file, err := queue.store.Open(entry.ID); err == nil {
			entry.State, entry.Failure = downloadComplete, ""
			entry.Transferred = queue.store.Bytes(entry.ID)
			entry.Total = entry.Transferred
			entry.EncodedHeight = file.Height
		} else {
			entry.Completion = nil
			entry.Transferred = queue.store.Bytes(entry.ID)
			switch entry.State {
			case downloadRunning:
				entry.State = downloadQueued
			case downloadComplete:
				entry.State, entry.Failure = downloadFailed, "file"
			}
		}
		entries[i] = entry
	}
	queue.entries = core.NewSignalFunc(cx, entries, nil)
	return queue
}

var downloadQuotaOptions = []int64{0, 5 << 30, 20 << 30, 50 << 30}

func downloadID(account, item string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(account+"\x00"+item)))
}

func (q *downloadQueue) find(id string, entries []downloadEntry) (downloadEntry, bool) {
	if i := slices.IndexFunc(entries, func(entry downloadEntry) bool { return entry.ID == id }); i >= 0 {
		return entries[i], true
	}
	return downloadEntry{}, false
}

func (q *downloadQueue) source(entry downloadEntry, sources []source) (source, bool) {
	for _, current := range sources {
		if mediaAccountKey(current) == entry.Account {
			return current, true
		}
	}
	return source{Name: entry.SourceName}, false
}

func (q *downloadQueue) persist(entries []downloadEntry) bool {
	data, err := json.Marshal(entries)
	if err == nil {
		err = q.app.storage.SetItem(downloadQueueKey, string(data))
	}
	if err != nil {
		q.failure.Set("save")
		return false
	}
	q.entries.Set(entries)
	return true
}

func (q *downloadQueue) update(id string, save bool, change func(*downloadEntry)) bool {
	entries := slices.Clone(q.entries.GetUntracked())
	i := slices.IndexFunc(entries, func(entry downloadEntry) bool { return entry.ID == id })
	if i < 0 {
		return false
	}
	change(&entries[i])
	if save {
		return q.persist(entries)
	}
	q.entries.Set(entries)
	return true
}

func (q *downloadQueue) enqueue(current route, height int, waitForWiFi bool) bool {
	account := mediaAccountKey(current.source)
	id := downloadID(account, current.item.ID)
	entries := q.entries.GetUntracked()
	if _, existing := q.find(id, entries); existing {
		return true
	}
	if q.store.Dir == "" || !playable(current.item) || current.item.ID == "" {
		return false
	}
	options, _ := q.app.initialPlaybackOptions(current)
	options.AudioIndex, options.SubtitleIndex = nil, nil
	entry := downloadEntry{ID: id, Account: account, SourceName: current.source.Name, Item: offlineItem(current.item),
		Version: options, Height: height, WaitForWiFi: waitForWiFi, IncludeTracks: q.tracks.GetUntracked(),
		State: downloadQueued, Position: current.item.Position}
	if !q.persist(append(slices.Clone(entries), entry)) {
		return false
	}
	q.pump()
	return true
}

func (q *downloadQueue) pump() {
	for _, entry := range q.entries.GetUntracked() {
		if len(q.running) >= q.slots.GetUntracked() {
			return
		}
		if entry.State != downloadQueued || q.running[entry.ID] != nil || q.removing[entry.ID] {
			continue
		}
		if q.waitFor(entry) != downloadNetworkReady {
			continue
		}
		current, ok := q.source(entry, q.app.sources.GetUntracked())
		if !ok {
			q.update(entry.ID, true, func(e *downloadEntry) { e.State, e.Failure = downloadFailed, "source" })
			continue
		}
		if !q.update(entry.ID, true, func(e *downloadEntry) { e.State, e.Failure = downloadRunning, "" }) {
			return
		}
		ctx, cancel := context.WithCancel(q.lifetime)
		q.running[entry.ID] = cancel
		quota := q.quota.GetUntracked()
		go q.transfer(ctx, entry, current, quota, q.trackDownload(entry, current))
	}
}

func (q *downloadQueue) transfer(ctx context.Context, entry downloadEntry, current source, quota int64,
	tracks func(context.Context, media.Stream) downloads.Tracks) {
	var file downloads.File
	var err error
	client, err := q.app.client(ctx, current)
	if err == nil {
		negotiation, cancel := context.WithTimeout(ctx, 25*time.Second)
		var stream media.Stream
		var input downloads.Source
		if entry.Height > 0 {
			stream, err = media.TranscodedDownload(negotiation, client, entry.Item, entry.Version, entry.Height, downloadBitrate(entry.Height))
			input = downloads.Source{Resource: stream.Resource(), Name: "video.mp4", MediaID: stream.SourceID,
				MaxHeight: entry.Height, Duration: stream.Duration, Playlist: current.Provider == media.Plex}
		} else {
			stream, err = client.Playback(negotiation, entry.Item, entry.Version)
			input = downloads.Source{Resource: stream.Original(), Name: stream.File.Name, Size: stream.File.Size, MediaID: stream.SourceID}
		}
		cancel()
		if err == nil {
			if tracks != nil {
				input.Tracks = func(ctx context.Context) downloads.Tracks { return tracks(ctx, stream) }
			}
			if input.URL == "" {
				err = downloads.ErrInvalidFile
			}
			var published time.Time
			var lastBytes int64
			progress := func(bytes, total int64) {
				now := time.Now()
				if (total < 0 || bytes < total) && now.Sub(published) < 250*time.Millisecond {
					return
				}
				var rate int64
				if elapsed := now.Sub(published).Seconds(); !published.IsZero() && elapsed > 0 && bytes > lastBytes {
					rate = int64(float64(bytes-lastBytes) / elapsed)
				}
				published, lastBytes = now, bytes
				q.app.scope.Post(func() {
					q.update(entry.ID, false, func(e *downloadEntry) {
						if e.State == downloadRunning {
							e.Transferred, e.Total, e.Rate = bytes, total, rate
						}
					})
				})
			}
			if err == nil {
				for attempt := 0; attempt < 3; attempt++ {
					file, err = q.store.Transfer(ctx, entry.ID, input, downloads.Limits{Quota: quota}, progress)
					if err == nil || ctx.Err() != nil || errors.Is(err, downloads.ErrNoSpace) || errors.Is(err, downloads.ErrBusy) {
						break
					}
					if attempt < 2 {
						select {
						case <-ctx.Done():
						case <-time.After(time.Duration(attempt+1) * time.Second):
						}
						if ctx.Err() != nil {
							break
						}
					}
				}
			}
			if input.Playlist {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				media.StopDownloadTranscode(cleanup, client, stream)
				cancel()
			}
		}
	}
	bytes := q.store.Bytes(entry.ID)
	q.app.scope.Post(func() {
		q.running[entry.ID]()
		delete(q.running, entry.ID)
		q.update(entry.ID, true, func(e *downloadEntry) {
			e.Transferred, e.Rate = bytes, 0
			if err == nil {
				e.State, e.Failure, e.Total = downloadComplete, "", bytes
				e.EncodedHeight = file.Height
				return
			}
			if e.State == downloadRunning {
				if q.waitFor(*e) != downloadNetworkReady {
					e.State, e.Failure = downloadQueued, ""
				} else {
					e.State, e.Failure = downloadFailed, downloadFailure(err)
				}
			}
		})
		q.pump()
	})
}

func downloadBitrate(height int) int {
	switch height {
	case 1080:
		return 10_000_000
	case 720:
		return 4_000_000
	default:
		return 0
	}
}

func (q *downloadQueue) setHeight(value int) {
	if q.app.storage.SetItem(downloadQualityKey, strconv.Itoa(value)) != nil {
		q.failure.Set("save")
		return
	}
	q.height.Set(value)
}

func downloadFailure(err error) string {
	switch {
	case errors.Is(err, downloads.ErrNoSpace):
		return "space"
	case errors.Is(err, downloads.ErrInvalidFile), errors.Is(err, os.ErrNotExist):
		return "file"
	case errors.Is(err, media.ErrNoStream), errors.Is(err, media.ErrAuthentication):
		return "source"
	default:
		return "transfer"
	}
}

func (q *downloadQueue) pause(id string) {
	entry, ok := q.find(id, q.entries.GetUntracked())
	if !ok || (entry.State != downloadQueued && entry.State != downloadRunning) {
		return
	}
	if q.update(id, true, func(entry *downloadEntry) { entry.State = downloadPaused }) {
		if cancel := q.running[id]; cancel != nil {
			cancel()
		}
	}
}

func (q *downloadQueue) resume(id string) {
	entry, ok := q.find(id, q.entries.GetUntracked())
	if !ok || (entry.State != downloadPaused && entry.State != downloadFailed) {
		return
	}
	if q.update(id, true, func(entry *downloadEntry) { entry.State, entry.Failure = downloadQueued, "" }) {
		q.pump()
	}
}

func (q *downloadQueue) remove(ids []string) {
	if len(ids) == 0 {
		return
	}
	var pending []string
	for _, id := range ids {
		if q.running[id] == nil && !q.removing[id] {
			q.removing[id] = true
			pending = append(pending, id)
		}
	}
	if len(pending) != len(ids) {
		q.failure.Set("busy")
	}
	if len(pending) == 0 {
		return
	}
	go func() {
		var removed []string
		for _, id := range pending {
			if err := q.store.Remove(id); err == nil {
				removed = append(removed, id)
			}
		}
		q.app.scope.Post(func() {
			for _, id := range pending {
				delete(q.removing, id)
			}
			if len(removed) != len(pending) {
				q.failure.Set("remove")
			}
			q.persist(slices.DeleteFunc(slices.Clone(q.entries.GetUntracked()), func(e downloadEntry) bool {
				return slices.Contains(removed, e.ID)
			}))
			q.pump()
		})
	}()
}

func (q *downloadQueue) setSlots(value int) {
	if q.app.storage.SetItem(downloadSlotsKey, strconv.Itoa(value)) != nil {
		q.failure.Set("save")
		return
	}
	q.slots.Set(value)
	q.pump()
}

func (q *downloadQueue) setQuota(value int64) {
	if q.app.storage.SetItem(downloadQuotaKey, strconv.FormatInt(value, 10)) != nil {
		q.failure.Set("save")
		return
	}
	q.quota.Set(value)
}
