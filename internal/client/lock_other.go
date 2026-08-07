//go:build !linux

package client

import "os"

func lockFile(_ *os.File) error   { return nil }
func unlockFile(_ *os.File) error { return nil }
