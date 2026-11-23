package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestApplePlaybackNetworkMonitor(t *testing.T) {
	if nsgotest.Target() == "" {
		t.Skip("requires an Apple device or simulator")
	}
	if !applePathWatch() {
		t.Fatal("could not start the Network framework path monitor")
	}
	_, seen, changed := appleNetworkSnapshot()
	if seen {
		return
	}
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("the Network framework did not publish its initial path")
	}
}
