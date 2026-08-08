package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	rand "math/rand/v2"
	"path/filepath"
	"sync"
	"time"
)

const (
	candidateQueueSize = 1024
	writeQueueSize     = 1024
)

func CheckConfig(cfg Config) error {
	instances, err := buildInstances(cfg)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		instance.close()
	}
	if cfg.RuleBot.Enabled {
		if _, err := resolveRuleBotToken(cfg.RuleBot); err != nil {
			return fmt.Errorf("rule_bot: %w", err)
		}
		if _, err := loadRuleBotExclusions(cfg.RuleBot.Privacy); err != nil {
			return fmt.Errorf("rule_bot: %w", err)
		}
		transport, err := buildRuleBotTransport(cfg.RuleBot)
		if err != nil {
			return fmt.Errorf("rule_bot: %w", err)
		}
		transport.CloseIdleConnections()
	}
	return nil
}

func Run(ctx context.Context, cfg Config, logger *log.Logger) error {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	instances, err := buildInstances(cfg)
	if err != nil {
		return err
	}
	defer func() {
		for _, instance := range instances {
			instance.close()
		}
	}()
	for _, instance := range instances {
		if instance.config.TLS.InsecureSkipVerify {
			logger.Printf("WARN instance=%s TLS certificate verification is disabled", instance.config.Name)
		}
	}

	outputCachePath := ""
	ruleBotCachePath := ""
	if cfg.RuntimeCacheDir != "" {
		outputCachePath = filepath.Join(cfg.RuntimeCacheDir, "domains.dedupe-cache")
		ruleBotCachePath = filepath.Join(cfg.RuntimeCacheDir, "rulebot.dedupe-cache")
	}
	store, seen, err := openOutput(cfg.Output, cfg.FlushInterval.Value(), outputCachePath)
	if err != nil {
		return err
	}
	defer store.Close()
	logger.Printf("INFO output=%s domain_mode=%s existing_domains=%d", cfg.Output, cfg.DomainMode, seen.Len())
	reporter := newStatusReporter(cfg, store, seen.Len())
	statusDone := make(chan struct{})
	if reporter != nil {
		statusStopped := make(chan struct{})
		go func() {
			reporter.run(statusDone)
			close(statusStopped)
		}()
		defer func() {
			close(statusDone)
			<-statusStopped
		}()
	}

	var sender *ruleBotSender
	if cfg.RuleBot.Enabled {
		sender, err = openRuleBotSender(cfg.RuleBot, cfg.Output, store, ruleBotCachePath)
		if err != nil {
			return fmt.Errorf("initialize Rule-Bot sender: %w", err)
		}
		defer sender.Close()
		logger.Printf(
			"INFO rule_bot enabled send_existing=%t state=%s privacy_registrable=%t proxy=%s",
			cfg.RuleBot.SendExisting,
			cfg.RuleBot.StateFile,
			cfg.RuleBot.Privacy.reduceToRegistrableDomain(),
			ruleBotProxyMode(cfg.RuleBot),
		)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	candidates := make(chan string, candidateQueueSize)
	writes := make(chan string, writeQueueSize)
	writerResult := make(chan error, 1)
	writerDone := make(chan struct{})
	go func() {
		err := store.Run(writes)
		writerResult <- err
		close(writerDone)
	}()
	senderResult := make(chan error, 1)
	if sender != nil {
		go func() {
			senderResult <- sender.Run(runCtx, logger)
		}()
	}

	var processor sync.WaitGroup
	processorResult := make(chan error, 1)
	processor.Add(1)
	go func() {
		defer processor.Done()
		defer close(writes)
		var processorErr error
		defer func() { processorResult <- processorErr }()
		for domain := range candidates {
			domain, ok := projectDomain(domain, cfg.DomainMode)
			if !ok {
				continue
			}
			added, err := seen.Add(domain)
			if err != nil {
				processorErr = fmt.Errorf("index captured domain: %w", err)
				return
			}
			if !added {
				continue
			}
			if reporter != nil {
				reporter.acceptedDomain()
			}
			select {
			case writes <- domain:
			case <-writerDone:
				return
			}
		}
	}()

	var readers sync.WaitGroup
	for _, instance := range instances {
		readers.Add(1)
		go func(instance *controllerInstance) {
			defer readers.Done()
			runInstance(runCtx, instance, cfg, candidates, logger, reporter)
		}(instance)
	}

	var fatal error
	writerConsumed := false
	senderConsumed := false
	processorConsumed := false
	select {
	case <-ctx.Done():
		logger.Printf("INFO shutdown requested")
	case err := <-writerResult:
		writerConsumed = true
		fatal = err
		if err != nil {
			logger.Printf("ERROR output failure: %v", err)
		}
	case err := <-senderResult:
		senderConsumed = true
		fatal = err
		if err != nil {
			logger.Printf("ERROR Rule-Bot sender failure: %v", err)
		}
	case err := <-processorResult:
		processorConsumed = true
		fatal = err
		if err != nil {
			logger.Printf("ERROR domain index failure: %v", err)
		}
	}
	cancel()
	readers.Wait()
	close(candidates)
	processor.Wait()
	if !processorConsumed {
		processorErr := <-processorResult
		if fatal == nil {
			fatal = processorErr
		}
	}
	if !writerConsumed {
		writerErr := <-writerResult
		if fatal == nil {
			fatal = writerErr
		}
	}
	if sender != nil && !senderConsumed {
		senderErr := <-senderResult
		if fatal == nil {
			fatal = senderErr
		}
	}
	if fatal != nil {
		return fatal
	}
	logger.Printf("INFO stopped")
	return nil
}

func ruleBotProxyMode(cfg RuleBotConfig) string {
	switch {
	case cfg.ProxyURL != "":
		return "configured"
	case cfg.ProxyFromEnvironment:
		return "environment"
	default:
		return "direct"
	}
}

func runInstance(ctx context.Context, instance *controllerInstance, cfg Config, candidates chan<- string, logger *log.Logger, reporter *statusReporter) {
	delay := instance.config.Reconnect.InitialDelay.Value()
	maxDelay := instance.config.Reconnect.MaxDelay.Value()
	lastError := ""
	lastErrorLog := time.Time{}
	outageStarted := time.Time{}
	retryAttempts := 0
	snapshotSlot := make(chan struct{}, 1)
	var snapshots sync.WaitGroup
	defer snapshots.Wait()

	submit := func(domain string) bool {
		select {
		case candidates <- domain:
			if reporter != nil {
				reporter.event(instance.config.Name)
			}
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		body, err := instance.openLogs(ctx)
		if err == nil {
			if reporter != nil {
				reporter.connected(instance.config.Name)
			}
			if outageStarted.IsZero() {
				logger.Printf("INFO instance=%s connected url=%s", instance.config.Name, instance.config.URL)
			} else {
				logger.Printf(
					"INFO instance=%s connected url=%s recovered_after=%s attempts=%d",
					instance.config.Name,
					instance.config.URL,
					time.Since(outageStarted).Round(time.Millisecond),
					retryAttempts,
				)
			}
			delay = instance.config.Reconnect.InitialDelay.Value()
			lastError = ""
			lastErrorLog = time.Time{}
			outageStarted = time.Time{}
			retryAttempts = 0
			select {
			case snapshotSlot <- struct{}{}:
				snapshots.Add(1)
				go func() {
					defer snapshots.Done()
					defer func() { <-snapshotSlot }()
					snapshotCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
					defer cancel()
					if snapshotErr := instance.snapshot(snapshotCtx, cfg.IncludeSingleLabelHosts, submit); snapshotErr != nil && ctx.Err() == nil {
						logger.Printf("WARN instance=%s connections snapshot failed: %v", instance.config.Name, snapshotErr)
					}
				}()
			default:
			}

			malformedCount := 0
			err = consumeLogStream(body, cfg.IncludeFailedConnections, cfg.IncludeSingleLabelHosts, submit, func(parseErr error) {
				malformedCount++
				if malformedCount == 1 || malformedCount%1000 == 0 {
					logger.Printf("WARN instance=%s malformed_log_lines=%d last_error=%v", instance.config.Name, malformedCount, parseErr)
				}
			})
			_ = body.Close()
		}
		if ctx.Err() != nil {
			return
		}
		if outageStarted.IsZero() {
			outageStarted = time.Now()
		}
		retryAttempts++

		wait := jitter(delay)
		var httpStatus *statusError
		if errors.As(err, &httpStatus) && (httpStatus.code == 401 || httpStatus.code == 403) && wait < 30*time.Second {
			wait = 30 * time.Second
		}
		errorText := err.Error()
		if reporter != nil {
			reporter.disconnected(instance.config.Name, err, wait)
		}
		if errorText != lastError || time.Since(lastErrorLog) >= 5*time.Minute {
			logger.Printf(
				"WARN instance=%s disconnected error=%v retry_in=%s attempt=%d",
				instance.config.Name,
				err,
				wait.Round(time.Millisecond),
				retryAttempts,
			)
			lastError = errorText
			lastErrorLog = time.Now()
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		if delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}

func jitter(delay time.Duration) time.Duration {
	spread := delay / 5
	if spread <= 0 {
		return delay
	}
	return delay - spread + time.Duration(rand.Uint64N(uint64(spread*2)+1))
}
