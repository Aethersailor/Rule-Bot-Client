package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Aethersailor/Rule-Bot-Client/internal/client"
)

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "/etc/rule-bot-client/config.json", "path to the JSON configuration file")
	check := flag.Bool("check", false, "validate configuration and referenced credential/TLS files, then exit")
	version := flag.Bool("version", false, "print version information and exit")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "rule-bot-client does not accept positional arguments")
		return 2
	}
	if *version {
		fmt.Println(client.VersionString())
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

	logger := log.New(os.Stderr, "", log.Ldate|log.Ltime|log.LUTC)
	logger.Printf("INFO starting %s", client.VersionString())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := client.Run(ctx, cfg, logger); err != nil {
		logger.Printf("ERROR %v", err)
		return 1
	}
	return 0
}
