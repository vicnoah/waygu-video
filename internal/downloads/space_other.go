//go:build !linux && !darwin && !windows

package downloads

func availableBytes(string) *int64 { return nil }
