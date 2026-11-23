package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"time"

	"waygu-video/internal/media"
)

const downloadSourceOfflineAge = 30 * 24 * time.Hour

type downloadExpiration string

const (
	downloadMissingFromSource downloadExpiration = "missing"
	downloadSourceOffline     downloadExpiration = "offline"
)

type downloadAvailability struct {
	OfflineSince int64              `json:",omitempty"` // Unix seconds
	Expired      downloadExpiration `json:",omitempty"`
	Keep         bool               `json:",omitempty"`
}

type downloadSourceAudit struct {
	cancel context.CancelFunc
}

func (q *downloadQueue) keepExpired(id string) {
	entry, found := q.find(id, q.entries.GetUntracked())
	if !found || entry.State != downloadComplete || entry.Availability.Expired == "" || q.removing[id] {
		return
	}
	q.update(id, true, func(entry *downloadEntry) {
		entry.Availability.Keep = true
		entry.Completion = nil
	})
}

func (q *downloadQueue) checkDownloadSources() {
	entries := q.entries.GetUntracked()
	for _, current := range q.app.sources.GetUntracked() {
		account := mediaAccountKey(current)
		if slices.ContainsFunc(entries, func(entry downloadEntry) bool {
			return entry.Account == account && entry.State == downloadComplete
		}) {
			q.app.checkSource(current, false)
		}
	}
}

// reconcileDownloadSource runs after an authenticated library request.
// Presence checks use that same account and endpoint.
func (q *downloadQueue) reconcileDownloadSource(current source, client media.Client, sourceErr error, refresh bool) {
	account := mediaAccountKey(current)
	if pending := q.audits[account]; pending != nil {
		if !refresh && sourceErr == nil {
			return
		}
		pending.cancel()
		delete(q.audits, account)
	}
	offline := unavailableDownloadSource(sourceErr)
	if sourceErr != nil && !offline {
		return
	}
	now := q.app.scope.WallTime()
	entries := slices.Clone(q.entries.GetUntracked())
	change := false
	var candidates []downloadEntry
	for i := range entries {
		entry := &entries[i]
		if entry.Account != account || entry.State != downloadComplete {
			continue
		}
		before := entry.Availability
		if offline {
			if entry.Availability.OfflineSince == 0 {
				entry.Availability.OfflineSince = now.Unix()
			}
			if entry.Availability.Expired == "" && !now.Before(time.Unix(entry.Availability.OfflineSince, 0).Add(downloadSourceOfflineAge)) {
				entry.Availability.Expired = downloadSourceOffline
			}
		} else {
			entry.Availability.OfflineSince = 0
			if refresh || entry.Availability.Expired == downloadSourceOffline {
				candidates = append(candidates, *entry)
			}
		}
		change = change || before != entry.Availability
	}
	if change {
		q.persist(entries)
	}
	if len(candidates) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(q.lifetime)
	audit := &downloadSourceAudit{cancel: cancel}
	q.audits[account] = audit
	go func() {
		defer cancel()
		results := make(map[string]downloadEntry)
		for _, entry := range candidates {
			if ctx.Err() != nil {
				return
			}
			probe, stop := context.WithTimeout(ctx, 8*time.Second)
			exists, err := media.ItemExists(probe, client, entry.Item)
			stop()
			if err == nil {
				entry.Availability.Expired = ""
				if !exists {
					entry.Availability.Expired = downloadMissingFromSource
				}
				results[entry.ID] = entry
			}
		}
		q.app.scope.Post(func() {
			if q.audits[account] != audit {
				return
			}
			delete(q.audits, account)
			if !slices.ContainsFunc(q.app.sources.GetUntracked(), func(source source) bool {
				return source.ID == current.ID && source.connection() == current.connection()
			}) {
				return
			}
			entries := slices.Clone(q.entries.GetUntracked())
			changed := false
			for i := range entries {
				entry := &entries[i]
				result, ok := results[entry.ID]
				if ok && entry.State == downloadComplete && entry.SavedAt == result.SavedAt && entry.Availability.Expired != result.Availability.Expired {
					entry.Availability.Expired = result.Availability.Expired
					changed = true
				}
			}
			if changed {
				q.persist(entries)
			}
		})
	}()
}

func unavailableDownloadSource(err error) bool {
	if errors.Is(err, media.ErrAuthentication) || errors.Is(err, context.Canceled) || errors.Is(err, errDifferentServer) {
		return false
	}
	var connection *net.OpError
	var dns *net.DNSError
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &connection) || errors.As(err, &dns) ||
		errors.Is(err, io.EOF) || errors.Is(err, media.StatusError(http.StatusBadGateway)) ||
		errors.Is(err, media.StatusError(http.StatusServiceUnavailable)) || errors.Is(err, media.StatusError(http.StatusGatewayTimeout))
}
