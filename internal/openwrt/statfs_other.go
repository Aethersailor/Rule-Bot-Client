//go:build !linux

package openwrt

func availableBytes(string) uint64 { return 0 }
