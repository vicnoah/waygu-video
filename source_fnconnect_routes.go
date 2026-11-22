package main

import (
	"context"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/media"
)

const (
	fnRouteCacheTime = time.Minute
	fnRouteRetryTime = 5 * time.Second
)

type refreshFNConnect func(context.Context, string, string, func(string) bool, func(context.Context, string) (media.ServerInfo, error)) (media.FNConnectRoute, error)

var fnConnectRefresher = core.NewKey("waygu.fnConnectRefresher", refreshFNConnect(media.FNConnectClient{}.Refresh))

func (s source) canRefreshFN() bool {
	return s.Provider == media.Flymoo && s.FNID != "" && s.ServerID != "" && !s.RemoteDisabled
}

// Refresh state shares the artwork proxy lock so a new endpoint and its
// credential-scoped artwork authorization become visible together.
type fnRouteRefresh struct {
	cached    sourceEndpoint
	artwork   artworkAccess
	checkedAt time.Time
	retryAt   time.Time
	lastError error
	flight    *fnRouteLookup
}

type fnRouteLookup struct {
	done     chan struct{}
	endpoint sourceEndpoint
	err      error
}

func (a *application) cachedFNRoute(entry source) sourceEndpoint {
	art := &a.sourceArtwork
	art.mu.Lock()
	defer art.mu.Unlock()
	state := art.fnRoutes[entry.connection()]
	if state != nil && time.Since(state.checkedAt) < fnRouteCacheTime {
		return state.cached
	}
	return sourceEndpoint{}
}

func (a *application) retryFNRoute(entry source) {
	art := &a.sourceArtwork
	art.mu.Lock()
	defer art.mu.Unlock()
	if state := art.fnRoutes[entry.connection()]; state != nil {
		state.retryAt = time.Time{}
	}
}

func (a *application) refreshSourceRoute(ctx context.Context, entry source, excluded ...string) (sourceEndpoint, error) {
	if ctx.Err() != nil {
		return sourceEndpoint{}, ctx.Err()
	}
	if a.refreshFNConnect == nil {
		return sourceEndpoint{}, media.ErrFNConnectRoute
	}
	key := entry.connection()
	art := &a.sourceArtwork
	art.mu.Lock()
	state := art.fnRoutes[key]
	if state == nil || !a.routesEnabled.Load() {
		art.mu.Unlock()
		return sourceEndpoint{}, media.ErrFNConnectRoute
	}
	flight := state.flight
	if flight == nil {
		now := time.Now()
		if now.Sub(state.checkedAt) < 2*time.Second && fnRouteAllowed(entry, excluded, state.cached.address) {
			endpoint := state.cached
			art.mu.Unlock()
			return endpoint, nil
		}
		if now.Before(state.retryAt) {
			err := state.lastError
			art.mu.Unlock()
			return sourceEndpoint{}, err
		}
		flight = &fnRouteLookup{done: make(chan struct{})}
		state.flight = flight
		go a.lookupSourceRoute(entry, key, state, flight, excluded)
	}
	art.mu.Unlock()

	select {
	case <-ctx.Done():
		return sourceEndpoint{}, ctx.Err()
	case <-flight.done:
	}
	art.mu.Lock()
	defer art.mu.Unlock()
	if flight.err != nil {
		return sourceEndpoint{}, flight.err
	}
	if art.fnRoutes[key] != state || !a.routesEnabled.Load() || slices.Contains(excluded, flight.endpoint.address) {
		return sourceEndpoint{}, media.ErrFNConnectRoute
	}
	return flight.endpoint, nil
}

func (a *application) lookupSourceRoute(entry source, key sourceConnection, state *fnRouteRefresh, flight *fnRouteLookup, excluded []string) {
	// Each waiting screen has its own deadline. Leaving one screen should
	// not cancel the lookup already being used by a catalog or player.
	ctx, cancel := context.WithTimeout(a.lifetime, 6*time.Second)
	defer cancel()
	allowed := func(address string) bool {
		return fnRouteAllowed(entry, excluded, address)
	}
	route, err := a.refreshFNConnect(ctx, entry.FNID, entry.ServerID, allowed, func(probe context.Context, address string) (media.ServerInfo, error) {
		if !allowed(address) {
			return media.ServerInfo{}, media.ErrFNConnectRoute
		}
		return a.verifySourceAddress(probe, entry, address, entry.ServerID, 1700*time.Millisecond)
	})
	if err == nil && !allowed(route.Address) {
		err = media.ErrFNConnectRoute
	}

	art := &a.sourceArtwork
	art.mu.Lock()
	defer art.mu.Unlock()
	flight.err = err
	state.flight = nil
	if art.fnRoutes[key] == state && a.routesEnabled.Load() {
		if err != nil {
			state.lastError, state.retryAt = err, time.Now().Add(fnRouteRetryTime)
		} else {
			flight.endpoint = sourceEndpoint{address: route.Address, remote: route.Remote}
			state.cached, state.checkedAt = flight.endpoint, time.Now()
			state.artwork = entry.artworkAccess(route.Address)
			state.lastError, state.retryAt = nil, time.Time{}
			art.retireArtwork()
		}
	} else if flight.err == nil {
		flight.err = media.ErrFNConnectRoute
	}
	close(flight.done)
}

func fnRouteAllowed(entry source, excluded []string, address string) bool {
	target, err := media.NormalizeAddress(address)
	if err != nil || target.String() != address || slices.Contains(excluded, address) {
		return false
	}
	saved, err := media.NormalizeAddress(entry.Address)
	return err == nil && (saved.Scheme != "https" || target.Scheme == "https")
}
