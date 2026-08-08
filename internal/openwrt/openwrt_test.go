package openwrt

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUCIParserRoundTripsQuotedValues(t *testing.T) {
	input := []byte("config source 'src_0123abcd'\n\toption type 'manual'\n\toption name 'Owner'\\''s Mihomo' # comment\n\tlist tag 'one'\n\tlist tag 'two'\n")
	config, err := ParseUCI(input)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Sections[0].Options["name"]; got != "Owner's Mihomo" {
		t.Fatalf("name = %q", got)
	}
	roundTrip, err := ParseUCI(RenderUCI(config))
	if err != nil {
		t.Fatal(err)
	}
	if got := roundTrip.Sections[0].Lists["tag"]; strings.Join(got, ",") != "one,two" {
		t.Fatalf("tags = %v", got)
	}
}

func TestBuildRuntimeConfigCombinesOpenClashNikkiAndManual(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "/etc/config/openclash", `config openclash 'config'
	option cn_port '9090'
	option dashboard_password 'openclash-secret'
	option http_port '7890'
	option mixed_port '7893'
`)
	writeRootFile(t, root, "/etc/nikki/run/config.yaml", `external-controller: "[::]:9091"
external-controller-tls: "[::]:9443"
secret: nikki-secret
mixed-port: 7894
port: 7895
`)
	settings := DefaultSettings()
	settings.Sources[1].Enabled = true
	settings.Sources = append(settings.Sources, Source{
		ID: "src_0123abcd", Type: SourceManual, Enabled: true, Name: "Remote Mihomo", URL: "http://192.0.2.10:9092", Secret: "manual-secret",
	})
	backend := Backend{Root: root, Testing: true}
	if _, err := backend.save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := buildRuntimeConfig(root, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated.Config.Instances) != 3 {
		t.Fatalf("instances = %+v", generated.Config.Instances)
	}
	wantURLs := map[string]string{
		"openclash":    "http://127.0.0.1:9090",
		"nikki":        "http://[::1]:9091",
		"src_0123abcd": "http://192.0.2.10:9092",
	}
	for _, instance := range generated.Config.Instances {
		if instance.URL != wantURLs[instance.Name] {
			t.Fatalf("%s URL = %q", instance.Name, instance.URL)
		}
	}
	for path, want := range map[string]string{
		"/var/run/rule-bot-client/openclash.secret":            "openclash-secret",
		"/var/run/rule-bot-client/nikki.secret":                "nikki-secret",
		"/etc/rule-bot-client/credentials/src_0123abcd.secret": "manual-secret",
	} {
		data, err := os.ReadFile(rooted(root, path))
		if err != nil || strings.TrimSpace(string(data)) != want {
			t.Fatalf("credential %s was not materialized", path)
		}
		info, statErr := os.Stat(rooted(root, path))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
			t.Fatalf("credential %s mode = %v", path, info.Mode().Perm())
		}
	}
}

func TestNikkiRuntimeTLSAndUCIFallback(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "/etc/nikki/run/config.yaml", `external-controller: 0.0.0.0:9090
external-controller-tls: "[::]:9443"
secret: runtime-secret
mixed-port: 7890
`)
	source := Source{ID: "nikki", Type: SourceNikki, Enabled: true, Name: "Nikki", PreferTLS: true, InsecureSkipVerify: true}
	instance, status, err := discoverNikki(root, source)
	if err != nil {
		t.Fatal(err)
	}
	if instance.URL != "https://[::1]:9443" || !instance.TLS.InsecureSkipVerify || status.Source != "/etc/nikki/run/config.yaml" {
		t.Fatalf("runtime discovery = %+v status=%+v", instance, status)
	}
	if err := os.Remove(rooted(root, "/etc/nikki/run/config.yaml")); err != nil {
		t.Fatal(err)
	}
	writeRootFile(t, root, "/etc/config/nikki", `config mixin 'mixin'
	option api_listen '0.0.0.0:9191'
	option api_secret 'fallback-secret'
	option mixed_port '7999'
`)
	source.PreferTLS = false
	source.InsecureSkipVerify = false
	instance, status, err = discoverNikki(root, source)
	if err != nil {
		t.Fatal(err)
	}
	if instance.URL != "http://127.0.0.1:9191" || status.MixedPort != 7999 || !strings.Contains(status.Source, "waiting") {
		t.Fatalf("fallback discovery = %+v status=%+v", instance, status)
	}
}

