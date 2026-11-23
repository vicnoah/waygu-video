package main

import (
	"context"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	applePathMu    sync.Mutex
	applePath      downloadNetwork
	applePathSeen  bool
	applePathEvent = make(chan struct{})
	appleMonitor   uintptr
	applePathWatch = sync.OnceValue(startApplePathMonitor)
)

func init() {
	nativeCellularPlayback = func(ctx context.Context) bool {
		if !applePathWatch() {
			return false
		}
		for {
			network, seen, changed := appleNetworkSnapshot()
			if seen {
				return network == downloadNetworkCellular
			}
			select {
			case <-changed:
			case <-ctx.Done():
				return false
			}
		}
	}
	watchNativeDownloadNetwork = func(cx *core.Scope, update func(downloadNetwork)) {
		lifetime := cx.Context()
		go func() {
			if !applePathWatch() {
				cx.Post(func() { update(downloadNetworkUnobserved) })
				return
			}
			for {
				network, _, changed := appleNetworkSnapshot()
				cx.Post(func() { update(network) })
				select {
				case <-changed:
				case <-lifetime.Done():
					return
				}
			}
		}()
	}
}

func appleNetworkSnapshot() (downloadNetwork, bool, <-chan struct{}) {
	applePathMu.Lock()
	defer applePathMu.Unlock()
	return applePath, applePathSeen, applePathEvent
}

func startApplePathMonitor() bool {
	network, err := purego.Dlopen("/System/Library/Frameworks/Network.framework/Network", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return false
	}
	system, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return false
	}
	var create func() uintptr
	var start func(uintptr)
	var setQueue func(uintptr, uintptr)
	var setHandler func(uintptr, objc.Block)
	var createQueue func(string, uintptr) uintptr
	var status func(uintptr) int32
	var usesInterface func(uintptr, int32) bool
	purego.RegisterLibFunc(&create, network, "nw_path_monitor_create")
	purego.RegisterLibFunc(&start, network, "nw_path_monitor_start")
	purego.RegisterLibFunc(&setQueue, network, "nw_path_monitor_set_queue")
	purego.RegisterLibFunc(&setHandler, network, "nw_path_monitor_set_update_handler")
	purego.RegisterLibFunc(&createQueue, system, "dispatch_queue_create")
	purego.RegisterLibFunc(&status, network, "nw_path_get_status")
	purego.RegisterLibFunc(&usesInterface, network, "nw_path_uses_interface_type")
	appleMonitor = create()
	if appleMonitor == 0 {
		return false
	}
	const (
		nwPathSatisfied     = 1
		nwInterfaceWiFi     = 1
		nwInterfaceCellular = 2
		nwInterfaceEthernet = 3
	)
	handler := objc.NewBlock(func(_ objc.Block, path uintptr) {
		var network downloadNetwork
		switch {
		case status(path) != nwPathSatisfied:
			network = downloadNetworkOffline
		case usesInterface(path, nwInterfaceCellular):
			network = downloadNetworkCellular
		case usesInterface(path, nwInterfaceWiFi):
			network = downloadNetworkWiFi
		case usesInterface(path, nwInterfaceEthernet):
			network = downloadNetworkWired
		}
		applePathMu.Lock()
		if !applePathSeen || applePath != network {
			applePath, applePathSeen = network, true
			close(applePathEvent)
			applePathEvent = make(chan struct{})
		}
		applePathMu.Unlock()
	})
	setQueue(appleMonitor, createQueue("dev.nsgo.wayguvideo.network", 0))
	setHandler(appleMonitor, handler)
	handler.Release()
	// The process keeps one monitor so the next player receives a recent path.
	start(appleMonitor)
	return true
}
