//go:build js && wasm && nsgoharmony

package main

import (
	"context"
	"syscall/js"
)

func init() {
	nativeCellularPlayback = func(context.Context) bool {
		navigator := js.Global().Get("navigator")
		if navigator.Type() != js.TypeObject {
			return false
		}
		connection := navigator.Get("connection")
		if connection.Type() != js.TypeObject {
			return false
		}
		transport := connection.Get("type")
		return transport.Type() == js.TypeString && transport.String() == "cellular"
	}
}
