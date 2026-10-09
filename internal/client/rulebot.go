package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	ruleBotStateVersion  = 2
	maxRuleBotPending    = 1024
	maxRuleBotStateBytes = 256 * 1024
)

var terminalRuleBotStatuses = map[string]struct{}{
	"added":           {},
	"exists_rules":    {},
	"exists_geosite":  {},
	"ignored_cn":      {},
	"rejected_policy": {},
	"invalid_domain":  {},
}

type ruleBotState struct {
	Version    int              `json:"version"`
	Offset     int64            `json:"offset"`
	Pending    []ruleBotPending `json:"pending,omitempty"`
	PauseUntil time.Time        `json:"pause_until,omitzero"`
}

// Retry entries reference the append-only output instead of duplicating domains.
type ruleBotPending struct {
	Offset      int64         `json:"offset"`
	NextAttempt time.Time     `json:"next_attempt"`
	Delay       time.Duration `json:"delay"`
}

type ruleBotResponse struct {
	Version  int    `json:"version"`
	Status   string `json:"status"`
	Deferred bool   `json:"deferred,omitempty"`
}

type ruleBotDeliveryError struct {
	statusCode int
	status     string
	auth       bool
	retryAfter time.Duration
	err        error
}

func (e *ruleBotDeliveryError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	if e.status != "" {
		return fmt.Sprintf("HTTP %d status=%s", e.statusCode, e.status)
	}
	return fmt.Sprintf("HTTP %d", e.statusCode)
}

type ruleBotSender struct {
	config     RuleBotConfig
	store      *outputStore
	file       *os.File
	client     *http.Client
	delivered  *fingerprintSet
	offset     int64
	pending    []ruleBotPending
	pauseUntil time.Time
}

