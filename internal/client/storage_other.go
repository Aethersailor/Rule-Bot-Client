//go:build !linux

package client

func ensureStorageReserve(string, int64) error { return nil }
