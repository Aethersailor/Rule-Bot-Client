//go:build linux

package openwrt

import "syscall"

func availableBytes(path string) uint64 {
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) != nil {
		return 0
	}
	return stat.Bavail * uint64(stat.Bsize)
}
