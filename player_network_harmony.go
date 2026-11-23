//go:build js && wasm && nsgoharmony

package main

import (
	"context"
	"syscall/js"

	"github.com/nsgo-dev/nsgo/core"
)

func init() {
	nativeCellularPlayback = func(context.Context) bool {
		return harmonyDownloadNetwork() == downloadNetworkCellular
	}
	watchNativeDownloadNetwork = func(cx *core.Scope, update func(downloadNetwork)) {
		global := js.Global()
		navigator := global.Get("navigator")
		if navigator.Type() != js.TypeObject {
			update(downloadNetworkUnobserved)
			return
		}
		connection := navigator.Get("connection")
		publish := func() { update(harmonyDownloadNetwork()) }
		listener := js.FuncOf(func(js.Value, []js.Value) any {
			cx.Post(publish)
			return nil
		})
		global.Call("addEventListener", "online", listener)
		global.Call("addEventListener", "offline", listener)
		if connection.Type() == js.TypeObject {
			connection.Call("addEventListener", "change", listener)
		}
		cx.Cleanup(func() {
			global.Call("removeEventListener", "online", listener)
			global.Call("removeEventListener", "offline", listener)
			if connection.Type() == js.TypeObject {
				connection.Call("removeEventListener", "change", listener)
			}
			listener.Release()
		})
		publish()
	}
}

func harmonyDownloadNetwork() downloadNetwork {
	navigator := js.Global().Get("navigator")
	if navigator.Type() != js.TypeObject {
		return downloadNetworkUnobserved
	}
	if online := navigator.Get("onLine"); online.Type() == js.TypeBoolean && !online.Bool() {
		return downloadNetworkOffline
	}
	connection := navigator.Get("connection")
	if connection.Type() != js.TypeObject {
		return downloadNetworkUnobserved
	}
	transport := connection.Get("type")
	if transport.Type() != js.TypeString {
		return downloadNetworkUnobserved
	}
	switch transport.String() {
	case "ethernet":
		return downloadNetworkWired
	case "none":
		return downloadNetworkOffline
	}
	return parseDownloadNetwork(transport.String())
}