func TestValidationRejectsDuplicateNormalizedManualTargets(t *testing.T) {
	settings := DefaultSettings()
	settings.Sources = []Source{
		{ID: "src_0123abcd", Type: SourceManual, Enabled: true, Name: "One", URL: "HTTP://EXAMPLE.COM:9090/"},
		{ID: "src_1234abcd", Type: SourceManual, Enabled: true, Name: "Two", URL: "http://example.com:9090"},
	}
	if err := ValidateSettings(&settings); err == nil || !strings.Contains(err.Error(), "duplicate controller") {
		t.Fatalf("ValidateSettings() error = %v", err)
	}
}

func TestOfflineManualTargetCanBeSavedAndDataSurvivesSourceDeletion(t *testing.T) {
	root := t.TempDir()
	settings := DefaultSettings()
	settings.Sources = []Source{{ID: "src_0123abcd", Type: SourceManual, Enabled: true, Name: "Offline", URL: "http://127.0.0.1:9"}}
	backend := Backend{Root: root, Testing: true}
	if _, err := backend.save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	writeRootFile(t, root, "/etc/rule-bot-client/data/domains.txt", "existing.example\n")
	writeRootFile(t, root, "/etc/rule-bot-client/data/rulebot-state.json", `{"version":1,"offset":17}`)
	settings.Sources = []Source{{ID: "src_1234abcd", Type: SourceManual, Enabled: true, Name: "Replacement", URL: "http://127.0.0.1:8"}}
	if _, err := backend.save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	for path := range map[string]struct{}{
		"/etc/rule-bot-client/data/domains.txt": {}, "/etc/rule-bot-client/data/rulebot-state.json": {},
	} {
		if _, err := os.Stat(rooted(root, path)); err != nil {
			t.Fatalf("source deletion removed %s: %v", path, err)
		}
	}
}

func TestExternalStorageMissingFailsClosed(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "/proc/mounts", "/dev/root / overlay rw 0 0\n")
	if _, err := resolveDataDir(root, Storage{Mode: StorageExternal, ExternalPath: "/mnt/usb/rule-bot-client"}); err == nil || !strings.Contains(err.Error(), "refusing overlay fallback") {
		t.Fatalf("resolveDataDir() error = %v", err)
	}
	writeRootFile(t, root, "/proc/mounts", "/dev/root / overlay rw 0 0\n/dev/sda1 /mnt/usb ext4 rw 0 0\n")
	if got, err := resolveDataDir(root, Storage{Mode: StorageExternal, ExternalPath: "/mnt/usb/rule-bot-client"}); err != nil || got != "/mnt/usb/rule-bot-client" {
		t.Fatalf("resolveDataDir() = %q, %v", got, err)
	}
}

