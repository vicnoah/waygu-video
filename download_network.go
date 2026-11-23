package main

import (
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/locales"
)

const (
	downloadWiFiKey        = "offline-download-wifi-v1"
	androidNetworkTypeCall = "waygu.network.type"
	androidNetworkChange   = "waygu.network.changed"
)

type downloadNetwork uint8

const (
	downloadNetworkUnknown downloadNetwork = iota
	downloadNetworkWiFi
	downloadNetworkWired
	downloadNetworkCellular
	downloadNetworkOffline
	downloadNetworkUnobserved
)

func parseDownloadNetwork(value any) downloadNetwork {
	switch value {
	case "wifi":
		return downloadNetworkWiFi
	case "wired":
		return downloadNetworkWired
	case "cellular":
		return downloadNetworkCellular
	case "offline":
		return downloadNetworkOffline
	default:
		return downloadNetworkUnknown
	}
}

var watchNativeDownloadNetwork func(*core.Scope, func(downloadNetwork))

func watchDownloadNetwork(cx *core.Scope, state *core.Signal[downloadNetwork]) {
	if cx.Env().OS() == core.OSAndroid {
		eventReceived := false
		cx.OnHostEvent(androidNetworkChange, func(data any) {
			eventReceived = true
			state.Set(parseDownloadNetwork(data))
		})
		cx.CallHostTimeout(androidNetworkTypeCall, nil, 1500*time.Millisecond, func(data any, err error) {
			if !eventReceived && err == nil {
				state.Set(parseDownloadNetwork(data))
			}
		})
	} else if watchNativeDownloadNetwork != nil {
		watchNativeDownloadNetwork(cx, func(network downloadNetwork) { state.Set(network) })
	} else {
		state.Set(downloadNetworkUnobserved)
	}
}

type downloadNetworkWait uint8

const (
	downloadNetworkReady downloadNetworkWait = iota
	downloadWaitWiFi
	downloadWaitConnection
)

func downloadWaitFor(network downloadNetwork, wifiOnly bool) downloadNetworkWait {
	if wifiOnly && (network == downloadNetworkUnknown || network == downloadNetworkCellular || network == downloadNetworkOffline) {
		return downloadWaitWiFi
	}
	if network == downloadNetworkOffline {
		return downloadWaitConnection
	}
	return downloadNetworkReady
}

func (q *downloadQueue) waitFor(entry downloadEntry) downloadNetworkWait {
	return downloadWaitFor(q.network.Get(), q.wifiOnly.Get() || entry.WaitForWiFi)
}

func (q *downloadQueue) networkChanged() {
	entries := q.entries.GetUntracked()
	var changed []downloadEntry
	var cancels []func()
	for i, entry := range entries {
		if entry.State != downloadRunning || q.waitFor(entry) == downloadNetworkReady {
			continue
		}
		if changed == nil {
			changed = slices.Clone(entries)
		}
		changed[i].State, changed[i].Failure, changed[i].Rate = downloadQueued, "", 0
		if cancel := q.running[entry.ID]; cancel != nil {
			cancels = append(cancels, cancel)
		}
	}
	if changed != nil {
		// Cancel even when persistence fails. Reopening repairs a saved
		// running entry to queued and checks the network before trying it.
		if !q.persist(changed) {
			q.entries.Set(changed)
		}
		for _, cancel := range cancels {
			cancel()
		}
	}
	q.pump()
}

func (q *downloadQueue) setWiFiOnly(value bool) {
	if q.app.storage.SetItem(downloadWiFiKey, strconv.FormatBool(value)) != nil {
		q.failure.Set("save")
		return
	}
	q.wifiOnly.Set(value)
}

func (q *downloadQueue) useCellular(id string) {
	if q.wifiOnly.GetUntracked() || q.network.GetUntracked() != downloadNetworkCellular {
		return
	}
	if q.update(id, true, func(entry *downloadEntry) { entry.WaitForWiFi = false }) {
		q.pump()
	}
}

func (wait downloadNetworkWait) label(cx *core.Scope) string {
	if wait == downloadWaitWiFi {
		return locales.DownloadWaitWiFi(cx)
	}
	return locales.DownloadWaitNetwork(cx)
}