func resolveRuleBotToken(cfg RuleBotConfig) (string, error) {
	if credentialSourceCount(cfg.Token, cfg.TokenFile, cfg.TokenEnv) > 1 {
		return "", errors.New("token, token_file, and token_env are mutually exclusive")
	}
	var token string
	switch {
	case cfg.TokenFile != "":
		data, err := readBoundedFile(cfg.TokenFile, "token_file", maxCredentialBytes)
		if err != nil {
			return "", fmt.Errorf("read token_file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	case cfg.TokenEnv != "":
		value, exists := os.LookupEnv(cfg.TokenEnv)
		if !exists {
			return "", fmt.Errorf("token_env %q is not set", cfg.TokenEnv)
		}
		token = value
	case cfg.Token != "":
		token = cfg.Token
	default:
		return "", errors.New("Rule-Bot token is not configured")
	}
	if token == "" {
		return "", errors.New("configured Rule-Bot token is empty")
	}
	if len(token) > maxCredentialBytes {
		return "", fmt.Errorf("Rule-Bot token exceeds %d bytes", maxCredentialBytes)
	}
	for index := range len(token) {
		if token[index] < 0x20 || token[index] == 0x7f {
			return "", errors.New("Rule-Bot token contains an HTTP control character")
		}
	}
	return token, nil
}

func openRuleBotSender(cfg RuleBotConfig, outputPath string, store *outputStore, configuredCachePath ...string) (*ruleBotSender, error) {
	if _, err := resolveRuleBotToken(cfg); err != nil {
		return nil, err
	}
	if _, err := loadRuleBotExclusions(cfg.Privacy); err != nil {
		return nil, err
	}
	file, err := os.Open(outputPath)
	if err != nil {
		return nil, fmt.Errorf("open output for Rule-Bot delivery: %w", err)
	}
	fail := func(err error) (*ruleBotSender, error) {
		_ = file.Close()
		return nil, err
	}

	state, exists, err := loadRuleBotState(cfg.StateFile)
	if err != nil {
		return fail(err)
	}
	durable := store.DurableSize()
	if exists && state.Offset > durable {
		return fail(fmt.Errorf("Rule-Bot state offset %d exceeds output size %d", state.Offset, durable))
	}
	if exists && state.Offset > 0 {
		var previous [1]byte
		if _, err := file.ReadAt(previous[:], state.Offset-1); err != nil {
			return fail(fmt.Errorf("validate Rule-Bot state offset: %w", err))
		}
		if previous[0] != '\n' {
			return fail(errors.New("Rule-Bot state offset is not on a line boundary"))
		}
	}
	if !exists {
		state = ruleBotState{Version: ruleBotStateVersion}
		if !cfg.SendExisting {
			state.Offset = durable
		}
		if err := writeRuleBotState(cfg.StateFile, state); err != nil {
			return fail(err)
		}
	}
	for _, pending := range state.Pending {
		if pending.Offset > 0 {
			var previous [1]byte
			if _, err := file.ReadAt(previous[:], pending.Offset-1); err != nil || previous[0] != '\n' {
				return fail(errors.New("Rule-Bot pending offset is not on a line boundary"))
			}
		}
		if _, _, ready, err := readDurableDomain(file, pending.Offset, state.Offset); err != nil || !ready {
			return fail(errors.New("Rule-Bot pending offset does not reference a complete domain"))
		}
	}
	cachePath := cfg.StateFile + ".dedupe-cache"
	if len(configuredCachePath) != 0 && configuredCachePath[0] != "" {
		cachePath = configuredCachePath[0]
	}
	delivered, err := loadDeliveredRuleBotDomains(file, state.Offset, cfg, cachePath)
	if err != nil {
		return fail(err)
	}

	transport, err := buildRuleBotTransport(cfg)
	if err != nil {
		_ = delivered.Close()
		return fail(err)
	}
	return &ruleBotSender{
		config:    cfg,
		store:     store,
		file:      file,
		delivered: delivered,
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		offset:     state.Offset,
		pending:    state.Pending,
		pauseUntil: state.PauseUntil,
	}, nil
}

func buildRuleBotTransport(cfg RuleBotConfig) (*http.Transport, error) {
	var proxy func(*http.Request) (*url.URL, error)
	switch {
	case cfg.ProxyURL != "":
		parsed, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, errors.New("proxy_url is not a valid URL")
		}
		proxy = http.ProxyURL(parsed)
	case cfg.ProxyFromEnvironment:
		proxy = http.ProxyFromEnvironment
	}
	return &http.Transport{
		Proxy: proxy,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}, nil
}

func loadDeliveredRuleBotDomains(file *os.File, offset int64, cfg RuleBotConfig, cachePath string) (*fingerprintSet, error) {
	delivered := newFingerprintSetAt(1024, cachePath)
	fail := func(err error) (*fingerprintSet, error) {
		_ = delivered.Close()
		return nil, err
	}
	historical := cfg
	historical.Privacy.ExcludeSuffixes = nil
	historical.Privacy.ExcludeFile = ""
	for current := int64(0); current < offset; {
		raw, next, ready, err := readDurableDomain(file, current, offset)
		if err != nil {
			return fail(fmt.Errorf("load delivered Rule-Bot domains: %w", err))
		}
		if !ready {
			return fail(errors.New("load delivered Rule-Bot domains: incomplete state boundary"))
		}
		domain, _, err := prepareRuleBotDomain(raw, historical)
		if err != nil {
			return fail(err)
		}
		if domain != "" {
			if _, err := delivered.Add(domain); err != nil {
				return fail(fmt.Errorf("index delivered Rule-Bot domain: %w", err))
			}
		}
		current = next
	}
	return delivered, nil
}

func (s *ruleBotSender) Close() error {
	if transport, ok := s.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return errors.Join(s.file.Close(), s.delivered.Close())
}

func (s *ruleBotSender) Run(ctx context.Context, logger *log.Logger) error {
	retryTurn := true
	for {
		if ctx.Err() != nil {
			return nil
		}
		now := time.Now()
		if now.Before(s.pauseUntil) {
			if !waitForRuleBot(ctx, min(250*time.Millisecond, s.pauseUntil.Sub(now))) {
				return nil
			}
			continue
		}
		durable := s.store.DurableSize()
		fresh := s.offset < durable && len(s.pending) < maxRuleBotPending
		retryIndex := -1
		wait := 250 * time.Millisecond
		for index, pending := range s.pending {
			if !pending.NextAttempt.After(now) {
				if retryIndex < 0 || pending.NextAttempt.Before(s.pending[retryIndex].NextAttempt) {
					retryIndex = index
				}
			} else {
				wait = min(wait, pending.NextAttempt.Sub(now))
			}
		}
		if !fresh && retryIndex < 0 {
			if !waitForRuleBot(ctx, wait) {
				return nil
			}
			continue
		}
		retrying := retryIndex >= 0 && (!fresh || retryTurn)
		offset := s.offset
		if retrying {
			offset = s.pending[retryIndex].Offset
		}
		rawDomain, nextOffset, ready, err := readDurableDomain(s.file, offset, durable)
		if err != nil {
			return err
		}
		if !ready {
			if !waitForRuleBot(ctx, 100*time.Millisecond) {
				return nil
			}
			continue
		}
		domain, localStatus, err := prepareRuleBotDomain(rawDomain, s.config)
		if err != nil {
			return fmt.Errorf("apply Rule-Bot privacy policy: %w", err)
		}
		if domain == "" {
			logger.Printf("INFO rule_bot domain_ref=%s local_status=%s", domainReference(rawDomain), localStatus)
		} else if !retrying {
			added, err := s.delivered.Add(domain)
			if err != nil {
				return fmt.Errorf("index delivered Rule-Bot domain: %w", err)
			}
			if !added {
				logger.Printf("INFO rule_bot domain_ref=%s local_status=duplicate_registrable_domain", domainReference(domain))
				domain = ""
			}
		}
		var deliveryErr error
		if domain != "" {
			_, deliveryErr = s.attempt(ctx, logger, domain)
		}
		if ctx.Err() != nil {
			return nil
		}
		if deliveryErr != nil {
			delay := s.config.Retry.InitialDelay.Value()
			if retrying {
				delay = s.pending[retryIndex].Delay * 2
			}
			if delay <= 0 {
				delay = 2 * time.Second
			}
			maxDelay := s.config.Retry.MaxDelay.Value()
			if maxDelay <= 0 {
				maxDelay = 5 * time.Minute
			}
			maxDelay = min(maxDelay, 24*time.Hour)
			delay = min(delay, maxDelay)
			retryIn := jitter(delay)
			var failure *ruleBotDeliveryError
			if errors.As(deliveryErr, &failure) {
				retryIn = max(retryIn, failure.retryAfter)
				// Endpoint failures pause the sender instead of spending its budget
				// on every queued domain. An application 503 is deferred individually.
				if failure.auth || failure.statusCode == http.StatusTooManyRequests || failure.status != "temporary_error" {
					if failure.auth || failure.statusCode == http.StatusTooManyRequests {
						retryIn = max(retryIn, 30*time.Second)
					}
					s.pauseUntil = time.Now().Add(retryIn)
				}
			}
			pending := ruleBotPending{Offset: offset, NextAttempt: time.Now().UTC().Add(retryIn), Delay: delay}
			if retrying {
				s.pending[retryIndex] = pending
			} else {
				s.pending = append(s.pending, pending)
			}
			logger.Printf("INFO rule_bot domain_ref=%s deferred=true retry_in=%s pending=%d", domainReference(domain), retryIn.Round(time.Millisecond), len(s.pending))
		} else if retrying {
			s.pending = append(s.pending[:retryIndex], s.pending[retryIndex+1:]...)
		}
		if retrying {
			nextOffset = s.offset
		}
		state := ruleBotState{Version: ruleBotStateVersion, Offset: nextOffset, Pending: s.pending, PauseUntil: s.pauseUntil}
		if err := writeRuleBotState(s.config.StateFile, state); err != nil {
			return err
		}
		s.offset = nextOffset
		retryTurn = !retrying
	}
}

func (s *ruleBotSender) attempt(ctx context.Context, logger *log.Logger, domain string) (string, error) {
	status, err := s.deliver(ctx, domain)
	if ctx.Err() != nil {
		return status, err
	}
	if err != nil {
		logger.Printf("WARN rule_bot domain_ref=%s delivery_failed=%s", domainReference(domain), ruleBotDeliveryLogReason(err))
	} else {
		logger.Printf("INFO rule_bot domain_ref=%s status=%s", domainReference(domain), status)
	}
	return status, err
}

func ruleBotDeliveryLogReason(err error) string {
	var deliveryError *ruleBotDeliveryError
	if !errors.As(err, &deliveryError) {
		return "delivery_error"
	}
	if deliveryError.auth {
		if deliveryError.statusCode == http.StatusUnauthorized || deliveryError.statusCode == http.StatusForbidden {
			return "authentication_failed"
		}
		return "credential_error"
	}
	if deliveryError.statusCode != 0 {
		return fmt.Sprintf("http_status_%d", deliveryError.statusCode)
	}
	var urlError *url.Error
	if errors.As(deliveryError.err, &urlError) {
		if urlError.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}
	var networkError net.Error
	if errors.As(deliveryError.err, &networkError) {
		if networkError.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}
	return "response_error"
}

func (s *ruleBotSender) deliver(ctx context.Context, domain string) (string, error) {
	token, err := resolveRuleBotToken(s.config)
	if err != nil {
		return "", &ruleBotDeliveryError{auth: true, err: err}
	}
	body, err := json.Marshal(map[string]any{"version": 1, "domain": domain})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Rule-Bot-Client/1")
	response, err := s.client.Do(request)
	if err != nil {
		return "", &ruleBotDeliveryError{err: err}
	}
	defer response.Body.Close()
	failure := &ruleBotDeliveryError{
		statusCode: response.StatusCode,
		retryAfter: ruleBotRetryAfter(response.Header.Get("Retry-After"), time.Now()),
		auth:       response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden,
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		failure.err = fmt.Errorf("read response: %w", err)
		return "", failure
	}
	if len(data) > 4096 {
		failure.err = errors.New("response exceeds 4096 bytes")
		return "", failure
	}
	var result ruleBotResponse
	if err := json.Unmarshal(data, &result); err != nil {
		failure.err = errors.New("invalid JSON response")
		return "", failure
	}
	failure.status = result.Status
	if result.Version != 1 {
		failure.err = errors.New("unsupported response version")
		return "", failure
	}
	if _, terminal := terminalRuleBotStatuses[result.Status]; terminal && ruleBotStatusCodeMatches(result.Status, response.StatusCode) {
		if result.Status == "rejected_policy" && result.Deferred {
			return "deferred_dns", nil
		}
		return result.Status, nil
	}
	return "", failure
}

func ruleBotRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(min(seconds, int64(86400))) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return min(max(date.Sub(now), 0), 24*time.Hour)
	}
	return 0
}

