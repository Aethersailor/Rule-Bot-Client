package openwrt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Aethersailor/Rule-Bot-Client/internal/client"
)

type generatedConfig struct {
	Config   client.Config
	Adapters map[string]AdapterStatus
	DataDir  string
}

func buildRuntimeConfig(root string, settings Settings) (generatedConfig, error) {
	if err := ValidateSettings(&settings); err != nil {
		return generatedConfig{}, err
	}
	dataDir, err := resolveDataDir(root, settings.Storage)
	if err != nil {
		return generatedConfig{}, err
	}
	if err := ensureRuntimeDirectories(root, dataDir); err != nil {
		return generatedConfig{}, err
	}
	discovered, err := discoverSources(root, settings)
	if err != nil {
		return generatedConfig{}, err
	}
	config := client.Config{
		Version:                  client.ConfigVersion,
		Output:                   filepath.ToSlash(filepath.Join(dataDir, "domains.txt")),
		StatusFile:               "/var/run/rule-bot-client/status/status.json",
		DomainMode:               client.DomainMode(settings.DomainMode),
		FlushInterval:            client.Duration(mustDuration(settings.FlushInterval)),
		IncludeFailedConnections: settings.IncludeFailedConnections,
		IncludeSingleLabelHosts:  settings.IncludeSingleLabelHosts,
		Instances:                discovered.Instances,
	}
	if settings.RuleBot.Enabled {
		config.RuleBot = client.RuleBotConfig{
			Enabled:      true,
			Endpoint:     settings.RuleBot.Endpoint,
			TokenFile:    ruleBotTokenPath(),
			StateFile:    filepath.ToSlash(filepath.Join(dataDir, "rulebot-state.json")),
			SendExisting: settings.RuleBot.SendExisting,
			ProxyURL:     settings.RuleBot.ProxyURL,
			Privacy: client.RuleBotPrivacyConfig{
				ExcludeFile: "/etc/rule-bot-client/exclude.list",
			},
		}
		if !regularNonempty(rooted(root, ruleBotTokenPath())) {
			return generatedConfig{}, errors.New("Rule-Bot is enabled but its token is missing")
		}
	}
	return generatedConfig{Config: config, Adapters: discovered.Adapters, DataDir: dataDir}, nil
}

func writeRuntimeConfig(root string, generated generatedConfig) error {
	data, err := json.MarshalIndent(generated.Config, "", "  ")
	if err != nil {
		return err
	}
	path := rooted(root, "/var/run/rule-bot-client/config.json")
	if err := atomicWrite(path, append(data, '\n'), 0o640); err != nil {
		return fmt.Errorf("publish generated config: %w", err)
	}
	if root == "" || root == "/" {
		if err := os.Chown(path, 0, 65534); err != nil {
			return fmt.Errorf("secure generated config ownership: %w", err)
		}
	}
	return nil
}

func resolveDataDir(root string, storage Storage) (string, error) {
	switch storage.Mode {
	case StoragePersistent:
		return "/etc/rule-bot-client/data", nil
	case StorageTemporary:
		return "/tmp/rule-bot-client/data", nil
	case StorageExternal:
		if !externalMounted(root, storage.ExternalPath) {
			return "", fmt.Errorf("external storage %s is not mounted; refusing overlay fallback", storage.ExternalPath)
		}
		return storage.ExternalPath, nil
	default:
		return "", errors.New("unsupported storage mode")
	}
}

func externalMounted(root, path string) bool {
	data, err := os.ReadFile(rooted(root, "/proc/mounts"))
	if err != nil {
		return false
	}
	clean := filepath.Clean(path)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		mountpoint := strings.ReplaceAll(fields[1], `\040`, " ")
		if mountpoint == "/" || mountpoint == "/overlay" {
			continue
		}
		mountpoint = filepath.Clean(mountpoint)
		if clean == mountpoint || strings.HasPrefix(filepath.ToSlash(clean), filepath.ToSlash(mountpoint)+"/") {
			return true
		}
	}
	return false
}

func ensureRuntimeDirectories(root, dataDir string) error {
	paths := []struct {
		path  string
		mode  os.FileMode
		owner int
	}{
		{"/var/run/rule-bot-client", 0o750, 0},
		{"/var/run/rule-bot-client/status", 0o750, 65534},
		{"/etc/rule-bot-client/credentials", 0o750, 0},
		{"/etc/rule-bot-client/certs", 0o750, 0},
		{dataDir, 0o750, 65534},
	}
	for _, item := range paths {
		path := rooted(root, item.path)
		if err := os.MkdirAll(path, item.mode); err != nil {
			return fmt.Errorf("create %s: %w", item.path, err)
		}
		if err := os.Chmod(path, item.mode); err != nil {
			return err
		}
		if root == "" || root == "/" {
			if err := os.Chown(path, item.owner, 65534); err != nil {
				return fmt.Errorf("set %s ownership: %w", item.path, err)
			}
		}
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".rule-bot-client-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func mustDuration(value string) time.Duration {
	duration, _ := time.ParseDuration(value)
	return duration
}
