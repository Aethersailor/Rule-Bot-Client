package client

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuleBotDeferredFailureDoesNotBlockAndSurvivesRestart(t *testing.T) {
	var recovered atomic.Bool
	var good atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Domain string `json:"domain"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body.Domain == "broken.example" && !recovered.Load() {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(503)
			io.WriteString(w, `{"version":1,"status":"temporary_error"}`)
		} else {
			good.Add(1)
			io.WriteString(w, `{"version":1,"status":"exists_rules"}`)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	output := filepath.Join(dir, "domains.txt")
	statePath := filepath.Join(dir, "state.json")
	const data = "broken.example\ngood.example\n"
	os.WriteFile(output, []byte(data), 0600)
	// Existing version-1 checkpoints must retain all unsent records.
	os.WriteFile(statePath, []byte(`{"version":1,"offset":0}`), 0600)
	store, _, err := openOutput(output, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := RuleBotConfig{Enabled: true, Endpoint: server.URL, Token: "token", StateFile: statePath}
	sender, err := openRuleBotSender(cfg, output, store)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- sender.Run(ctx, log.New(io.Discard, "", 0)) }()
	deadline := time.Now().Add(3 * time.Second)
	var state ruleBotState
	for time.Now().Before(deadline) {
		state, _, _ = loadRuleBotState(statePath)
		if state.Offset == int64(len(data)) && len(state.Pending) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	sender.Close()
	if state.Offset != int64(len(data)) || len(state.Pending) != 1 || state.Pending[0].Offset != 0 || good.Load() != 1 {
		t.Fatalf("failure blocked following domain: state=%+v good=%d", state, good.Load())
	}
	if time.Until(state.Pending[0].NextAttempt) < 59*time.Minute {
		t.Fatal("Retry-After was ignored")
	}
	encoded, _ := os.ReadFile(statePath)
	for _, domain := range []string{"broken.example", "good.example"} {
		if strings.Contains(string(encoded), domain) {
			t.Fatal("retry state contains a raw domain")
		}
	}
	recovered.Store(true)
	state.Pending[0].NextAttempt = time.Now().Add(-time.Second)
	if err := writeRuleBotState(statePath, state); err != nil {
		t.Fatal(err)
	}
	second, err := openRuleBotSender(cfg, output, store)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	ctx, cancel = context.WithCancel(context.Background())
	go func() { result <- second.Run(ctx, log.New(io.Discard, "", 0)) }()
	waitForRuleBotOffset(t, statePath, int64(len(data)))
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if good.Load() != 2 {
		t.Fatalf("terminal domain replayed or deferred domain lost: %d", good.Load())
	}
}

func TestRuleBotThrottlePauseSurvivesRestart(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
		io.WriteString(w, `{"version":1,"status":"rate_limited"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	output := filepath.Join(dir, "domains.txt")
	statePath := filepath.Join(dir, "state.json")
	os.WriteFile(output, []byte("one.example\ntwo.example\n"), 0600)
	os.WriteFile(statePath, []byte(`{"version":1,"offset":0}`), 0600)
	store, _, err := openOutput(output, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := RuleBotConfig{Endpoint: server.URL, Token: "token", StateFile: statePath}
	for range 2 {
		sender, err := openRuleBotSender(cfg, output, store)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		err = sender.Run(ctx, log.New(io.Discard, "", 0))
		cancel()
		sender.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("endpoint throttle was bypassed: %d", requests.Load())
	}
}

func TestRuleBotRetryStateRejectsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	for _, state := range []ruleBotState{
		{Version: 2, Offset: 5, Pending: []ruleBotPending{{Offset: 5, Delay: time.Second, NextAttempt: time.Now()}}},
		{Version: 2, Offset: 5, Pending: []ruleBotPending{{Offset: 0, Delay: 0, NextAttempt: time.Now()}}},
		{Version: 1, Offset: 5, Pending: []ruleBotPending{{Offset: 0, Delay: time.Second, NextAttempt: time.Now()}}},
	} {
		if err := writeRuleBotState(path, state); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadRuleBotState(path); err == nil {
			t.Fatalf("accepted corrupt state %+v", state)
		}
	}
}

func TestRuleBotServerOwnedDeferralIsTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"version":1,"status":"rejected_policy","deferred":true}`)
	}))
	defer server.Close()
	sender := ruleBotSender{config: RuleBotConfig{Endpoint: server.URL, Token: "token"}, client: server.Client()}
	status, err := sender.deliver(context.Background(), "example.com")
	if err != nil || status != "deferred_dns" {
		t.Fatalf("server deferral status=%q err=%v", status, err)
	}
}
