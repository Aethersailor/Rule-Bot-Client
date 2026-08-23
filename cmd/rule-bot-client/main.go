package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/Aethersailor/Rule-Bot-Client/internal/client"
)

func main() {
	os.Exit(run())
}

func run() int {
	client.CleanupClientUpdateTemp()
	configPath := flag.String("config", defaultConfigPath(), "path to the JSON configuration file")
	check := flag.Bool("check", false, "validate configuration and referenced credential/TLS files, then exit")
	version := flag.Bool("version", false, "print version information and exit")
	updateCheck := flag.Bool("update-check", false, "check the latest stable Release and exit")
	updateApply := flag.Bool("update-apply", false, "install the latest stable Release and exit")
	updateHelper := flag.String("update-helper", "", "internal update replacement plan")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "rule-bot-client does not accept positional arguments")
		return 2
	}
	if *version {
		fmt.Println(client.VersionString())
		return 0
	}
	if *updateHelper != "" {
		if err := client.RunClientUpdateHelper(*updateHelper); err != nil {
			fmt.Fprintf(os.Stderr, "rule-bot-client: update helper failed: %v\n", err)
			return 1
		}
		return 0
	}
	updateOptions := client.ClientUpdateOptions{
		ConfigPath: *configPath, RestartArgs: []string{"--config", *configPath},
	}
	if *updateCheck {
		info, err := client.CheckClientUpdate(context.Background(), updateOptions)
		if err != nil && !errors.Is(err, client.ErrNoUpdate) {
			fmt.Fprintf(os.Stderr, "rule-bot-client: update check failed: %v\n", err)
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(info)
		return 0
	}

	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rule-bot-client: %v\n", err)
		return 1
	}
	if *check {
		if err := client.CheckConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "rule-bot-client: configuration check failed: %v\n", err)
			return 1
		}
		fmt.Println("configuration is valid")
		return 0
	}
	if *updateApply {
		if err := client.CheckConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "rule-bot-client: configuration check failed: %v\n", err)
			return 1
		}
		info, err := client.ApplyLatestClientUpdate(context.Background(), updateOptions)
		if err != nil && !errors.Is(err, client.ErrNoUpdate) && !errors.Is(err, client.ErrRestartScheduled) {
			fmt.Fprintf(os.Stderr, "rule-bot-client: update failed: %v\n", err)
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(info)
		return 0
	}

	logger := log.New(os.Stderr, "", log.Ldate|log.Ltime|log.LUTC)
	logger.Printf("INFO starting %s", client.VersionString())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runClient(ctx, cfg, *configPath, logger); err != nil {
		logger.Printf("ERROR %v", err)
		return 1
	}
	return 0
}

func defaultConfigPath() string {
	if runtime.GOOS != "windows" {
		return "/etc/rule-bot-client/config.json"
	}
	executable, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(executable), "config.json")
}

func runClient(ctx context.Context, cfg client.Config, configPath string, logger *log.Logger) error {
	if runtime.GOOS != "windows" || !cfg.AutoUpdate {
		return client.Run(ctx, cfg, logger)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- client.Run(runCtx, cfg, logger) }()
	prepared := make(chan *client.PreparedClientUpdate, 1)
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-timer.C:
			}
			candidate, err := client.PrepareClientUpdate(runCtx, client.ClientUpdateOptions{
				ConfigPath: configPath, RestartArgs: []string{"--config", configPath},
			})
			switch {
			case err == nil:
				prepared <- candidate
				return
			case errors.Is(err, client.ErrNoUpdate):
				logger.Printf("INFO automatic update check: already up to date")
			default:
				logger.Printf("WARN automatic update check failed: %v", err)
			}
			timer.Reset(24 * time.Hour)
		}
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		cancel()
		return <-result
	case candidate := <-prepared:
		logger.Printf("INFO automatic update ready version=%s commit=%s", candidate.Info.LatestVersion, candidate.Info.Commit)
		cancel()
		if err := <-result; err != nil {
			candidate.Abort()
			return err
		}
		err := candidate.Activate(context.Background())
		if errors.Is(err, client.ErrRestartScheduled) {
			logger.Printf("INFO automatic update replacement scheduled")
			return nil
		}
		return err
	}
}