func TestInitializeCreatesOnlyNativeUCI(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "/etc/rule-bot-client/config.json", `{
  "version":1,
  "output":"/etc/rule-bot-client/data/domains.txt",
  "domain_mode":"hostname",
  "flush_interval":"5s",
  "instances":[{"name":"unexpected","url":"http://127.0.0.1:9090","secret":"must-not-import"}]
}`)
	backend := Backend{Root: root, Testing: true}
	if _, err := backend.initialize(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(rooted(root, "/etc/config/rule_bot_client"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.initialize(); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(rooted(root, "/etc/config/rule_bot_client"))
	if string(first) != string(second) {
		t.Fatal("initialization is not idempotent")
	}
	if strings.Contains(string(first), "unexpected") {
		t.Fatal("initializer imported an alternate JSON configuration")
	}
	initialized, err := LoadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	if !initialized.IncludeFailedConnections || initialized.IncludeSingleLabelHosts {
		t.Fatalf("native defaults were not preserved: %+v", initialized)
	}
}

func TestDisabledServicePersistsWithoutGeneratingRuntimeConfig(t *testing.T) {
	root := t.TempDir()
	settings := DefaultSettings()
	settings.Enabled = false
	backend := Backend{Root: root, Testing: true}
	if _, err := backend.save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Enabled {
		t.Fatal("disabled service setting was not persisted")
	}
	generated, err := backend.generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if generated["enabled"] != false {
		t.Fatalf("generate() = %#v", generated)
	}
	if _, err := os.Stat(rooted(root, "/var/run/rule-bot-client/config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled service generated a runtime config: %v", err)
	}
}

func TestBackupRestoresAcrossPackageManagerChange(t *testing.T) {
	sourceRoot := t.TempDir()
	writeRootFile(t, sourceRoot, "/bin/opkg", "")
	settings := DefaultSettings()
	settings.Sources = []Source{{ID: "src_0123abcd", Type: SourceManual, Enabled: true, Name: "Manual", URL: "http://127.0.0.1:9"}}
	sourceBackend := Backend{Root: sourceRoot, Testing: true}
	if _, err := sourceBackend.save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	writeRootFile(t, sourceRoot, "/etc/rule-bot-client/data/domains.txt", "preserved.example\n")
	backup, err := sourceBackend.backup()
	if err != nil {
		t.Fatal(err)
	}
	archive, err := base64.StdEncoding.DecodeString(backup["archive"].(string))
	if err != nil || len(archive) == 0 {
		t.Fatal("backup archive is invalid")
	}

	targetRoot := t.TempDir()
	writeRootFile(t, targetRoot, "/usr/bin/apk", "")
	writeRootFile(t, targetRoot, "/lib/upgrade/keep.d/rule-bot-client", `/etc/config/rule_bot_client
/etc/rule-bot-client/credentials/
/etc/rule-bot-client/certs/
/etc/rule-bot-client/exclude.list
/etc/rule-bot-client/data/
/etc/rule-bot-client/recover.sh
`)
	targetBackend := Backend{Root: targetRoot, Testing: true}
	if _, err := targetBackend.restore(context.Background(), backup["archive"].(string)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(rooted(targetRoot, "/etc/rule-bot-client/data/domains.txt"))
	if err != nil || string(data) != "preserved.example\n" {
		t.Fatalf("restored data = %q, %v", data, err)
	}
	if mode := fileMode(t, targetRoot, "/etc/rule-bot-client/data/domains.txt"); runtime.GOOS != "windows" && mode != 0o600 {
		t.Fatalf("restored data mode = %04o", mode)
	}
	info, err := targetBackend.upgradeInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info["package_manager"] != "apk" || info["complete"] != true {
		t.Fatalf("upgradeInfo = %#v", info)
	}
}

func TestClearKeepsRuntimeDataWritableAndRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	settings := DefaultSettings()
	settings.Sources = []Source{{ID: "src_0123abcd", Type: SourceManual, Enabled: true, Name: "Manual", URL: "http://127.0.0.1:9"}}
	backend := Backend{Root: root, Testing: true}
	if _, err := backend.save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	writeRootFile(t, root, "/etc/rule-bot-client/data/domains.txt", "existing.example\n")
	writeRootFile(t, root, "/etc/rule-bot-client/data/rulebot-state.json", `{"version":1,"offset":17}`)
	if _, err := backend.clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, root, "/etc/rule-bot-client/data/domains.txt"); runtime.GOOS != "windows" && mode != 0o600 {
		t.Fatalf("cleared data mode = %04o", mode)
	}
	if _, err := os.Stat(rooted(root, "/etc/rule-bot-client/data/rulebot-state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rulebot state survived clear: %v", err)
	}

	target := rooted(root, "/etc/rule-bot-client/data/target")
	writeRootFile(t, root, "/etc/rule-bot-client/data/target", "not-data")
	link := rooted(root, "/etc/rule-bot-client/data/rulebot-state.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := secureRuntimeData(root, settings.Storage); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("secureRuntimeData() error = %v", err)
	}
}

func TestRuntimeStatusFreshnessRequiresCurrentProcess(t *testing.T) {
	now := time.Now().UTC()
	startedAfter := now.Add(-time.Second)
	fresh := fmt.Sprintf(`{"started_at":%q,"updated_at":%q}`, now.Add(-500*time.Millisecond).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if !runtimeStatusIsFresh([]byte(fresh), startedAfter, now) {
		t.Fatal("current process status was rejected")
	}
	staleProcess := fmt.Sprintf(`{"started_at":%q,"updated_at":%q}`, now.Add(-time.Minute).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if runtimeStatusIsFresh([]byte(staleProcess), startedAfter, now) {
		t.Fatal("stale process status was accepted because its update was recent")
	}
	staleUpdate := fmt.Sprintf(`{"started_at":%q,"updated_at":%q}`, now.Add(-500*time.Millisecond).Format(time.RFC3339Nano), now.Add(-time.Minute).Format(time.RFC3339Nano))
	if runtimeStatusIsFresh([]byte(staleUpdate), startedAfter, now) {
		t.Fatal("stale update was accepted")
	}
}

func fileMode(t *testing.T, root, logical string) os.FileMode {
	t.Helper()
	info, err := os.Stat(rooted(root, logical))
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func writeRootFile(t *testing.T, root, logical, contents string) {
	t.Helper()
	path := rooted(root, logical)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
