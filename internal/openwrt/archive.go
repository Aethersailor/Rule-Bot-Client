package openwrt

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type backupEntry struct {
	data []byte
	mode os.FileMode
}

type parsedBackup struct {
	entries    map[string]backupEntry
	activeData map[string][]byte
}

func (b Backend) backup() (map[string]any, error) {
	archive, err := b.createBackup()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"filename": "rule-bot-client-backup-" + time.Now().UTC().Format("20060102T150405Z") + ".tar.gz",
		"sha256":   sha256Hex(archive), "size": len(archive), "archive": base64.StdEncoding.EncodeToString(archive),
	}, nil
}

func (b Backend) createBackup() ([]byte, error) {
	var buffer bytes.Buffer
	gzipWriter, _ := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	tarWriter := tar.NewWriter(gzipWriter)
	paths := []string{
		"/etc/config/rule_bot_client", "/etc/rule-bot-client/credentials", "/etc/rule-bot-client/certs",
		"/etc/rule-bot-client/exclude.list", "/etc/rule-bot-client/data", "/etc/rule-bot-client/recover.sh",
	}
	for _, logical := range paths {
		if err := addBackupPath(tarWriter, b.Root, logical, strings.TrimPrefix(filepath.ToSlash(logical), "/")); err != nil {
			return nil, err
		}
	}
	settings, err := LoadSettings(b.Root)
	if err == nil {
		if dataDir, resolveErr := resolveDataDir(b.Root, settings.Storage); resolveErr == nil && dataDir != "/etc/rule-bot-client/data" {
			for _, name := range []string{"domains.txt", "rulebot-state.json"} {
				if err := addBackupPath(tarWriter, b.Root, filepath.ToSlash(filepath.Join(dataDir, name)), "active-data/"+name); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > maxBackupBytes {
		return nil, errors.New("backup exceeds 4 MiB; download domains separately or reduce retained data")
	}
	return buffer.Bytes(), nil
}

func addBackupPath(writer *tar.Writer, root, logical, archiveName string) error {
	if strings.HasSuffix(filepath.Base(logical), ".dedupe-cache") {
		return nil
	}
	path := rooted(root, logical)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink in backup: %s", logical)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := addBackupPath(writer, root, filepath.ToSlash(filepath.Join(logical, entry.Name())), filepath.ToSlash(filepath.Join(archiveName, entry.Name()))); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing special file in backup: %s", logical)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	header := &tar.Header{Name: archiveName, Mode: int64(info.Mode().Perm()), Size: int64(len(data)), ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func (b Backend) restore(ctx context.Context, encoded string) (map[string]any, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(maxBackupBytes) {
		return nil, errors.New("restore archive exceeds 4 MiB")
	}
	archive, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("restore archive is not valid base64")
	}
	parsed, err := parseBackup(archive)
	if err != nil {
		return nil, err
	}
	if _, exists := parsed.entries["etc/config/rule_bot_client"]; !exists {
		return nil, errors.New("restore archive is missing etc/config/rule_bot_client")
	}
	preRestore, err := b.createBackup()
	if err != nil {
		return nil, fmt.Errorf("create rollback backup: %w", err)
	}
	if err := b.runService("stop"); err != nil {
		return nil, err
	}
	rollback := func() {
		_ = b.applyBackup(preRestore)
		_, _ = b.generate(context.Background())
		_ = b.runService("start")
	}
	if err := b.applyParsedBackup(parsed); err != nil {
		rollback()
		return nil, err
	}
	if _, err := b.initialize(); err != nil {
		rollback()
		return nil, err
	}
	settings, err := LoadSettings(b.Root)
	if err != nil {
		rollback()
		return nil, err
	}
	if settings.Enabled {
		if _, err := b.generate(ctx); err != nil {
			rollback()
			return nil, err
		}
		startedAfter := time.Now()
		if err := b.runService("start"); err != nil {
			rollback()
			return nil, err
		}
		if err := b.confirmHealthy(startedAfter); err != nil {
			rollback()
			return nil, err
		}
	}
	return map[string]any{"ok": true, "sha256": sha256Hex(archive)}, nil
}

func (b Backend) applyBackup(archive []byte) error {
	parsed, err := parseBackup(archive)
	if err != nil {
		return err
	}
	return b.applyParsedBackup(parsed)
}

func (b Backend) applyParsedBackup(parsed parsedBackup) error {
	for _, logical := range []string{
		"/etc/rule-bot-client/credentials",
		"/etc/rule-bot-client/certs",
		"/etc/rule-bot-client/data",
	} {
		if err := reconcileBackupDirectory(b.Root, logical, parsed.entries); err != nil {
			return err
		}
	}

	entryNames := make([]string, 0, len(parsed.entries))
	for name := range parsed.entries {
		entryNames = append(entryNames, name)
	}
	slices.Sort(entryNames)
	for _, name := range entryNames {
		entry := parsed.entries[name]
		logical := "/" + name
		if err := atomicWrite(rooted(b.Root, logical), entry.data, entry.mode); err != nil {
			return err
		}
		if entry.mode == 0o640 {
			if err := secureRuntimeReadable(b.Root, logical); err != nil {
				return err
			}
		}
	}

	settings, err := LoadSettings(b.Root)
	if err != nil {
		return err
	}
	dataDir, err := resolveDataDir(b.Root, settings.Storage)
	if err != nil {
		return err
	}
	if dataDir != "/etc/rule-bot-client/data" || len(parsed.activeData) != 0 {
		if err := reconcileActiveData(b.Root, dataDir, parsed.activeData); err != nil {
			return err
		}
	}
	if err := secureCredentialDirectories(b.Root); err != nil {
		return err
	}
	if err := secureCredentialFiles(b.Root); err != nil {
		return err
	}
	return secureRuntimeData(b.Root, settings.Storage)
}

func parseBackup(archive []byte) (parsedBackup, error) {
	result := parsedBackup{
		entries:    map[string]backupEntry{},
		activeData: map[string][]byte{},
	}
	if len(archive) > maxBackupBytes {
		return result, errors.New("archive exceeds 4 MiB")
	}
	compressed := bytes.NewReader(archive)
	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return result, errors.New("archive is not gzip")
	}
	gzipReader.Multistream(false)
	reader := tar.NewReader(gzipReader)
	total := int64(0)
	seen := map[string]struct{}{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = gzipReader.Close()
			return result, err
		}
		name := filepath.ToSlash(filepath.Clean(header.Name))
		if header.Typeflag != tar.TypeReg || strings.HasPrefix(name, "/") || strings.Contains(name, "../") || !allowedBackupName(name) {
			_ = gzipReader.Close()
			return result, fmt.Errorf("unsafe restore entry %q", header.Name)
		}
		if _, exists := seen[name]; exists {
			_ = gzipReader.Close()
			return result, fmt.Errorf("duplicate restore entry %q", header.Name)
		}
		seen[name] = struct{}{}
		if header.Size < 0 || header.Size > int64(maxBackupBytes)-total {
			_ = gzipReader.Close()
			return result, errors.New("unpacked restore exceeds 4 MiB")
		}
		total += header.Size
		data, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(data)) != header.Size {
			_ = gzipReader.Close()
			return result, errors.New("truncated restore entry")
		}
		if strings.HasPrefix(name, "active-data/") {
			result.activeData[strings.TrimPrefix(name, "active-data/")] = data
			continue
		}
		mode := os.FileMode(0o600)
		if strings.HasPrefix(name, "etc/rule-bot-client/credentials/") || strings.HasPrefix(name, "etc/rule-bot-client/certs/") || name == "etc/rule-bot-client/exclude.list" {
			mode = 0o640
		} else if name == "etc/rule-bot-client/recover.sh" {
			mode = 0o755
		}
		result.entries[name] = backupEntry{data: data, mode: mode}
	}
	trailing, err := io.ReadAll(io.LimitReader(gzipReader, maxBackupBytes+1))
	if err != nil {
		_ = gzipReader.Close()
		return result, err
	}
	if len(trailing) > maxBackupBytes {
		_ = gzipReader.Close()
		return result, errors.New("unpacked restore exceeds 4 MiB")
	}
	for _, value := range trailing {
		if value != 0 {
			_ = gzipReader.Close()
			return result, errors.New("archive contains trailing data")
		}
	}
	if err := gzipReader.Close(); err != nil {
		return result, err
	}
	if compressed.Len() != 0 {
		return result, errors.New("archive contains trailing compressed data")
	}
	return result, nil
}

