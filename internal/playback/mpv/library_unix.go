//go:build (linux && !android) || (darwin && !ios)

package mpv

import (
	"runtime"

	"github.com/ebitengine/purego"
)

func openLibrary() (uintptr, error) {
	names := []string{"libmpv.so.2", "libmpv.so"}
	if runtime.GOOS == "darwin" {
		names = []string{"/opt/homebrew/lib/libmpv.2.dylib", "/usr/local/lib/libmpv.2.dylib", "libmpv.2.dylib"}
	}
	for _, name := range names {
		if handle, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_LOCAL); err == nil {
			return handle, nil
		}
	}
	return 0, ErrUnavailable
}

func symbolAddress(handle uintptr, name string) (uintptr, error) { return purego.Dlsym(handle, name) }

func closeLibrary(handle uintptr) { _ = purego.Dlclose(handle) }
