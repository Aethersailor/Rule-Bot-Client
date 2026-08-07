package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxProbeResponse = 2 * 1024 * 1024

type ProbeResult struct {
	OK          bool           `json:"ok"`
	Name        string         `json:"name"`
	URL         string         `json:"url"`
	Version     map[string]any `json:"version,omitempty"`
	ConfigsOK   bool           `json:"configs_ok"`
	Connections bool           `json:"connections_ok"`
	LatencyMS   int64          `json:"latency_ms"`
}

func ProbeController(ctx context.Context, cfg InstanceConfig) (ProbeResult, error) {
	instance, err := buildInstance(cfg)
	if err != nil {
		return ProbeResult{}, err
	}
	defer instance.close()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	started := time.Now()
	result := ProbeResult{Name: cfg.Name, URL: cfg.URL}
	version, err := probeJSON(ctx, instance, "/version")
	if err != nil {
		return result, fmt.Errorf("version probe: %w", err)
	}
	result.Version = version
	if _, err := probeJSON(ctx, instance, "/configs"); err != nil {
		return result, fmt.Errorf("configs probe: %w", err)
	}
	result.ConfigsOK = true
	if _, err := probeJSON(ctx, instance, "/connections"); err != nil {
		return result, fmt.Errorf("connections probe: %w", err)
	}
	result.Connections = true
	result.LatencyMS = time.Since(started).Milliseconds()
	result.OK = true
	return result, nil
}

func probeJSON(ctx context.Context, instance *controllerInstance, path string) (map[string]any, error) {
	response, err := instance.request(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &statusError{code: response.StatusCode, text: http.StatusText(response.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProbeResponse+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxProbeResponse {
		return nil, errors.New("response exceeds 2 MiB")
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, errors.New("controller response is not JSON")
	}
	return value, nil
}