func reconcileBackupDirectory(root, logical string, entries map[string]backupEntry) error {
	path := rooted(root, logical)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o750); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("restore target %s is not a real directory", logical)
	}
	prefix := strings.TrimPrefix(filepath.ToSlash(logical), "/") + "/"
	wanted := map[string]struct{}{}
	for name := range entries {
		if strings.HasPrefix(name, prefix) {
			wanted[strings.TrimPrefix(name, prefix)] = struct{}{}
		}
	}
	current, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range current {
		if _, keep := wanted[entry.Name()]; keep {
			continue
		}
		if err := os.Remove(filepath.Join(path, entry.Name())); err != nil {
			return fmt.Errorf("remove stale restored path %s: %w", filepath.ToSlash(filepath.Join(logical, entry.Name())), err)
		}
	}
	return nil
}

func reconcileActiveData(root, dataDir string, activeData map[string][]byte) error {
	for _, name := range []string{"domains.txt", "rulebot-state.json"} {
		logical := filepath.ToSlash(filepath.Join(dataDir, name))
		data, exists := activeData[name]
		if exists {
			if err := atomicWrite(rooted(root, logical), data, 0o600); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(rooted(root, logical)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale active data %s: %w", logical, err)
		}
	}
	return nil
}

func allowedBackupName(name string) bool {
	if name == "etc/config/rule_bot_client" || name == "etc/rule-bot-client/exclude.list" || name == "etc/rule-bot-client/recover.sh" ||
		name == "active-data/domains.txt" || name == "active-data/rulebot-state.json" {
		return true
	}
	for _, prefix := range []string{"etc/rule-bot-client/credentials/", "etc/rule-bot-client/certs/", "etc/rule-bot-client/data/"} {
		if strings.HasPrefix(name, prefix) && !strings.Contains(strings.TrimPrefix(name, prefix), "/") {
			return true
		}
	}
	return false
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
