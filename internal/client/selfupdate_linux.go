//go:build linux

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func platformClientUpdateKind(executable string) string {
	if filepath.Clean(executable) == "/usr/bin/rule-bot-client" {
		command := exec.Command("dpkg-query", "-W", "-f=${db:Status-Abbrev}", "rule-bot-client")
		if output, err := command.Output(); err == nil && strings.TrimSpace(string(output)) == "ii" {
			return "deb"
		}
	}
	return "archive"
}

func preparePlatformClientUpdate(ctx context.Context, prepared *PreparedClientUpdate) error {
	if prepared.Info.Kind == "archive" {
		prepared.candidate = filepath.Join(prepared.workDir, "rule-bot-client.candidate")
		return extractClientUpdateBinary(prepared.downloadPath, prepared.Info.Target, prepared.candidate)
	}
	root := filepath.Join(prepared.workDir, "deb-root")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if output, err := exec.CommandContext(ctx, "dpkg-deb", "-x", prepared.downloadPath, root).CombinedOutput(); err != nil {
		return fmt.Errorf("extract candidate Debian package: %s", strings.TrimSpace(string(output)))
	}
	prepared.candidate = filepath.Join(root, "usr", "bin", "rule-bot-client")
	if _, err := os.Stat(prepared.candidate); err != nil {
		return errors.New("candidate Debian package does not contain rule-bot-client")
	}
	if !stableUpdateVersion.MatchString(BuildVersion) {
		return nil
	}
	manifestURL, err := clientUpdateManifestURL(prepared.options, BuildVersion)
	if err != nil {
		return err
	}
	manifest, err := fetchClientUpdateManifest(ctx, manifestURL, prepared.options)
	if err != nil {
		return fmt.Errorf("prepare Debian rollback: %w", err)
	}
	if manifest.Version != BuildVersion {
		return errors.New("rollback manifest does not match the installed version")
	}
	asset, err := selectClientUpdateAsset(manifest, BuildTarget, "deb")
	if err != nil {
		return fmt.Errorf("prepare Debian rollback: %w", err)
	}
	prepared.rollback = filepath.Join(prepared.workDir, "rollback.deb")
	return downloadClientUpdateAsset(ctx, prepared.options, BuildVersion, asset, prepared.rollback)
}

func copyClientUpdateFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}

func replaceLinuxExecutable(source, target string) error {
	directory := filepath.Dir(target)
	stage, err := os.CreateTemp(directory, ".rule-bot-client-update-*")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	_ = stage.Close()
	_ = os.Remove(stagePath)
	if err := copyClientUpdateFile(source, stagePath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(stagePath, target); err != nil {
		_ = os.Remove(stagePath)
		return err
	}
	return nil
}

func commandOutput(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %s", filepath.Base(name), strings.TrimSpace(string(output)))
	}
	return nil
}

func systemdServiceActive(ctx context.Context) bool {
	return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "rule-bot-client.service").Run() == nil
}

func restartLinuxService(ctx context.Context, wasActive bool) error {
	if !wasActive {
		return nil
	}
	if err := commandOutput(ctx, "systemctl", "try-restart", "rule-bot-client.service"); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if systemdServiceActive(ctx) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("updated rule-bot-client service did not become active")
}

func activateLinuxArchive(ctx context.Context, prepared *PreparedClientUpdate, wasActive bool) error {
	backup := filepath.Join(prepared.workDir, "previous-rule-bot-client")
	if err := copyClientUpdateFile(prepared.options.Executable, backup, 0o700); err != nil {
		return err
	}
	rollback := func(cause error) error {
		restoreErr := replaceLinuxExecutable(backup, prepared.options.Executable)
		if restoreErr == nil {
			restoreErr = restartLinuxService(ctx, wasActive)
		}
		if restoreErr != nil {
			return fmt.Errorf("update failed: %v; rollback failed: %w", cause, restoreErr)
		}
		return fmt.Errorf("update failed and was rolled back: %w", cause)
	}
	if err := replaceLinuxExecutable(prepared.candidate, prepared.options.Executable); err != nil {
		return err
	}
	if err := verifyClientUpdateCandidate(ctx, prepared.options.Executable, prepared.Info, prepared.options.ConfigPath); err != nil {
		return rollback(err)
	}
	if err := restartLinuxService(ctx, wasActive); err != nil {
		return rollback(err)
	}
	return nil
}

func installDebianUpdate(ctx context.Context, packagePath string, allowDowngrade bool) error {
	args := []string{"install", "-y", "--no-install-recommends"}
	if allowDowngrade {
		args = append(args, "--allow-downgrades")
	}
	args = append(args, packagePath)
	return commandOutput(ctx, "apt-get", args...)
}

func activateLinuxDeb(ctx context.Context, prepared *PreparedClientUpdate, wasActive bool) error {
	installErr := installDebianUpdate(ctx, prepared.downloadPath, false)
	if installErr == nil {
		installErr = verifyClientUpdateCandidate(ctx, prepared.options.Executable, prepared.Info, prepared.options.ConfigPath)
	}
	if installErr == nil {
		installErr = restartLinuxService(ctx, wasActive)
	}
	if installErr == nil || prepared.rollback == "" {
		return installErr
	}
	rollbackErr := installDebianUpdate(ctx, prepared.rollback, true)
	if rollbackErr == nil {
		rollbackErr = restartLinuxService(ctx, wasActive)
	}
	if rollbackErr != nil {
		return fmt.Errorf("update failed: %v; rollback failed: %w", installErr, rollbackErr)
	}
	return fmt.Errorf("update failed and was rolled back: %w", installErr)
}

func activatePlatformClientUpdate(ctx context.Context, prepared *PreparedClientUpdate) error {
	wasActive := systemdServiceActive(ctx)
	if prepared.Info.Kind == "deb" {
		return activateLinuxDeb(ctx, prepared, wasActive)
	}
	return activateLinuxArchive(ctx, prepared, wasActive)
}

func RunClientUpdateHelper(string) error {
	return errors.New("update helper mode is only available on Windows")
}

func CleanupClientUpdateTemp() {}
