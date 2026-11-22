package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"waygu-video/internal/media"
)

const sourceFailoverKey = "source-route-failover-v1"

var errDifferentServer = errors.New("the backup address belongs to a different server")

type sourceEndpoint struct {
	address string
	remote  bool
}

func (s source) automaticRoute() bool {
	return s.RemoteAddress != "" && s.RemoteAddress != s.Address && s.ServerID != "" && !s.RemoteDisabled
}

func (s source) capBitrate(endpoint sourceEndpoint, bitrate int) int {
	if endpoint.remote && s.RemoteBitrate > 0 && (bitrate == 0 || bitrate > s.RemoteBitrate) {
		return s.RemoteBitrate
	}
	return bitrate
}

func backupAddress(raw, primary string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	address, err := media.NormalizeAddress(raw)
	if err != nil || address.String() == primary {
		return "", media.ErrInvalidAddress
	}
	return address.String(), nil
}

func (a *application) probeSourceAddress(ctx context.Context, kind media.Provider, address string, timeout time.Duration) (media.ServerInfo, error) {
	probe, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	guest, err := a.newMedia(kind, address, a.device, media.Session{})
	if err != nil {
		return media.ServerInfo{}, err
	}
	return guest.Probe(probe)
}

func (a *application) verifySourceAddress(ctx context.Context, kind media.Provider, address, expected string, timeout time.Duration) (media.ServerInfo, error) {
	info, err := a.probeSourceAddress(ctx, kind, address, timeout)
	if err == nil && (expected == "" || info.ID != expected) {
		err = errDifferentServer
	}
	return info, err
}

// resolveSource uses public identity before loading the saved account. Each
// operation starts with the local address so it can recover after a network change.
func (a *application) resolveSource(ctx context.Context, entry source, skip string) (sourceEndpoint, error) {
	primary := sourceEndpoint{address: entry.Address}
	if !a.routesEnabled.Load() || !entry.automaticRoute() {
		if skip == primary.address {
			return sourceEndpoint{}, media.ErrNoStream
		}
		return primary, nil
	}
	var localError error
	if skip != entry.Address {
		if _, err := a.verifySourceAddress(ctx, entry.Provider, entry.Address, entry.ServerID, 1500*time.Millisecond); err == nil {
			return primary, nil
		} else {
			localError = err
		}
	}
	if ctx.Err() != nil {
		return sourceEndpoint{}, ctx.Err()
	}
	if skip != entry.RemoteAddress {
		if _, err := a.verifySourceAddress(ctx, entry.Provider, entry.RemoteAddress, entry.ServerID, 3*time.Second); err == nil {
			return sourceEndpoint{address: entry.RemoteAddress, remote: true}, nil
		} else {
			return sourceEndpoint{}, errors.Join(localError, err)
		}
	}
	return sourceEndpoint{}, localError
}

func (a *application) selectSourceClient(ctx context.Context, entry source, skip string) (media.Client, sourceEndpoint, error) {
	endpoint, err := a.resolveSource(ctx, entry, skip)
	if err != nil {
		return nil, sourceEndpoint{}, err
	}
	client, err := a.clientAt(entry, endpoint.address)
	return client, endpoint, err
}

func (a *application) client(ctx context.Context, entry source) (media.Client, error) {
	client, _, err := a.selectSourceClient(ctx, entry, "")
	return client, err
}

func (a *application) updateSourceRouting(entry source, change func(*source)) error {
	entries := slices.Clone(a.sources.GetUntracked())
	index := slices.IndexFunc(entries, func(current source) bool { return current.ID == entry.ID && current.connection() == entry.connection() })
	if index < 0 {
		return media.ErrResponse
	}
	change(&entries[index])
	if err := a.saveSources(entries); err != nil {
		return err
	}
	a.libraryChanges.Update(func(revision uint64) uint64 { return revision + 1 })
	return nil
}

func (a *application) setSourceFailover(enabled bool) error {
	data := "false"
	if enabled {
		data = "true"
	}
	if err := a.storage.SetItem(sourceFailoverKey, data); err != nil {
		return err
	}
	a.routesEnabled.Store(enabled)
	a.sourceChecks.Set(map[string]sourceCheck{})
	a.sourceFailover.Set(enabled)
	a.libraryChanges.Update(func(revision uint64) uint64 { return revision + 1 })
	return nil
}
