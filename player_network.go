package main

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

const androidCellularHostCall = "waygu.network.cellular"

type playbackTransport uint8

const (
	transportPending playbackTransport = iota
	transportDefault
	transportCellular
)

var nativeCellularPlayback func(context.Context) bool

func playbackNetwork(cx *core.Scope, local bool) *core.Signal[playbackTransport] {
	network := core.NewSignal(cx, transportPending)
	finish := func(cellular bool) {
		if network.GetUntracked() != transportPending {
			return
		}
		if cellular {
			network.Set(transportCellular)
		} else {
			network.Set(transportDefault)
		}
	}
	const timeout = 1500 * time.Millisecond
	env := cx.Env()
	appleTV := env.OS() == core.OSIOS && env.Idiom() == core.IdiomTV
	probe := nativeCellularPlayback
	switch {
	case local || appleTV:
		finish(false)
	case env.OS() == core.OSAndroid:
		cx.CallHostTimeout(androidCellularHostCall, nil, timeout, func(result any, err error) {
			cellular, _ := result.(bool)
			finish(err == nil && cellular)
		})
	case probe != nil:
		cx.AfterFunc(timeout, func() { finish(false) })
		lifetime := cx.Context()
		go func() {
			ctx, cancel := context.WithTimeout(lifetime, timeout)
			defer cancel()
			cellular := probe(ctx)
			cx.Post(func() { finish(cellular) })
		}()
	default:
		finish(false)
	}
	return network
}

func playbackCellularWarning(cx *core.Scope, open func() bool, back, proceed func()) core.Node {
	return ui.AlertDialog{Title: locales.PlaybackCellularConfirmTitle(cx),
		Description: locales.PlaybackCellularConfirmDescription(cx), TestID: "playback-cellular-warning", Open: open,
		Actions: []ui.AlertAction{
			{Text: locales.DesktopSettingsCancel(cx), TestID: "playback-cellular-cancel", Role: core.AlertCancel,
				Variant: ui.ButtonOutline, OnPress: back},
			{Text: locales.PlaybackCellularContinue(cx), TestID: "playback-cellular-continue", OnPress: proceed},
		},
	}
}
