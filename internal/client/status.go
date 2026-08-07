package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const runtimeStatusVersion = 1

type RuntimeStatus struct {
	Version   int                       `json:"version"`
	StartedAt time.Time                 `json:"started_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
	Instances map[string]InstanceStatus `json:"instances"`
	Output    OutputStatus              `json:"output"`
	RuleBot   RuleBotStatus             `json:"rule_bot"`
}

type InstanceStatus struct {
	Name            string    `json:"name"`
	URL             string    `json:"url"`
	Connected       bool      `json:"connected"`
	LastConnectedAt time.Time `json:"last_connected_at,omitempty"`
	LastEventAt     time.Time `json:"last_event_at,omitempty"`
	CapturedEvents  uint64    `json:"captured_events"`
	Reconnects      uint64    `json:"reconnects"`
	RecentError     string    `json:"recent_error,omitempty"`
	RetryIn         string    `json:"retry_in,omitempty"`
}

type OutputStatus struct {
	Path    string `json:"path"`
	Domains uint64 `json:"domains"`
	Bytes   int64  `json:"bytes"`
}

type RuleBotStatus struct {
	Enabled   bool   `json:"enabled"`
	StateFile string `json:"state_file,omitempty"`
	Offset    int64  `json:"offset"`
}

type statusReporter struct {
	path      string
	stateFile string
	store     *outputStore

	mutex  sync.Mutex
	status RuntimeStatus
	dirty  chan struct{}
}

func newStatusReporter(cfg Config, store *outputStore, existingDomains int) *statusReporter {
	if cfg.StatusFile == "" {
		return nil
	}
	instances := make(map[string]InstanceStatus, len(cfg.Instances))
	for _, instance := range cfg.Instances {
		instances[instance.Name] = InstanceStatus{Name: instance.Name, URL: instance.URL}
	}
	now := time.Now().UTC()
	return &statusReporter{
		path:      cfg.StatusFile,
		stateFile: cfg.RuleBot.StateFile,
		store:     store,
		status: RuntimeStatus{
			Version:   runtimeStatusVersion,
			StartedAt: now,
			UpdatedAt: now,
			Instances: instances,
			Output: OutputStatus{
				Path:    cfg.Output,
				Domains: uint64(existingDomains),
				Bytes:   store.durable.Load(),
			},
			RuleBot: RuleBotStatus{Enabled: cfg.RuleBot.Enabled, StateFile: cfg.RuleBot.StateFile},
		},
		dirty: make(chan struct{}, 1),
	}
}

func (r *statusReporter) run(done <-chan struct{}) {
	if r == nil {
		return
	}
	r.write()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			r.refreshStorage()
			r.write()
			return
		case <-r.dirty:
		case <-ticker.C:
		}
		r.refreshStorage()
		r.write()
	}
}

func (r *statusReporter) markDirty() {
	if r == nil {
		return
	}
	select {
	case r.dirty <- struct{}{}:
	default:
	}
}

func (r *statusReporter) connected(name string) {
	if r == nil {
		return
	}
	r.mutex.Lock()
	status := r.status.Instances[name]
	status.Connected = true
	status.LastConnectedAt = time.Now().UTC()
	status.RecentError = ""
	status.RetryIn = ""
	r.status.Instances[name] = status
	r.mutex.Unlock()
	r.markDirty()
}

func (r *statusReporter) disconnected(name string, err error, retry time.Duration) {
	if r == nil {
		return
	}
	r.mutex.Lock()
	status := r.status.Instances[name]
	status.Connected = false
	status.Reconnects++
	status.RecentError = err.Error()
	status.RetryIn = retry.Round(time.Millisecond).String()
	r.status.Instances[name] = status
	r.mutex.Unlock()
	r.markDirty()
}

func (r *statusReporter) event(name string) {
	if r == nil {
		return
	}
	r.mutex.Lock()
	status := r.status.Instances[name]
	status.LastEventAt = time.Now().UTC()
	status.CapturedEvents++
	r.status.Instances[name] = status
	r.mutex.Unlock()
	r.markDirty()
}

func (r *statusReporter) acceptedDomain() {
	if r == nil {
		return
	}
	r.mutex.Lock()
	r.status.Output.Domains++
	r.mutex.Unlock()
	r.markDirty()
}

func (r *statusReporter) refreshStorage() {
	if r == nil {
		return
	}
	r.mutex.Lock()
	r.status.Output.Bytes = r.store.durable.Load()
	if r.status.RuleBot.Enabled && r.stateFile != "" {
		if state, exists, err := loadRuleBotState(r.stateFile); err == nil && exists {
			r.status.RuleBot.Offset = state.Offset
		}
	}
	r.mutex.Unlock()
}

func (r *statusReporter) write() {
	if r == nil {
		return
	}
	r.mutex.Lock()
	r.status.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(r.status)
	r.mutex.Unlock()
	if err != nil {
		return
	}
	directory := filepath.Dir(r.path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return
	}
	temporary, err := os.CreateTemp(directory, ".status-*")
	if err != nil {
		return
	}
	temporaryPath := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o640); err != nil {
		return
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		return
	}
	if err := temporary.Sync(); err != nil {
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	if err := os.Rename(temporaryPath, r.path); err != nil {
		_ = fmt.Errorf("publish status: %w", err)
		return
	}
	ok = true
}
