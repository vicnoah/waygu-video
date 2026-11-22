package main

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	appleCellular  atomic.Bool
	applePathReady = make(chan struct{})
	appleMonitor   uintptr
	applePathWatch = sync.OnceValue(startApplePathMonitor)
)

func init() {
	nativeCellularPlayback = func(ctx context.Context) bool {
		if !applePathWatch() {
			return false
		}
		select {
		case <-applePathReady:
			return appleCellular.Load()
		case <-ctx.Done():
			return false
		}
	}
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
		nwInterfaceCellular = 2
	)
	var observed sync.Once
	handler := objc.NewBlock(func(_ objc.Block, path uintptr) {
		appleCellular.Store(status(path) == nwPathSatisfied && usesInterface(path, nwInterfaceCellular))
		observed.Do(func() { close(applePathReady) })
	})
	setQueue(appleMonitor, createQueue("dev.nsgo.wayguvideo.network", 0))
	setHandler(appleMonitor, handler)
	handler.Release()
	// The process keeps one monitor so the next player receives a recent path.
	start(appleMonitor)
	return true
}
