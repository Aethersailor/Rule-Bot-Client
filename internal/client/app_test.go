package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWaitsForInitiallyUnavailableControllerAndRecovers(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	configPath := writeTestConfig(t, directory, fmt.Sprintf(`{
  "version":1,
  "output":"domains.txt",
  "flush_interval":"10ms",
  "instances":[{"name":"late","url":"http://%s","reconnect":{"initial_delay":"10ms","max_delay":"20ms"}}]
}`, address))
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	logs := newRecoveryLogCapture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg, log.New(logs, "", 0)) }()
	waitForDisconnect(t, logs, result)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: controllerHandler("recovered.example")}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		<-serverDone
	})

	waitForDomains(t, filepath.Join(directory, "domains.txt"), []string{"recovered.example"})
	cancel()
	waitForRun(t, result)
	logText := logs.String()
	if !strings.Contains(logText, "instance=late disconnected") ||
		!strings.Contains(logText, "retry_in=") ||
		!strings.Contains(logText, "instance=late connected") ||
		!strings.Contains(logText, "recovered_after=") {
		t.Fatalf("recovery logs missing state details: %q", logText)
	}
}

func TestRunReconnectsAfterLogStreamEnds(t *testing.T) {
	var logRequests atomic.Int64
	var snapshotRequests atomic.Int64
	firstSnapshotDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/logs":
			switch logRequests.Add(1) {
			case 1:
				writeLogEvent(writer, "before-restart.example")
				<-firstSnapshotDone
				time.Sleep(20 * time.Millisecond)
			case 2:
				http.Error(writer, "temporarily unavailable", http.StatusServiceUnavailable)
			default:
				writeLogEvent(writer, "after-restart.example")
				<-request.Context().Done()
			}
		case "/connections":
			if snapshotRequests.Add(1) == 1 {
				io.WriteString(writer, `{"connections":[]}`)
				close(firstSnapshotDone)
			} else {
				io.WriteString(writer, `{"connections":[{"metadata":{"host":"snapshot-after-restart.example"},"rule":"Match"}]}`)
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	configPath := writeTestConfig(t, directory, fmt.Sprintf(`{
  "version":1,
  "output":"domains.txt",
  "flush_interval":"10ms",
  "instances":[{"name":"restart","url":%q,"reconnect":{"initial_delay":"10ms","max_delay":"20ms"}}]
}`, server.URL))
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg, log.New(io.Discard, "", 0)) }()

	waitForDomains(t, filepath.Join(directory, "domains.txt"), []string{
		"before-restart.example",
		"after-restart.example",
		"snapshot-after-restart.example",
	})
	cancel()
	waitForRun(t, result)
	if got := logRequests.Load(); got < 3 {
		t.Fatalf("log stream requests = %d; want at least 3", got)
	}
	if got := snapshotRequests.Load(); got < 2 {
		t.Fatalf("connection snapshots = %d; want at least 2", got)
	}
}

func TestRunIsolatesUnavailableController(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unavailableURL := "http://" + probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	healthy := httptest.NewServer(controllerHandler("healthy.example"))
	defer healthy.Close()

	directory := t.TempDir()
	configPath := writeTestConfig(t, directory, fmt.Sprintf(`{
  "version":1,
  "output":"domains.txt",
  "flush_interval":"10ms",
  "instances":[
    {"name":"offline","url":%q,"reconnect":{"initial_delay":"10ms","max_delay":"20ms"}},
    {"name":"healthy","url":%q,"reconnect":{"initial_delay":"10ms","max_delay":"20ms"}}
  ]
}`, unavailableURL, healthy.URL))
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	logs := newRecoveryLogCapture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg, log.New(logs, "", 0)) }()

	waitForDisconnect(t, logs, result)
	waitForDomains(t, filepath.Join(directory, "domains.txt"), []string{"healthy.example"})
	assertRunStillRunning(t, result, 50*time.Millisecond)
	cancel()
	waitForRun(t, result)
}

