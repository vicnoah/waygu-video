package mpv

import (
	"os"
	"path/filepath"
	"syscall"
)

func openLibrary() (uintptr, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, ErrUnavailable
	}
	for _, name := range []string{"libmpv-2.dll", "mpv-2.dll"} {
		handle, err := syscall.LoadLibrary(filepath.Join(filepath.Dir(exe), name))
		if err == nil {
			return uintptr(handle), nil
		}
	}
	return 0, ErrUnavailable
}

func symbolAddress(handle uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(handle), name)
}

func closeLibrary(handle uintptr) { _ = syscall.FreeLibrary(syscall.Handle(handle)) }