func ruleBotStatusCodeMatches(status string, statusCode int) bool {
	switch status {
	case "added":
		return statusCode == http.StatusCreated
	case "invalid_domain":
		return statusCode == http.StatusBadRequest
	default:
		return statusCode == http.StatusOK
	}
}

func readDurableDomain(file *os.File, offset, durable int64) (string, int64, bool, error) {
	if durable <= offset {
		return "", offset, false, nil
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(file, offset, durable-offset), 512)
	line, err := reader.ReadString('\n')
	if errors.Is(err, io.EOF) {
		return "", offset, false, nil
	}
	if err != nil {
		return "", offset, false, fmt.Errorf("read durable output: %w", err)
	}
	domain, ok := normalizeDomain(strings.TrimSuffix(line, "\n"), true)
	if !ok {
		return "", offset, false, errors.New("durable output contains an invalid domain")
	}
	return domain, offset + int64(len(line)), true, nil
}

func loadRuleBotState(path string) (ruleBotState, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return ruleBotState{}, false, nil
	}
	if err != nil {
		return ruleBotState{}, false, fmt.Errorf("open Rule-Bot state: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRuleBotStateBytes+1))
	if err != nil {
		return ruleBotState{}, false, fmt.Errorf("read Rule-Bot state: %w", err)
	}
	if len(data) > maxRuleBotStateBytes {
		return ruleBotState{}, false, errors.New("Rule-Bot state exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state ruleBotState
	if err := decoder.Decode(&state); err != nil {
		return ruleBotState{}, false, fmt.Errorf("decode Rule-Bot state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ruleBotState{}, false, errors.New("decode Rule-Bot state: trailing data")
	}
	if (state.Version != 1 && state.Version != ruleBotStateVersion) || state.Offset < 0 || len(state.Pending) > maxRuleBotPending || (state.Version == 1 && len(state.Pending) != 0) {
		return ruleBotState{}, false, errors.New("invalid Rule-Bot state")
	}
	seen := make(map[int64]bool, len(state.Pending))
	for _, pending := range state.Pending {
		if pending.Offset < 0 || pending.Offset >= state.Offset || pending.Delay <= 0 || pending.Delay > 24*time.Hour || pending.NextAttempt.IsZero() || seen[pending.Offset] {
			return ruleBotState{}, false, errors.New("invalid Rule-Bot pending retry")
		}
		seen[pending.Offset] = true
	}
	return state, true, nil
}

func writeRuleBotState(path string, state ruleBotState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create Rule-Bot state directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".rule-bot-client-rulebot-*")
	if err != nil {
		return fmt.Errorf("create Rule-Bot state: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure Rule-Bot state: %w", err)
	}
	if err := json.NewEncoder(temporary).Encode(state); err != nil {
		return fmt.Errorf("encode Rule-Bot state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("synchronize Rule-Bot state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Rule-Bot state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace Rule-Bot state: %w", err)
	}
	removeTemporary = false
	if runtime.GOOS != "windows" {
		directory, err := os.Open(filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("open Rule-Bot state directory: %w", err)
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if err := errors.Join(syncErr, closeErr); err != nil {
			return fmt.Errorf("synchronize Rule-Bot state directory: %w", err)
		}
	}
	return nil
}

func waitForRuleBot(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
