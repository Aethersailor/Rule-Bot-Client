package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuleBotSenderReducesAndDeduplicatesBeforeNetwork(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		var body struct {
			Domain string `json:"domain"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Domain != "example.com" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		io.WriteString(writer, `{"version":1,"status":"exists_rules"}`)
	}))
	defer server.Close()

	directory := t.TempDir()
	outputPath := filepath.Join(directory, "domains.txt")
	statePath := filepath.Join(directory, "state.json")
	store, _, err := openOutput(outputPath, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sender, err := openRuleBotSender(RuleBotConfig{
		Enabled:   true,
		Endpoint:  server.URL + "/hidden",
		Token:     "token",
		StateFile: statePath,
	}, outputPath, store)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var logs lockedBuffer
	result := make(chan error, 1)
	go func() { result <- sender.Run(ctx, log.New(&logs, "", 0)) }()
	writes := make(chan string, 2)
	writes <- "account.api.example.com"
	writes <- "cdn.example.com"
	close(writes)
	if err := store.Run(writes); err != nil {
		t.Fatal(err)
	}
	wantOffset := int64(len("account.api.example.com\ncdn.example.com\n"))
	waitForRuleBotOffset(t, statePath, wantOffset)
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d", requests.Load())
	}
	if strings.Contains(logs.String(), "example.com") || !strings.Contains(logs.String(), "domain_ref=") {
		t.Fatalf("privacy-safe logs = %q", logs.String())
	}
}

func TestRuleBotSenderUsesConfiguredHTTPProxy(t *testing.T) {
	var requests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Host != "rule-bot.invalid" {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		io.WriteString(writer, `{"version":1,"status":"exists_rules"}`)
	}))
	defer proxy.Close()

	directory := t.TempDir()
	outputPath := filepath.Join(directory, "domains.txt")
	store, _, err := openOutput(outputPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sender, err := openRuleBotSender(RuleBotConfig{
		Enabled:   true,
		Endpoint:  "http://rule-bot.invalid/hidden",
		Token:     "token",
		ProxyURL:  proxy.URL,
		StateFile: filepath.Join(directory, "state.json"),
	}, outputPath, store)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	status, err := sender.deliver(context.Background(), "example.com")
	if err != nil || status != "exists_rules" || requests.Load() != 1 {
		t.Fatalf("proxy delivery status=%q requests=%d error=%v", status, requests.Load(), err)
	}
}

func TestRuleBotSenderRetriesAndPersistsOffset(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/hidden/submission/path" {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodPost ||
			request.Header.Get("Authorization") != "Bearer test-token" ||
			request.Header.Get("User-Agent") != "Rule-Bot-Client/1" ||
			request.Header.Get("Content-Type") != "application/json" ||
			request.Header.Get("Accept") != "application/json" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil ||
			len(body) != 2 || body["version"] != float64(1) || body["domain"] != "example.com" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if requests.Add(1) == 1 {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(writer, `{"version":1,"status":"temporary_error"}`)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		io.WriteString(writer, `{"version":1,"status":"added"}`)
	}))
	defer server.Close()

	directory := t.TempDir()
	outputPath := filepath.Join(directory, "domains.txt")
	statePath := filepath.Join(directory, "rulebot-state.json")
	store, _, err := openOutput(outputPath, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := RuleBotConfig{
		Enabled:   true,
		Endpoint:  server.URL + "/hidden/submission/path",
		Token:     "test-token",
		StateFile: statePath,
		Retry: ReconnectConfig{
			InitialDelay: Duration(time.Millisecond),
			MaxDelay:     Duration(5 * time.Millisecond),
		},
	}
	sender, err := openRuleBotSender(config, outputPath, store)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	ctx, cancel := context.WithCancel(context.Background())
	senderResult := make(chan error, 1)
	go func() { senderResult <- sender.Run(ctx, log.New(io.Discard, "", 0)) }()
	writes := make(chan string, 1)
	writes <- "example.com"
	close(writes)
	writerResult := make(chan error, 1)
	go func() { writerResult <- store.Run(writes) }()
	if err := <-writerResult; err != nil {
		t.Fatal(err)
	}

	waitForRuleBotOffset(t, statePath, int64(len("example.com\n")))
	cancel()
	if err := <-senderResult; err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}

	second, err := openRuleBotSender(config, outputPath, store)
	if err != nil {
		t.Fatal(err)
	}
	secondCtx, secondCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer secondCancel()
	if err := second.Run(secondCtx, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	second.Close()
	if requests.Load() != 2 {
		t.Fatalf("domain was resent after persisted offset; requests = %d", requests.Load())
	}
}

func TestRuleBotTerminalStatusContract(t *testing.T) {
	want := map[string]int{
		"added":           http.StatusCreated,
		"exists_rules":    http.StatusOK,
		"exists_geosite":  http.StatusOK,
		"ignored_cn":      http.StatusOK,
		"rejected_policy": http.StatusOK,
		"invalid_domain":  http.StatusBadRequest,
	}
	if len(terminalRuleBotStatuses) != len(want) {
		t.Fatalf("terminal status count = %d", len(terminalRuleBotStatuses))
	}
	for status, code := range want {
		if _, exists := terminalRuleBotStatuses[status]; !exists {
			t.Errorf("terminal status %q is missing", status)
		}
		if !ruleBotStatusCodeMatches(status, code) {
			t.Errorf("terminal status %q rejected HTTP %d", status, code)
		}
	}
}

func TestResolveRuleBotTokenSupportsEveryCredentialSource(t *testing.T) {
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "rulebot.token")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const environmentVariable = "RULE_BOT_CLIENT_TEST_TOKEN_SOURCE"
	t.Setenv(environmentVariable, "environment-token")

	tests := []struct {
		name string
		cfg  RuleBotConfig
		want string
	}{
		{name: "inline", cfg: RuleBotConfig{Token: "inline-token"}, want: "inline-token"},
		{name: "file", cfg: RuleBotConfig{TokenFile: tokenPath}, want: "file-token"},
		{name: "environment", cfg: RuleBotConfig{TokenEnv: environmentVariable}, want: "environment-token"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveRuleBotToken(test.cfg)
			if err != nil {
				t.Fatalf("resolveRuleBotToken() error = %v", err)
			}
			if got != test.want {
				t.Fatal("resolveRuleBotToken() selected the wrong credential source")
			}
		})
	}
}

func TestResolveRuleBotTokenRequiresCredentialSource(t *testing.T) {
	_, err := resolveRuleBotToken(RuleBotConfig{})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("resolveRuleBotToken() error = %v", err)
	}
}

func TestResolveRuleBotTokenRejectsEveryCredentialSourceConflict(t *testing.T) {
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "rulebot.token")
	const fileToken = "file-token-marker"
	if err := os.WriteFile(tokenPath, []byte(fileToken), 0o600); err != nil {
		t.Fatal(err)
	}
	const environmentVariable = "RULE_BOT_CLIENT_TEST_TOKEN_CONFLICT"
	const environmentToken = "environment-token-marker"
	t.Setenv(environmentVariable, environmentToken)
	const inlineToken = "inline-token-marker"

	tests := map[string]RuleBotConfig{
		"inline and file":        {Token: inlineToken, TokenFile: tokenPath},
		"inline and environment": {Token: inlineToken, TokenEnv: environmentVariable},
		"file and environment":   {TokenFile: tokenPath, TokenEnv: environmentVariable},
		"all sources":            {Token: inlineToken, TokenFile: tokenPath, TokenEnv: environmentVariable},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := resolveRuleBotToken(cfg)
			if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("resolveRuleBotToken() error = %v", err)
			}
			for _, credential := range []string{inlineToken, fileToken, environmentToken} {
				if strings.Contains(err.Error(), credential) {
					t.Fatal("resolveRuleBotToken() error exposed a credential value")
				}
			}
		})
	}
}

func TestResolveRuleBotTokenErrorsDoNotExposeCredentialValues(t *testing.T) {
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "rulebot.token")
	const credentialMarker = "rule-bot-token-marker"
	if err := os.WriteFile(tokenPath, []byte(credentialMarker+"\ninvalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	const environmentVariable = "RULE_BOT_CLIENT_TEST_UNSAFE_TOKEN"
	t.Setenv(environmentVariable, credentialMarker+"\ninvalid")

	tests := map[string]RuleBotConfig{
		"inline":      {Token: credentialMarker + "\ninvalid"},
		"file":        {TokenFile: tokenPath},
		"environment": {TokenEnv: environmentVariable},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := resolveRuleBotToken(cfg)
			if err == nil {
				t.Fatal("resolveRuleBotToken() succeeded")
			}
			if strings.Contains(err.Error(), credentialMarker) {
				t.Fatal("resolveRuleBotToken() error exposed a credential value")
			}
		})
	}
}

func TestRuleBotSenderReadsRotatedTokenFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Header.Get("Authorization") != "Bearer new-token" {
			writer.WriteHeader(http.StatusUnauthorized)
			io.WriteString(writer, `{"version":1,"status":"unauthorized"}`)
			return
		}
		io.WriteString(writer, `{"version":1,"status":"exists_rules"}`)
	}))
	defer server.Close()

	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "rulebot.token")
	if err := os.WriteFile(tokenPath, []byte("old-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(directory, "domains.txt")
	store, _, err := openOutput(outputPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sender, err := openRuleBotSender(RuleBotConfig{
		Enabled:   true,
		Endpoint:  server.URL + "/hidden/path",
		TokenFile: tokenPath,
		StateFile: filepath.Join(directory, "state.json"),
	}, outputPath, store)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	if _, err := sender.deliver(context.Background(), "example.com"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("first deliver error = %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("new-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := sender.deliver(context.Background(), "example.com")
	if err != nil || status != "exists_rules" {
		t.Fatalf("second deliver status=%q error=%v", status, err)
	}
}

func TestRuleBotSenderSkipsExistingOutputByDefault(t *testing.T) {
	directory := t.TempDir()
	outputPath := filepath.Join(directory, "domains.txt")
	if err := os.WriteFile(outputPath, []byte("old.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _, err := openOutput(outputPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sender, err := openRuleBotSender(RuleBotConfig{
		Enabled:   true,
		Endpoint:  "https://rule-bot.example/hidden/path",
		Token:     "token",
		StateFile: filepath.Join(directory, "state.json"),
	}, outputPath, store)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	if sender.offset != int64(len("old.example\n")) {
		t.Fatalf("offset = %d", sender.offset)
	}
}

func TestRuleBotDeliveryLogDoesNotExposeEndpointOrToken(t *testing.T) {
	const endpoint = "https://private-rule-bot.example/api/private/hidden-path"
	const token = "private-rule-bot-token-marker"
	sender := &ruleBotSender{
		config: RuleBotConfig{
			Endpoint: endpoint,
			Token:    token,
			Retry: ReconnectConfig{
				InitialDelay: Duration(10 * time.Millisecond),
				MaxDelay:     Duration(20 * time.Millisecond),
			},
		},
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		})},
	}

	ctx, cancel := context.WithCancel(context.Background())
	var logs lockedBuffer
	result := make(chan error, 1)
	go func() {
		result <- sender.deliverUntilTerminal(ctx, log.New(&logs, "", 0), "example.com")
	}()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(logs.String(), "delivery_failed=") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "private-rule-bot.example") || strings.Contains(logs.String(), "/api/private/hidden-path") || strings.Contains(logs.String(), token) || !strings.Contains(logs.String(), "delivery_failed=network_error") {
		t.Fatalf("delivery log exposed endpoint: %q", logs.String())
	}
}

func TestRuleBotCredentialErrorLogDoesNotExposeToken(t *testing.T) {
	const credentialMarker = "private-rule-bot-token-marker"
	var requests atomic.Int64
	sender := &ruleBotSender{
		config: RuleBotConfig{
			Endpoint: "https://private-rule-bot.example/api/private/hidden-path",
			Token:    credentialMarker + "\ninvalid",
			Retry: ReconnectConfig{
				InitialDelay: Duration(10 * time.Millisecond),
				MaxDelay:     Duration(20 * time.Millisecond),
			},
		},
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, errors.New("unexpected request")
		})},
	}

	ctx, cancel := context.WithCancel(context.Background())
	var logs lockedBuffer
	result := make(chan error, 1)
	go func() {
		result <- sender.deliverUntilTerminal(ctx, log.New(&logs, "", 0), "example.com")
	}()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(logs.String(), "delivery_failed=") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("invalid token reached the HTTP transport")
	}
	if strings.Contains(logs.String(), credentialMarker) || strings.Contains(logs.String(), "private-rule-bot.example") || !strings.Contains(logs.String(), "delivery_failed=credential_error") {
		t.Fatalf("credential error log exposed sensitive configuration: %q", logs.String())
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.Buffer.Write(data)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.Buffer.String()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func waitForRuleBotOffset(t *testing.T, path string, want int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, exists, err := loadRuleBotState(path)
		if err == nil && exists && state.Offset == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, exists, err := loadRuleBotState(path)
	t.Fatalf("state offset did not reach %d: state=%+v exists=%t err=%v", want, state, exists, err)
}
