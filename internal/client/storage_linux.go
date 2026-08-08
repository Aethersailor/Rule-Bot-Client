//go:build linux

package client

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"syscall"
)

const (
	minimumStorageReserve = uint64(16 * 1024 * 1024)
	maximumStorageReserve = uint64(256 * 1024 * 1024)
)

func ensureStorageReserve(path string, growth int64) error {
	if growth < 0 {
		return errors.New("storage growth must not be negative")
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(filepath.Dir(path), &stat); err != nil {
		return fmt.Errorf("inspect storage capacity: %w", err)
	}
	blockSize := uint64(stat.Bsize)
	available := multiplyStorageBytes(stat.Bavail, blockSize)
	total := multiplyStorageBytes(stat.Blocks, blockSize)
	reserve := total / 20
	if reserve < minimumStorageReserve {
		reserve = minimumStorageReserve
	}
	if reserve > maximumStorageReserve {
		reserve = maximumStorageReserve
	}
	if uint64(growth) > available || available-uint64(growth) < reserve {
		return fmt.Errorf("storage reserve reached: need %d bytes while preserving %d bytes", growth, reserve)
	}
	return nil
}

func multiplyStorageBytes(blocks uint64, blockSize uint64) uint64 {
	if blockSize != 0 && blocks > math.MaxUint64/blockSize {
		return math.MaxUint64
	}
	return blocks * blockSize
}
