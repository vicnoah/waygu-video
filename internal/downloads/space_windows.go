package downloads

import "golang.org/x/sys/windows"

func availableBytes(dir string) *int64 {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(path, &free, nil, nil); err != nil {
		return nil
	}
	bytes := int64(free)
	return &bytes
}
