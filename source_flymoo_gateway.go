package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"waygu-video/internal/media"
)

type connectionProbe struct {
	client  media.Client
	address string
	info    media.ServerInfo
}

// probeConnectionAddress is for onboarding. Stored accounts keep the exact
// verified URL and use normal, same-origin authenticated requests.
func (a *application) probeConnectionAddress(ctx context.Context, form source, address string, timeout time.Duration) (connectionProbe, error) {
	probe, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if form.Provider == media.Flymoo {
		return a.probeFlymooGateway(probe, form, address)
	}
	client, err := a.sourceClient(form, address, media.Session{}, false)
	if err != nil {
		return connectionProbe{}, err
	}
	info, err := client.Probe(probe)
	return connectionProbe{client: client, address: address, info: info}, sourceAddressError(address, err)
}

func (a *application) probeFlymooGateway(ctx context.Context, form source, address string) (connectionProbe, error) {
	base, err := media.NormalizeAddress(address)
	if err != nil {
		return connectionProbe{}, err
	}
	// A bare gateway may serve the media APIs at either its root or /v.
	// Keep a supplied reverse-proxy prefix unchanged unless the server moves it.
	preferred, fallback := address, ""
	if base.Path == "" {
		base.Path, base.RawPath = "/v", ""
		fallback = base.String()
		switch base.Port() {
		case "5666", "5667", "8000", "8001":
			preferred, fallback = fallback, preferred
		}
	}
	next := preferred
	visited := make(map[string]bool)
	var legacy connectionProbe
	moved := false
	for len(visited) < 5 && !visited[next] {
		visited[next] = true
		client, err := a.sourceClient(form, next, media.Session{}, false)
		if err != nil {
			return connectionProbe{}, err
		}
		result := connectionProbe{client: client, address: next}
		if next == preferred {
			legacy = result
		}
		if prober, ok := client.(media.FlymooGatewayProber); ok {
			result.info, err = prober.ProbeFlymooGateway(ctx)
		} else {
			result.info, err = client.Probe(ctx)
		}
		if err == nil {
			return result, nil
		}
		var redirect *media.FlymooGatewayRedirect
		if errors.As(err, &redirect) {
			moved, next = true, redirect.Address
			continue
		}
		err = sourceAddressError(next, err)
		var certificate *sourceCertificateError
		if errors.As(err, &certificate) {
			return connectionProbe{}, err
		}
		if moved || errors.Is(err, media.ErrFlymooGateway) {
			return connectionProbe{}, errors.Join(media.ErrFlymooGateway, err)
		}
		if next == preferred && fallback != "" && (errors.Is(err, media.StatusError(http.StatusNotFound)) || errors.Is(err, media.ErrResponse)) {
			next = fallback
			continue
		}
		// Older media deployments can accept login before sys/config. Only
		// the original origin is eligible for this compatibility path.
		return legacy, err
	}
	return connectionProbe{}, media.ErrFlymooGateway
}

func (a *application) verifyConnectionAddress(ctx context.Context, form source, address, expected string, timeout time.Duration) (connectionProbe, error) {
	result, err := a.probeConnectionAddress(ctx, form, address, timeout)
	if err == nil && (expected == "" || result.info.ID != expected) {
		return connectionProbe{}, errDifferentServer
	}
	return result, err
}