func TestControllerClientAllowsQuietLogStream(t *testing.T) {
	instance, err := buildInstance(InstanceConfig{Name: "timeout", URL: "http://127.0.0.1:9090"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.close()
	transport, ok := instance.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("controller transport type = %T", instance.client.Transport)
	}
	if got := transport.ResponseHeaderTimeout; got != 0 {
		t.Fatalf("controller response header timeout = %s; quiet Mihomo streams require no timeout", got)
	}
}

func TestRunCollectsStreamAndSnapshotAcrossInstances(t *testing.T) {
	var authorized atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") == "Bearer test-secret" {
			authorized.Add(1)
		} else {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/logs":
			writer.Header().Set("Content-Type", "application/json")
			event, _ := json.Marshal(map[string]string{
				"type":    "info",
				"payload": "[TCP] client:1234 --> Live.Example:443 match Match using DIRECT",
			})
			fmt.Fprintf(writer, "%s\n", event)
			writer.(http.Flusher).Flush()
			<-request.Context().Done()
		case "/connections":
			io.WriteString(writer, `{"connections":[{"metadata":{"host":"snapshot.example"},"rule":"Match"},{"metadata":{"host":"ignored.example"},"rule":"RuleSet"}]}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	configPath := writeTestConfig(t, directory, fmt.Sprintf(`{
  "version":1,
  "output":"domains.txt",
  "flush_interval":"10ms",
  "instances":[
    {"name":"one","url":%q,"secret":"test-secret","reconnect":{"initial_delay":"10ms","max_delay":"20ms"}},
    {"name":"two","url":%q,"secret":"test-secret","reconnect":{"initial_delay":"10ms","max_delay":"20ms"}}
  ]
}`, server.URL, server.URL))
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, cfg, log.New(io.Discard, "", 0))
	}()

	want := []string{"live.example", "snapshot.example"}
	outputPath := filepath.Join(directory, "domains.txt")
	waitForDomains(t, outputPath, want)
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not stop")
	}
	if authorized.Load() < 4 {
		t.Fatalf("authorized requests = %d", authorized.Load())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(data))
	sort.Strings(lines)
	if strings.Join(lines, ",") != strings.Join(want, ",") {
		t.Fatalf("output domains = %v", lines)
	}
}

func TestRunAggregatesBeforeOutputAndRuleBotDelivery(t *testing.T) {
	hosts := []string{
		"update.windhawk.net",
		"mods.windhawk.net",
		"yr2.c.lencr.org",
		"crt.rootg2.amazontrust.com",
		"acme-v02.api.letsencrypt.org",
		"service.example.co.uk",
		"WWW.BÜCHER.DE.",
	}
	controller := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/logs":
			for _, host := range hosts {
				event, _ := json.Marshal(map[string]string{
					"type":    "info",
					"payload": "[TCP] client:1234 --> " + host + ":443 match Match using DIRECT",
				})
				fmt.Fprintf(writer, "%s\n", event)
			}
			writer.(http.Flusher).Flush()
			<-request.Context().Done()
		case "/connections":
			io.WriteString(writer, `{"connections":[{"metadata":{"host":"cdn.windhawk.net"},"rule":"Match"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer controller.Close()

	var requestLock sync.Mutex
	var delivered []string
	ruleBot := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/hidden" || request.Header.Get("Authorization") != "Bearer token" {
			http.NotFound(writer, request)
			return
		}
		var body struct {
			Domain string `json:"domain"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		requestLock.Lock()
		delivered = append(delivered, body.Domain)
		requestLock.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		io.WriteString(writer, `{"version":1,"status":"exists_rules"}`)
	}))
	defer ruleBot.Close()

	directory := t.TempDir()
	configPath := writeTestConfig(t, directory, fmt.Sprintf(`{
  "version":1,
  "output":"domains.txt",
  "domain_mode":"registrable_domain",
  "flush_interval":"10ms",
  "instances":[{"name":"home","url":%q}],
  "rule_bot":{"enabled":true,"endpoint":%q,"token":"token","state_file":"state.json","retry":{"initial_delay":"10ms","max_delay":"20ms"}}
}`, controller.URL, ruleBot.URL+"/hidden"))
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg, log.New(io.Discard, "", 0)) }()

	want := []string{
		"amazontrust.com",
		"example.co.uk",
		"lencr.org",
		"letsencrypt.org",
		"windhawk.net",
		"xn--bcher-kva.de",
	}
	outputPath := filepath.Join(directory, "domains.txt")
	waitForDomains(t, outputPath, want)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		requestLock.Lock()
		count := len(delivered)
		requestLock.Unlock()
		if count == len(want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	requestLock.Lock()
	got := append([]string(nil), delivered...)
	requestLock.Unlock()
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Rule-Bot domains = %v; want %v", got, want)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range hosts {
		if strings.Contains(string(data), strings.ToLower(strings.TrimSuffix(raw, "."))) {
			t.Fatalf("output leaked full hostname %q: %q", raw, data)
		}
	}
}

func TestControllerRejectsRedirect(t *testing.T) {
	targetHit := atomic.Bool{}
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHit.Store(true)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
	defer redirect.Close()
	instance, err := buildInstance(InstanceConfig{Name: "redirect", URL: redirect.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.close()
	body, err := instance.openLogs(context.Background())
	if body != nil {
		body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "302") || targetHit.Load() {
		t.Fatalf("openLogs() error=%v targetHit=%v", err, targetHit.Load())
	}
}

func controllerHandler(domain string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/logs":
			writeLogEvent(writer, domain)
			<-request.Context().Done()
		case "/connections":
			io.WriteString(writer, `{"connections":[]}`)
		default:
			http.NotFound(writer, request)
		}
	})
}

func writeLogEvent(writer http.ResponseWriter, domain string) {
	event, _ := json.Marshal(map[string]string{
		"type":    "info",
		"payload": "[TCP] client:1234 --> " + domain + ":443 match Match using DIRECT",
	})
	fmt.Fprintf(writer, "%s\n", event)
	writer.(http.Flusher).Flush()
}

type recoveryLogCapture struct {
	mutex        sync.Mutex
	buffer       bytes.Buffer
	disconnected chan struct{}
	once         sync.Once
}

func newRecoveryLogCapture() *recoveryLogCapture {
	return &recoveryLogCapture{disconnected: make(chan struct{})}
}

func (c *recoveryLogCapture) Write(data []byte) (int, error) {
	c.mutex.Lock()
	written, err := c.buffer.Write(data)
	c.mutex.Unlock()
	if bytes.Contains(data, []byte(" disconnected ")) {
		c.once.Do(func() { close(c.disconnected) })
	}
	return written, err
}

func (c *recoveryLogCapture) String() string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.buffer.String()
}

func waitForDisconnect(t *testing.T, logs *recoveryLogCapture, result <-chan error) {
	t.Helper()
	select {
	case <-logs.disconnected:
	case err := <-result:
		t.Fatalf("Run() stopped before reporting the unavailable controller: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for disconnected log: %q", logs.String())
	}
}

func assertRunStillRunning(t *testing.T, result <-chan error, duration time.Duration) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("Run() stopped while a controller was unavailable: %v", err)
	case <-time.After(duration):
	}
}

func waitForRun(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not stop")
	}
}

func waitForDomains(t *testing.T, path string, want []string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			got := strings.Fields(string(data))
			sort.Strings(got)
			copyWant := append([]string(nil), want...)
			sort.Strings(copyWant)
			if strings.Join(got, ",") == strings.Join(copyWant, ",") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("timed out waiting for %v; output=%q", want, data)
}
