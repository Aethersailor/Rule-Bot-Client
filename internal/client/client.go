package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const maxConnectionsSnapshot = 16 * 1024 * 1024

type controllerInstance struct {
	config InstanceConfig
	base   *url.URL
	secret string
	client *http.Client
}

type statusError struct {
	code int
	text string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("controller returned HTTP %d %s", e.code, e.text)
}

func buildInstances(cfg Config) ([]*controllerInstance, error) {
	instances := make([]*controllerInstance, 0, len(cfg.Instances))
	for index := range cfg.Instances {
		instance, err := buildInstance(cfg.Instances[index])
		if err != nil {
			for _, built := range instances {
				built.close()
			}
			return nil, fmt.Errorf("instance %q: %w", cfg.Instances[index].Name, err)
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func buildInstance(cfg InstanceConfig) (*controllerInstance, error) {
	base, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	secret, err := resolveSecret(cfg)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := buildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   2,
		MaxConnsPerHost:       2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 0,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
		TLSClientConfig:       tlsConfig,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &controllerInstance{config: cfg, base: base, secret: secret, client: client}, nil
}

func resolveSecret(cfg InstanceConfig) (string, error) {
	var secret string
	configured := true
	switch {
	case cfg.SecretFile != "":
		data, err := os.ReadFile(cfg.SecretFile)
		if err != nil {
			return "", fmt.Errorf("read secret_file: %w", err)
		}
		secret = strings.TrimSpace(string(data))
	case cfg.SecretEnv != "":
		value, exists := os.LookupEnv(cfg.SecretEnv)
		if !exists {
			return "", fmt.Errorf("secret_env %q is not set", cfg.SecretEnv)
		}
		secret = value
	case cfg.Secret != "":
		secret = cfg.Secret
	default:
		configured = false
	}
	if configured && secret == "" {
		return "", errors.New("configured controller secret is empty")
	}
	for index := 0; index < len(secret); index++ {
		if secret[index] < 0x20 || secret[index] == 0x7f {
			return "", errors.New("controller secret contains an HTTP control character")
		}
	}
	return secret, nil
}

func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec -- explicit opt-in with startup warning
	}
	if cfg.CAFile == "" {
		return tlsConfig, nil
	}
	pem, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read TLS ca_file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("TLS ca_file contains no valid certificates")
	}
	tlsConfig.RootCAs = pool
	return tlsConfig, nil
}

func (c *controllerInstance) endpoint(path string, query url.Values) string {
	endpoint := *c.base
	endpoint.Path = path
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

func (c *controllerInstance) request(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(path, query), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if c.secret != "" {
		request.Header.Set("Authorization", "Bearer "+c.secret)
	}
	return c.client.Do(request)
}

func (c *controllerInstance) openLogs(ctx context.Context) (io.ReadCloser, error) {
	response, err := c.request(ctx, "/logs", url.Values{"level": []string{"info"}})
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, &statusError{code: response.StatusCode, text: http.StatusText(response.StatusCode)}
	}
	return response.Body, nil
}

func (c *controllerInstance) snapshot(ctx context.Context, includeSingleLabel bool, submit func(string) bool) error {
	response, err := c.request(ctx, "/connections", nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &statusError{code: response.StatusCode, text: http.StatusText(response.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxConnectionsSnapshot+1))
	if err != nil {
		return fmt.Errorf("read connections snapshot: %w", err)
	}
	if len(data) > maxConnectionsSnapshot {
		return errors.New("connections snapshot exceeds 16 MiB")
	}
	var snapshot struct {
		Connections []struct {
			Metadata struct {
				Host string `json:"host"`
			} `json:"metadata"`
			Rule string `json:"rule"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode connections snapshot: %w", err)
	}
	for _, connection := range snapshot.Connections {
		if connection.Rule != "Match" {
			continue
		}
		domain, ok := normalizeDomain(connection.Metadata.Host, includeSingleLabel)
		if ok && !submit(domain) {
			return nil
		}
	}
	return nil
}

func (c *controllerInstance) close() {
	if transport, ok := c.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}
