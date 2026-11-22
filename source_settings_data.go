package main

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
	"waygu-video/internal/mediatls"
)

const sourceTimeoutKey = "source-timeout-v1"

var sourceTimeouts = []int{5, 10, 20, 30, 60}

type sourceConnection struct {
	localTrust, remoteTrust                               mediatls.Trust
	address, remote, server, account, credential, headers string
	remoteDisabled                                        bool
	provider                                              media.Provider
}

func (s source) connection() sourceConnection {
	return sourceConnection{s.certificateFor(s.Address), s.certificateFor(s.RemoteAddress), s.Address, s.RemoteAddress, s.ServerID, s.AccountID, s.credentialKey(), s.HeaderSecret, s.RemoteDisabled, s.Provider}
}

type sourceCheck struct {
	connection sourceConnection
	endpoint   sourceEndpoint
	request    uint64
	at         time.Time
	checking   bool
	syncing    bool
	err        error
	saveError  error
	scanSent   bool
	scanError  error
}

func readSourceTimeout(value string) int {
	timeout, _ := strconv.Atoi(value)
	if slices.Contains(sourceTimeouts, timeout) {
		return timeout
	}
	return 10
}

func (a *application) watchSourceConnections(cx *core.Scope) {
	cx.Effect(func() {
		a.sourceFailover.Get()
		for _, entry := range a.sources.Get() {
			a.checkSource(entry, false)
		}
	})
}

// checkSource validates the saved credentials against the library endpoint.
// An explicit refresh also reconciles the imports with the latest server list.
func (a *application) checkSource(entry source, refresh bool) {
	a.checkSourceWithScan(entry, refresh, false)
}

func (a *application) checkSourceWithScan(entry source, refresh, scan bool) {
	identity := entry.connection()
	previous := a.sourceChecks.GetUntracked()[entry.ID]
	if previous.connection == identity {
		if previous.syncing || (!refresh && (previous.checking || (!previous.at.IsZero() && a.scope.Now().Sub(previous.at) < time.Minute))) {
			return
		}
	}
	a.sourceCheckSerial++
	started := sourceCheck{connection: identity, request: a.sourceCheckSerial, checking: true, syncing: refresh}
	if previous.connection == identity && !scan {
		started.scanSent, started.scanError = previous.scanSent, previous.scanError
	}
	a.setSourceCheck(entry.ID, started)
	timeout := time.Duration(a.sourceTimeout.GetUntracked()) * time.Second
	lifetime := a.lifetime
	go func() {
		ctx, cancel := context.WithTimeout(lifetime, timeout)
		defer cancel()
		client, endpoint, err := a.selectSourceClient(ctx, entry, "")
		var available []media.Library
		scanSent, scanError := started.scanSent, started.scanError
		if err == nil && scan {
			if scanner, ok := client.(media.LibraryScanClient); ok {
				scanError = scanner.ScanLibraries(ctx)
				scanSent = scanError == nil
			}
		}
		if err == nil {
			available, err = client.Libraries(ctx)
		}
		a.scope.Post(func() {
			sources := a.sources.GetUntracked()
			index := slices.IndexFunc(sources, func(current source) bool {
				return current.ID == entry.ID && current.connection() == identity
			})
			// An explicit refresh may supersede a pending background probe.
			latest := a.sourceChecks.GetUntracked()[entry.ID]
			if index < 0 || latest.request != started.request || !latest.checking {
				return
			}
			finished := sourceCheck{connection: identity, endpoint: endpoint, request: started.request, at: a.scope.Now(), err: err,
				scanSent: scanSent, scanError: scanError}
			if refresh && err == nil {
				updated := slices.Clone(sources)
				updated[index].Libraries = mergeLibraries(available, updated[index].Libraries, true)
				updated[index].LibrariesSyncedAt = time.Now().Unix()
				finished.saveError = a.saveSources(updated)
				if finished.saveError == nil {
					a.libraryChanges.Update(func(revision uint64) uint64 { return revision + 1 })
				}
			}
			a.setSourceCheck(entry.ID, finished)
			if scan && scanSent {
				a.scope.AfterFunc(15*time.Second, func() {
					for _, current := range a.sources.GetUntracked() {
						if current.ID == entry.ID && current.connection() == identity {
							a.checkSource(current, true)
							break
						}
					}
				})
			}
		})
	}()
}

func (a *application) setSourceCheck(id string, status sourceCheck) {
	current := maps.Clone(a.sourceChecks.GetUntracked())
	current[id] = status
	a.sourceChecks.Set(current)
}

func (a *application) removeSource(entry source) error {
	entries := slices.Clone(a.sources.GetUntracked())
	index := slices.IndexFunc(entries, func(current source) bool { return current.ID == entry.ID })
	if index < 0 {
		return nil
	}
	entry = entries[index]
	if err := a.saveSources(slices.Delete(entries, index, index+1)); err != nil {
		return err
	}
	a.storage.RemoveItem(resumeCacheKey(entry))
	a.removeSourceProgress(entry)
	key := entry.credentialKey()
	a.tokens.Delete(key)
	checks := maps.Clone(a.sourceChecks.GetUntracked())
	delete(checks, entry.ID)
	a.sourceChecks.Set(checks)
	if a.sourceFilter.GetUntracked() == entry.ID {
		a.sourceFilter.Set("")
	}
	go func() { _ = a.secrets.Delete(key) }()
	a.forgetSourceHeaders(entry.HeaderSecret)
	return nil
}

func (a *application) moveSource(id string, delta int) error {
	entries := a.sources.GetUntracked()
	index := slices.IndexFunc(entries, func(entry source) bool { return entry.ID == id })
	other := index + delta
	if index < 0 || other < 0 || other >= len(entries) || index == other {
		return nil
	}
	moved := slices.Clone(entries)
	entry := moved[index]
	if other > index {
		copy(moved[index:other], moved[index+1:other+1])
	} else {
		copy(moved[other+1:index+1], moved[other:index])
	}
	moved[other] = entry
	return a.saveSources(moved)
}
