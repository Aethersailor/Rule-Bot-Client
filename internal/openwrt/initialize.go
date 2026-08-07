package openwrt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// initialize creates and secures only Rule-Bot Client's native OpenWrt
// configuration and runtime paths. The new product intentionally does not
// discover or import configuration from any earlier product identity.
func (b Backend) initialize() (map[string]any, error) {
	for _, directory := range []string{
		"/etc/config", "/etc/rule-bot-client", "/etc/rule-bot-client/credentials", "/etc/rule-bot-client/certs", "/etc/rule-bot-client/data", "/var/run/rule-bot-client", "/var/run/rule-bot-client/status", "/var/run/rule-bot-client/rpc",
	} {
		mode := os.FileMode(0o750)
		if directory == "/etc/config" || directory == "/etc/rule-bot-client" {
			mode = 0o755
		} else if directory == "/var/run/rule-bot-client/rpc" {
			mode = 0o700
		}
		if err := os.MkdirAll(rooted(b.Root, directory), mode); err != nil {
			return nil, err
		}
		if err := os.Chmod(rooted(b.Root, directory), mode); err != nil {
			return nil, err
		}
	}
	if err := secureCredentialDirectories(b.Root); err != nil {
		return nil, err
	}
	if err := secureRuntimeDirectories(b.Root); err != nil {
		return nil, err
	}

	settings, err := LoadSettings(b.Root)
	if err != nil {
		return nil, err
	}
	if err := ValidateSettings(&settings); err != nil {
		return nil, fmt.Errorf("initialize schema: %w", err)
	}
	if err := secureRuntimeData(b.Root, settings.Storage); err != nil {
		return nil, fmt.Errorf("secure runtime data: %w", err)
	}
	uciPath := rooted(b.Root, "/etc/config/rule_bot_client")
	if err := atomicWrite(uciPath, RenderUCI(settingsUCI(settings)), 0o600); err != nil {
		return nil, err
	}

	excludePath := rooted(b.Root, "/etc/rule-bot-client/exclude.list")
	if _, err := os.Stat(excludePath); errors.Is(err, os.ErrNotExist) {
		if err := atomicWrite(excludePath, nil, 0o640); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := secureRuntimeReadable(b.Root, "/etc/rule-bot-client/exclude.list"); err != nil {
		return nil, err
	}
	if err := secureCredentialFiles(b.Root); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "schema_version": SchemaVersion}, nil
}

func secureCredentialDirectories(root string) error {
	for _, logical := range []string{"/etc/rule-bot-client/credentials", "/etc/rule-bot-client/certs"} {
		path := rooted(root, logical)
		if err := os.Chmod(path, 0o750); err != nil {
			return err
		}
		if root == "" || root == "/" {
			if err := os.Chown(path, 0, 65534); err != nil {
				return err
			}
		}
	}
	return nil
}

func secureRuntimeDirectories(root string) error {
	for _, item := range []struct {
		logical string
		owner   int
		group   int
		mode    os.FileMode
	}{
		{"/var/run/rule-bot-client", 0, 65534, 0o750},
		{"/var/run/rule-bot-client/status", 65534, 65534, 0o750},
		{"/var/run/rule-bot-client/rpc", 0, 0, 0o700},
	} {
		path := rooted(root, item.logical)
		if err := os.Chmod(path, item.mode); err != nil {
			return err
		}
		if root == "" || root == "/" {
			if err := os.Chown(path, item.owner, item.group); err != nil {
				return err
			}
		}
	}
	return nil
}

func secureCredentialFiles(root string) error {
	for _, logical := range []string{"/etc/rule-bot-client/credentials", "/etc/rule-bot-client/certs"} {
		entries, err := os.ReadDir(rooted(root, logical))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
				return fmt.Errorf("credential path %s contains a non-regular entry", logical)
			}
			if err := secureRuntimeReadable(root, filepath.ToSlash(filepath.Join(logical, entry.Name()))); err != nil {
				return err
			}
		}
	}
	return nil
}

func secureRuntimeReadable(root, logical string) error {
	path := rooted(root, logical)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("runtime credential %s is not a regular file", logical)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		return err
	}
	if root == "" || root == "/" {
		if err := os.Chown(path, 0, 65534); err != nil {
			return err
		}
	}
	return nil
}

func secureRuntimeData(root string, storage Storage) error {
	dataDir, err := resolveDataDir(root, storage)
	if err != nil {
		return err
	}
	path := rooted(root, dataDir)
	if err := os.MkdirAll(path, 0o750); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("runtime data path %s is not a directory", dataDir)
	}
	if err := os.Chmod(path, 0o750); err != nil {
		return err
	}
	if root == "" || root == "/" {
		if err := os.Chown(path, 65534, 65534); err != nil {
			return err
		}
	}
	for _, name := range []string{"domains.txt", "rulebot-state.json"} {
		logical := filepath.ToSlash(filepath.Join(dataDir, name))
		if _, err := os.Lstat(rooted(root, logical)); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := secureRuntimeWritable(root, logical); err != nil {
			return err
		}
	}
	return nil
}

func secureRuntimeWritable(root, logical string) error {
	path := rooted(root, logical)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("runtime data %s is not a regular file", logical)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	if root == "" || root == "/" {
		if err := os.Chown(path, 65534, 65534); err != nil {
			return err
		}
	}
	return nil
}
