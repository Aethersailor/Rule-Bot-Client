package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/Aethersailor/Rule-Bot-Client/internal/client"
	"github.com/Aethersailor/Rule-Bot-Client/internal/openwrt"
)

var requestIDPattern = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("rule-bot-client-openwrt " + client.VersionString())
		return 0
	}
	if len(os.Args) < 2 || len(os.Args) > 3 {
		writeResult(nil, errors.New("usage: rule-bot-client-openwrt <fixed-action> [request-id]"))
		return 2
	}
	root := ""
	testing := os.Getenv("RULE_BOT_CLIENT_TESTING") == "1"
	if testing {
		root = os.Getenv("RULE_BOT_CLIENT_ROOT")
	}
	payload, err := readPayload(root)
	if err != nil {
		writeResult(nil, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := (openwrt.Backend{Root: root, Testing: testing}).Dispatch(ctx, os.Args[1], payload)
	writeResult(result, err)
	if err != nil {
		return 1
	}
	return 0
}

func readPayload(root string) ([]byte, error) {
	var reader io.Reader = os.Stdin
	var requestPath string
	if len(os.Args) == 3 {
		if !requestIDPattern.MatchString(os.Args[2]) {
			return nil, errors.New("invalid request ID")
		}
		requestPath = rootedForCLI(root, "/var/run/rule-bot-client/rpc/"+os.Args[2]+".json")
		file, err := os.Open(requestPath)
		if err != nil {
			return nil, errors.New("request payload is unavailable")
		}
		defer file.Close()
		defer os.Remove(requestPath)
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return nil, errors.New("read request payload")
	}
	if len(data) > 4<<20 {
		return nil, errors.New("request payload exceeds 4 MiB")
	}
	return data, nil
}

func rootedForCLI(root, path string) string {
	if root == "" || root == "/" {
		return path
	}
	return filepath.Join(root, filepath.FromSlash(path[1:]))
}

func writeResult(result any, err error) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(true)
	if err != nil {
		_ = encoder.Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if result == nil {
		result = map[string]any{"ok": true}
	}
	_ = encoder.Encode(result)
}
