//go:build !linux && !windows

package client

import (
	"context"
	"errors"
)

func platformClientUpdateKind(string) string { return "archive" }

func preparePlatformClientUpdate(context.Context, *PreparedClientUpdate) error {
	return errors.New("automatic updates are unavailable on this operating system")
}

func activatePlatformClientUpdate(context.Context, *PreparedClientUpdate) error {
	return errors.New("automatic updates are unavailable on this operating system")
}

func RunClientUpdateHelper(string) error {
	return errors.New("update helper mode is only available on Windows")
}

func CleanupClientUpdateTemp() {}
