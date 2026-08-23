//go:build windows

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const updateCleanupEnvironment = "RULE_BOT_CLIENT_UPDATE_CLEANUP"
const updateRecoveredEnvironment = "RULE_BOT_CLIENT_UPDATE_RECOVERED"

type windowsUpdatePlan struct {
	Target     string   `json:"target"`
	Payload    string   `json:"payload"`
	Backup     string   `json:"backup"`
	WorkDir    string   `json:"work_dir"`
	StatusPath string   `json:"status_path"`
	Args       []string `json:"args"`
}

type windowsUpdateStatus struct {
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
}

func platformClientUpdateKind(string) string { return "archive" }

func preparePlatformClientUpdate(_ context.Context, prepared *PreparedClientUpdate) error {
	prepared.candidate = filepath.Join(prepared.workDir, "rule-bot-client.candidate.exe")
	return extractClientUpdateBinary(prepared.downloadPath, prepared.Info.Target, prepared.candidate)
}

func copyWindowsUpdateFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func writeWindowsUpdateStatus(path, state string, updateErr error) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	status := windowsUpdateStatus{State: state, UpdatedAt: time.Now().UTC()}
	if updateErr != nil {
		status.Error = updateErr.Error()
	}
	data, err := json.Marshal(status)
	if err != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".update-status-*")
	if err != nil {
		return
	}
	name := temporary.Name()
	if _, err = temporary.Write(append(data, '\n')); err == nil {
		err = temporary.Close()
	} else {
		_ = temporary.Close()
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
}

func activatePlatformClientUpdate(_ context.Context, prepared *PreparedClientUpdate) error {
	backup := filepath.Join(prepared.workDir, "rule-bot-client.previous.exe")
	helper := filepath.Join(prepared.workDir, "rule-bot-client-update-helper.exe")
	if err := copyWindowsUpdateFile(prepared.options.Executable, backup); err != nil {
		return err
	}
	if err := copyWindowsUpdateFile(prepared.candidate, helper); err != nil {
		return err
	}
	plan := windowsUpdatePlan{
		Target: prepared.options.Executable, Payload: prepared.candidate, Backup: backup,
		WorkDir: prepared.workDir, StatusPath: filepath.Join(filepath.Dir(prepared.options.Executable), "data", "update-status.json"),
		Args: append([]string(nil), prepared.options.RestartArgs...),
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	planPath := filepath.Join(prepared.workDir, "replace.json")
	if err := os.WriteFile(planPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	command := exec.Command(helper, "--update-helper", planPath)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	if err := command.Process.Release(); err != nil {
		return err
	}
	prepared.workDir = ""
	return ErrRestartScheduled
}

func validWindowsUpdatePlan(plan windowsUpdatePlan) error {
	workDir, err := filepath.Abs(plan.WorkDir)
	if err != nil || !strings.HasPrefix(strings.ToLower(filepath.Base(workDir)), "rule-bot-client-update-") {
		return errors.New("update helper work directory is invalid")
	}
	tempRoot, err := filepath.Abs(os.TempDir())
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(tempRoot, workDir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("update helper work directory is outside the temporary directory")
	}
	for _, item := range []string{plan.Payload, plan.Backup} {
		relative, err = filepath.Rel(workDir, item)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("update helper file is outside its work directory")
		}
	}
	if !strings.EqualFold(filepath.Base(plan.Target), "rule-bot-client.exe") {
		return errors.New("update helper target is invalid")
	}
	return nil
}

func replaceWindowsExecutable(source, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		stage, err := os.CreateTemp(filepath.Dir(target), ".rule-bot-client-update-*.exe")
		if err != nil {
			return err
		}
		stagePath := stage.Name()
		_ = stage.Close()
		_ = os.Remove(stagePath)
		if err = copyWindowsUpdateFile(source, stagePath); err == nil {
			err = os.Rename(stagePath, target)
		}
		if err == nil {
			return nil
		}
		_ = os.Remove(stagePath)
		if time.Now().After(deadline) {
			return fmt.Errorf("replace executable: %w", err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func startWindowsUpdateTarget(plan windowsUpdatePlan, recovered bool) error {
	command := exec.Command(plan.Target, plan.Args...)
	command.Env = append(os.Environ(), updateCleanupEnvironment+"="+plan.WorkDir)
	if recovered {
		command.Env = append(command.Env, updateRecoveredEnvironment+"=1")
	}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	return nil
}

func RunClientUpdateHelper(planPath string) error {
	data, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var plan windowsUpdatePlan
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("update replacement plan contains trailing data")
	}
	if err := validWindowsUpdatePlan(plan); err != nil {
		return err
	}
	writeWindowsUpdateStatus(plan.StatusPath, "installing", nil)
	if err := replaceWindowsExecutable(plan.Payload, plan.Target, 30*time.Second); err != nil {
		writeWindowsUpdateStatus(plan.StatusPath, "failed", err)
		return err
	}
	if err := startWindowsUpdateTarget(plan, false); err != nil {
		restoreErr := replaceWindowsExecutable(plan.Backup, plan.Target, 5*time.Second)
		var restartErr error
		if restoreErr == nil {
			restartErr = startWindowsUpdateTarget(plan, true)
		}
		combined := errors.Join(err, restoreErr, restartErr)
		writeWindowsUpdateStatus(plan.StatusPath, "failed", combined)
		return combined
	}
	writeWindowsUpdateStatus(plan.StatusPath, "completed", nil)
	return nil
}

func CleanupClientUpdateTemp() {
	directory := os.Getenv(updateCleanupEnvironment)
	if directory == "" {
		return
	}
	_ = os.Unsetenv(updateCleanupEnvironment)
	go func() {
		time.Sleep(2 * time.Second)
		var plan windowsUpdatePlan
		plan.WorkDir = directory
		plan.Payload = filepath.Join(directory, "payload")
		plan.Backup = filepath.Join(directory, "backup")
		plan.Target = "rule-bot-client.exe"
		if validWindowsUpdatePlan(plan) == nil {
			_ = os.RemoveAll(directory)
		}
	}()
}
