package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunPublishesAtomicRuntimeStatus(t *testing.T) {
	controller := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/logs":
			writeLogEvent(writer, "status.example")
			<-request.Context().Done()
		case "/connections":
			io.WriteString(writer, `{"connections":[]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer controller.Close()

	directory := t.TempDir()
	configPath := writeTestConfig(t, directory, fmt.Sprintf(`{
  "version":1,
  "output":"domains.txt",
  "status_file":"status.json",
  "flush_interval":"10ms",
  "instances":[{"name":"home","url":%q,"reconnect":{"initial_delay":"10ms","max_delay":"20ms"}}]
}`, controller.URL))
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg, log.New(io.Discard, "", 0)) }()
	waitForDomains(t, filepath.Join(directory, "domains.txt"), []string{"status.example"})

	statusPath := filepath.Join(directory, "status.json")
	deadline := time.Now().Add(3 * time.Second)
	var status RuntimeStatus
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(statusPath)
		if readErr == nil && json.Unmarshal(data, &status) == nil && status.Instances["home"].CapturedEvents > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	instance := status.Instances["home"]
	if !instance.Connected || instance.CapturedEvents == 0 || instance.LastEventAt.IsZero() {
		t.Fatalf("runtime instance status = %+v", instance)
	}
	if status.Output.Path != filepath.Join(directory, "domains.txt") || status.Output.Domains != 1 {
		t.Fatalf("runtime output status = %+v", status.Output)
	}

	cancel()
	waitForRun(t, result)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("status JSON is not atomic/valid: %v", err)
	}
}
