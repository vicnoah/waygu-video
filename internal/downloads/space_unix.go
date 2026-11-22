//go:build linux || darwin

package downloads

import "golang.org/x/sys/unix"

func availableBytes(dir string) *int64 {
	var info unix.Statfs_t
	if err := unix.Statfs(dir, &info); err != nil {
		return nil
	}
	bytes := int64(info.Bavail) * int64(info.Bsize)
	return &bytes
}
