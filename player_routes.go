package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"time"

	"waygu-video/internal/media"
)

type playerRecovery struct {
	from     string
	position time.Duration
	paused   bool
	bitrate  int
}

func playbackRouteError(err error) bool {
	var network net.Error
	var status media.StatusError
	return errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &status) && status >= http.StatusInternalServerError)
}

func (a *application) recoverPlayer(current route, active playerMedia, position time.Duration, paused bool) bool {
	if current.recovery != nil || !a.routesEnabled.Load() || !current.source.automaticRoute() || active.endpoint.address == "" {
		return false
	}
	stack := a.routes.GetUntracked()
	last := &stack[len(stack)-1]
	if last.screen != screenPlayer || last.source.ID != current.source.ID || last.item.ID != current.item.ID || last.recovery != nil {
		return false
	}
	next := *last
	next.castOnOpen = false
	next.versionID = active.stream.VersionID()
	for _, track := range active.stream.Tracks {
		if track.Type == "audio" && track.Selected {
			next.audioTrack = track.ID
			break
		}
	}
	next.recovery = &playerRecovery{from: active.endpoint.address, position: position, paused: paused, bitrate: active.requestedBitrate}
	stack = slices.Clone(stack)
	stack[len(stack)-1] = next
	a.routes.Set(stack)
	return true
}
