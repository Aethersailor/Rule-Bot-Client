package openwrt

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	if generated.Config.RuntimeCacheDir != "/etc/rule-bot-client/cache" {
		t.Fatalf("runtime cache directory = %q", generated.Config.RuntimeCacheDir)
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

func TestAutomaticControllerSecretOverrideAndFallback(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "/etc/config/openclash", `config openclash 'config'
	option cn_port '9090'
	option dashboard_password 'discovered-openclash-secret'
`)
	writeRootFile(t, root, "/etc/nikki/run/config.yaml", `external-controller: "127.0.0.1:9091"
secret: discovered-nikki-secret
`)
	settings := DefaultSettings()
	settings.Sources[0].Secret = "override-openclash-secret"
	settings.Sources[1].Enabled = true
	settings.Sources[1].Secret = "override-nikki-secret"
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
	wantOverridePaths := map[string]string{
		SourceOpenClash: sourceSecretPath(SourceOpenClash),
		SourceNikki:     sourceSecretPath(SourceNikki),
	}
	gotOverridePaths := map[string]string{}
	for _, instance := range generated.Config.Instances {
		gotOverridePaths[instance.Name] = instance.SecretFile
	}
	for id, want := range wantOverridePaths {
		if gotOverridePaths[id] != want {
			t.Fatalf("%s secret_file = %q, want override %q", id, gotOverridePaths[id], want)
		}
	}
	uci, err := os.ReadFile(rooted(root, "/etc/config/rule_bot_client"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{"override-openclash-secret", "override-nikki-secret"} {
		if bytesContains(uci, sensitive) {
			t.Fatalf("UCI exposed automatic adapter override %q", sensitive)
		}
	}
	editableResult, err := backend.Dispatch(context.Background(), "config_edit", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONOmits(t, editableResult, "override-openclash-secret", "override-nikki-secret")
	editable := editableResult.(Settings)
	for index := range editable.Sources {
		if editable.Sources[index].ID == SourceOpenClash || editable.Sources[index].ID == SourceNikki {
			if !editable.Sources[index].SecretSet || editable.Sources[index].Secret != "" {
				t.Fatalf("editable source was not redacted: %+v", editable.Sources[index])
			}
			editable.Sources[index].ClearSecret = true
		}
	}
	if _, err := backend.save(context.Background(), editable); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	generated, err = buildRuntimeConfig(root, loaded)
	if err != nil {
		t.Fatal(err)
	}
	wantDiscoveredPaths := map[string]string{
		SourceOpenClash: "/var/run/rule-bot-client/openclash.secret",
		SourceNikki:     "/var/run/rule-bot-client/nikki.secret",
	}
	gotDiscoveredPaths := map[string]string{}
	for _, instance := range generated.Config.Instances {
		gotDiscoveredPaths[instance.Name] = instance.SecretFile
	}
	for id, want := range wantDiscoveredPaths {
		if gotDiscoveredPaths[id] != want {
			t.Fatalf("%s secret_file = %q, want discovered %q", id, gotDiscoveredPaths[id], want)
		}
	}
	for _, id := range []string{SourceOpenClash, SourceNikki} {
		if _, err := os.Stat(rooted(root, sourceSecretPath(id))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s override survived clear: %v", id, err)
		}
	}
	for path, want := range map[string]string{
		"/var/run/rule-bot-client/openclash.secret": "discovered-openclash-secret",
		"/var/run/rule-bot-client/nikki.secret":     "discovered-nikki-secret",
	} {
		data, err := os.ReadFile(rooted(root, path))
		if err != nil || strings.TrimSpace(string(data)) != want {
			t.Fatalf("discovered credential %s = %q, %v", path, data, err)
		}
	}
}

func TestCredentialClearSetConflictsAreRejectedWithoutEchoingValues(t *testing.T) {
	tests := []struct {
		name      string
		sensitive string
		wantError string
		configure func(*Settings)
	}{
		{
			name: "source secret", sensitive: "do-not-echo-source-secret", wantError: "cannot set and clear its secret",
			configure: func(settings *Settings) {
				settings.Sources[0].Secret = "do-not-echo-source-secret"
				settings.Sources[0].ClearSecret = true
			},
		},
		{
			name: "custom CA", sensitive: "do-not-echo-custom-ca", wantError: "cannot set and clear its custom CA",
			configure: func(settings *Settings) {
				settings.Sources[0].CAPEM = "do-not-echo-custom-ca"
				settings.Sources[0].ClearCA = true
			},
		},
		{
			name: "Rule-Bot token", sensitive: "do-not-echo-rule-bot-token", wantError: "cannot be set and cleared",
			configure: func(settings *Settings) {
				settings.RuleBot.Token = "do-not-echo-rule-bot-token"
				settings.RuleBot.ClearToken = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := DefaultSettings()
			test.configure(&settings)
			_, err := (Backend{Root: t.TempDir(), Testing: true}).save(context.Background(), settings)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("save() error = %v", err)
			}
			if strings.Contains(err.Error(), test.sensitive) {
				t.Fatalf("save() error exposed submitted credential: %v", err)
			}
		})
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
	settings.Sources = []Source{{
		ID: "src_0123abcd", Type: SourceManual, Enabled: true, Name: "Manual", URL: "http://127.0.0.1:9",
		Secret: "backup-controller-secret",
	}}
	settings.RuleBot.Token = "backup-rule-bot-token"
	sourceBackend := Backend{Root: sourceRoot, Testing: true}
	if _, err := sourceBackend.save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	writeRootFile(t, sourceRoot, "/etc/rule-bot-client/data/domains.txt", "preserved.example\n")
	writeRootFile(t, sourceRoot, "/etc/rule-bot-client/data/domains.txt.dedupe-cache", strings.Repeat("x", maxBackupBytes+1))
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
	for path, want := range map[string]string{
		sourceSecretPath("src_0123abcd"): "backup-controller-secret",
		ruleBotTokenPath():               "backup-rule-bot-token",
	} {
		credential, err := os.ReadFile(rooted(targetRoot, path))
		if err != nil || strings.TrimSpace(string(credential)) != want {
			t.Fatalf("restored credential %s = %q, %v", path, credential, err)
		}
		if mode := fileMode(t, targetRoot, path); runtime.GOOS != "windows" && mode != 0o640 {
			t.Fatalf("restored credential %s mode = %04o", path, mode)
		}
	}
	restoredUCI, err := os.ReadFile(rooted(targetRoot, "/etc/config/rule_bot_client"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{"backup-controller-secret", "backup-rule-bot-token"} {
		if bytesContains(restoredUCI, sensitive) {
			t.Fatalf("restored UCI exposed credential %q", sensitive)
		}
	}
	info, err := targetBackend.upgradeInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info["package_manager"] != "apk" || info["complete"] != true {
		t.Fatalf("upgradeInfo = %#v", info)
	}
}

func TestRestoreRemovesManagedFilesMissingFromBackup(t *testing.T) {
	sourceRoot := t.TempDir()
	sourceSettings := DefaultSettings()
	sourceSettings.Enabled = false
	sourceBackend := Backend{Root: sourceRoot, Testing: true}
	if _, err := sourceBackend.save(context.Background(), sourceSettings); err != nil {
		t.Fatal(err)
	}
	archive, err := sourceBackend.createBackup()
	if err != nil {
		t.Fatal(err)
	}

	targetRoot := t.TempDir()
	targetSettings := DefaultSettings()
	targetSettings.Enabled = false
	targetBackend := Backend{Root: targetRoot, Testing: true}
	if _, err := targetBackend.save(context.Background(), targetSettings); err != nil {
		t.Fatal(err)
	}
	stale := []string{
		sourceSecretPath(SourceOpenClash),
		ruleBotTokenPath(),
		sourceCAPath(SourceOpenClash),
		"/etc/rule-bot-client/data/rulebot-state.json",
	}
	for _, path := range stale {
		writeRootFile(t, targetRoot, path, "stale-restored-value\n")
	}

	if _, err := targetBackend.restore(context.Background(), base64.StdEncoding.EncodeToString(archive)); err != nil {
		t.Fatal(err)
	}
	for _, path := range stale {
		if _, err := os.Lstat(rooted(targetRoot, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed path absent from backup survived restore: %s: %v", path, err)
		}
	}
}

func TestRestoreRejectsArchiveWithoutUCIConfig(t *testing.T) {
	partialRoot := t.TempDir()
	writeRootFile(t, partialRoot, ruleBotTokenPath(), "partial-token\n")
	partialArchive, err := (Backend{Root: partialRoot, Testing: true}).createBackup()
	if err != nil {
		t.Fatal(err)
	}

	targetRoot := t.TempDir()
	targetSettings := DefaultSettings()
	targetSettings.Enabled = false
	targetBackend := Backend{Root: targetRoot, Testing: true}
	if _, err := targetBackend.save(context.Background(), targetSettings); err != nil {
		t.Fatal(err)
	}
	configPath := rooted(targetRoot, "/etc/config/rule_bot_client")
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = targetBackend.restore(context.Background(), base64.StdEncoding.EncodeToString(partialArchive))
	if err == nil || !strings.Contains(err.Error(), "missing etc/config/rule_bot_client") {
		t.Fatalf("restore() error = %v", err)
	}
	currentConfig, err := os.ReadFile(configPath)
	if err != nil || string(currentConfig) != string(originalConfig) {
		t.Fatalf("partial restore changed the existing UCI config: %q, %v", currentConfig, err)
	}
	if _, err := os.Lstat(rooted(targetRoot, ruleBotTokenPath())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial restore wrote a token before rejecting the archive: %v", err)
	}
}

func TestReconcileBackupDirectoryRejectsNonDirectoryTargets(t *testing.T) {
	const logical = "/etc/rule-bot-client/credentials"
	t.Run("regular file", func(t *testing.T) {
		root := t.TempDir()
		writeRootFile(t, root, logical, "do-not-replace\n")
		err := reconcileBackupDirectory(root, logical, map[string]backupEntry{})
		if err == nil || !strings.Contains(err.Error(), "not a real directory") {
			t.Fatalf("reconcileBackupDirectory() error = %v", err)
		}
		data, readErr := os.ReadFile(rooted(root, logical))
		if readErr != nil || string(data) != "do-not-replace\n" {
			t.Fatalf("non-directory target changed: %q, %v", data, readErr)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		victim := filepath.Join(outside, "outside-secret")
		if err := os.WriteFile(victim, []byte("must-survive\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := rooted(root, logical)
		if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("directory symlinks unavailable: %v", err)
		}
		err := reconcileBackupDirectory(root, logical, map[string]backupEntry{})
		if err == nil || !strings.Contains(err.Error(), "not a real directory") {
			t.Fatalf("reconcileBackupDirectory() error = %v", err)
		}
		data, readErr := os.ReadFile(victim)
		if readErr != nil || string(data) != "must-survive\n" {
			t.Fatalf("symlink target was modified: %q, %v", data, readErr)
		}
	})
}

func TestApplyBackupValidatesCompleteArchiveBeforeMutation(t *testing.T) {
	sourceRoot := t.TempDir()
	sourceSettings := DefaultSettings()
	sourceSettings.Enabled = false
	sourceSettings.Sources[0].Secret = "replacement-secret"
	sourceBackend := Backend{Root: sourceRoot, Testing: true}
	if _, err := sourceBackend.save(context.Background(), sourceSettings); err != nil {
		t.Fatal(err)
	}
	archive, err := sourceBackend.createBackup()
	if err != nil {
		t.Fatal(err)
	}
	archive[len(archive)-1] ^= 0xff

	targetRoot := t.TempDir()
	targetSettings := DefaultSettings()
	targetSettings.Enabled = false
	targetBackend := Backend{Root: targetRoot, Testing: true}
	if _, err := targetBackend.save(context.Background(), targetSettings); err != nil {
		t.Fatal(err)
	}
	writeRootFile(t, targetRoot, sourceSecretPath(SourceOpenClash), "original-secret\n")
	if err := targetBackend.applyBackup(archive); err == nil {
		t.Fatal("applyBackup() accepted an archive with a corrupt trailer")
	}
	secret, err := os.ReadFile(rooted(targetRoot, sourceSecretPath(SourceOpenClash)))
	if err != nil || string(secret) != "original-secret\n" {
		t.Fatalf("target changed before complete archive validation: %q, %v", secret, err)
	}
}

func TestRestoreFailureRollsBackExactManagedFiles(t *testing.T) {
	targetRoot := t.TempDir()
	targetSettings := DefaultSettings()
	targetSettings.Enabled = false
	targetBackend := Backend{Root: targetRoot, Testing: true}
	if _, err := targetBackend.save(context.Background(), targetSettings); err != nil {
		t.Fatal(err)
	}
	original := map[string]string{
		sourceSecretPath(SourceOpenClash):              "original-secret\n",
		ruleBotTokenPath():                             "original-token\n",
		"/etc/rule-bot-client/data/rulebot-state.json": "{\"version\":1,\"offset\":0}\n",
		"/etc/rule-bot-client/data/domains.txt":        "original.example\n",
	}
	for path, data := range original {
		writeRootFile(t, targetRoot, path, data)
	}

	invalidRoot := t.TempDir()
	writeRootFile(t, invalidRoot, "/etc/config/rule_bot_client", "unsupported invalid-backup\n")
	writeRootFile(t, invalidRoot, sourceSecretPath(SourceOpenClash), "replacement-secret\n")
	newOnlyPath := sourceCAPath(SourceNikki)
	writeRootFile(t, invalidRoot, newOnlyPath, "replacement-certificate\n")
	invalidArchive, err := (Backend{Root: invalidRoot, Testing: true}).createBackup()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := targetBackend.restore(context.Background(), base64.StdEncoding.EncodeToString(invalidArchive)); err == nil {
		t.Fatal("restore() succeeded with an invalid restored UCI configuration")
	}
	for path, want := range original {
		data, err := os.ReadFile(rooted(targetRoot, path))
		if err != nil || string(data) != want {
			t.Fatalf("rollback did not restore %s: %q, %v", path, data, err)
		}
	}
	if _, err := os.Lstat(rooted(targetRoot, newOnlyPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback retained a file introduced by the failed restore: %v", err)
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

func TestReadRPCDoesNotExposeRuleBotEndpointOrProxyCredentials(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "/etc/config/rule_bot_client", `config main 'main'
	option schema_version '1'
	option enabled '1'
	option work_mode 'rulebot'
	option domain_mode 'registrable_domain'
	option flush_interval '5s'
	option storage_mode 'persistent'

config source 'openclash'
	option type 'openclash'
	option enabled '1'
	option name 'OpenClash'

config source 'src_0123abcd'
	option type 'manual'
	option enabled '0'
	option name 'Private Controller'
	option url 'http://192.0.2.10:9090'

config rule_bot 'rule_bot'
	option enabled '1'
	option endpoint 'https://private-rule-bot.example/api/private/hidden-path'
	option proxy_url 'http://proxy-user:proxy-password@127.0.0.1:7890'
`)
	writeRootFile(t, root, ruleBotTokenPath(), "existing-token\n")
	writeRootFile(t, root, sourceSecretPath("src_0123abcd"), "existing-controller-secret\n")

	backend := Backend{Root: root, Testing: true}
	result, err := backend.Dispatch(context.Background(), "config", nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := result.(Settings)
	assertJSONOmits(t, result, "existing-token", "existing-controller-secret", "proxy-user", "proxy-password")
	if settings.RuleBot.Endpoint != "" || settings.RuleBot.ProxyURL != "" {
		t.Fatalf("read config exposed Rule-Bot settings: endpoint=%q proxy=%q", settings.RuleBot.Endpoint, settings.RuleBot.ProxyURL)
	}

	result, err = backend.Dispatch(context.Background(), "status", nil)
	if err != nil {
		t.Fatal(err)
	}
	status := result.(ServiceStatus)
	assertJSONOmits(t, result, "existing-token", "existing-controller-secret", "private-rule-bot.example", "proxy-user", "proxy-password")
	if status.Config.RuleBot.Endpoint != "" || status.Config.RuleBot.ProxyURL != "" {
		t.Fatalf("read status exposed Rule-Bot settings: endpoint=%q proxy=%q", status.Config.RuleBot.Endpoint, status.Config.RuleBot.ProxyURL)
	}
	status.Config.Enabled = false
	if _, err := backend.save(context.Background(), status.Config); err != nil {
		t.Fatal(err)
	}
	stored, err := LoadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RuleBot.Endpoint != "https://private-rule-bot.example/api/private/hidden-path" || stored.RuleBot.ProxyURL != "http://proxy-user:proxy-password@127.0.0.1:7890" {
		t.Fatalf("redacted status round trip changed sensitive settings: endpoint=%q proxy=%q", stored.RuleBot.Endpoint, stored.RuleBot.ProxyURL)
	}

	result, err = backend.Dispatch(context.Background(), "config_edit", nil)
	if err != nil {
		t.Fatal(err)
	}
	editable := result.(Settings)
	assertJSONOmits(t, result, "existing-token", "existing-controller-secret", "proxy-user", "proxy-password")
	if editable.RuleBot.Endpoint != "https://private-rule-bot.example/api/private/hidden-path" {
		t.Fatalf("write-authorized editor endpoint = %q", editable.RuleBot.Endpoint)
	}
	if editable.RuleBot.ProxyURL != "http://127.0.0.1:7890" || !editable.RuleBot.ProxyCredentialsSet {
		t.Fatalf("write-authorized editor proxy = %q credentials_set=%t", editable.RuleBot.ProxyURL, editable.RuleBot.ProxyCredentialsSet)
	}
	editable.Enabled = false
	if _, err := backend.save(context.Background(), editable); err != nil {
		t.Fatal(err)
	}
	stored, err = LoadSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RuleBot.Endpoint != "https://private-rule-bot.example/api/private/hidden-path" || stored.RuleBot.ProxyURL != "http://proxy-user:proxy-password@127.0.0.1:7890" {
		t.Fatalf("editor round trip changed sensitive settings: endpoint=%q proxy=%q", stored.RuleBot.Endpoint, stored.RuleBot.ProxyURL)
	}
}

func TestUpdateVersionComparison(t *testing.T) {
	development, err := parseUpdateVersion("0.1.0_git32650000000", false)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := parseUpdateVersion("v0.2.1", true)
	if err != nil {
		t.Fatal(err)
	}
	if compareUpdateVersions(development, stable) >= 0 {
		t.Fatal("development package was not older than the stable Release")
	}
	if _, err := parseUpdateVersion("v0.2.1-beta.1", true); err == nil {
		t.Fatal("stable update parser accepted a prerelease")
	}
}

func TestUpdateManifestSelectsHighestPriorityArchitecture(t *testing.T) {
	manifest := []byte("format\tarchitecture\tasset\tsha256\tsize\tsdk_url\n" +
		"ipk\tmips_24kc\tluci-app-rule-bot-client_0.2.1-r1_mips_24kc.ipk\t" + strings.Repeat("a", 64) + "\t5000000\thttps://downloads.openwrt.org/releases/24.10.4/targets/ath79/generic/sdk.tar.zst\n" +
		"ipk\tmipsel_24kc\tluci-app-rule-bot-client_0.2.1-r1_mipsel_24kc.ipk\t" + strings.Repeat("b", 64) + "\t5100000\thttps://downloads.openwrt.org/releases/24.10.4/targets/ramips/mt7621/sdk.tar.zst\n")
	packages, err := parseUpdateManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := selectUpdatePackage(packages, updateEnvironment{
		Manager: "opkg", Format: "ipk", Architectures: map[string]int{"mips_24kc": 10, "mipsel_24kc": 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Architecture != "mipsel_24kc" {
		t.Fatalf("selected architecture = %q", selected.Architecture)
	}
}

func TestUpdateManifestRejectsDuplicateOrUnsafeEntries(t *testing.T) {
	entry := "apk\tx86_64\tluci-app-rule-bot-client-0.2.1-r1_x86_64.apk\t" + strings.Repeat("c", 64) + "\t5000000\thttps://downloads.openwrt.org/releases/25.12.0/targets/x86/64/sdk.tar.zst\n"
	if _, err := parseUpdateManifest([]byte("format\tarchitecture\tasset\tsha256\tsize\tsdk_url\n" + entry + entry)); err == nil {
		t.Fatal("duplicate update package identity was accepted")
	}
	unsafe := strings.Replace(entry, "luci-app-rule-bot-client-0.2.1-r1_x86_64.apk", "../candidate.apk", 1)
	if _, err := parseUpdateManifest([]byte("format\tarchitecture\tasset\tsha256\tsize\tsdk_url\n" + unsafe)); err == nil {
		t.Fatal("unsafe update package name was accepted")
	}
}

func TestUpdateAssetIdentifiesStableVersion(t *testing.T) {
	for _, asset := range []string{
		"luci-app-rule-bot-client-0.2.1-r1_x86_64.apk",
		"luci-app-rule-bot-client_0.2.1-r1_x86_64.ipk",
	} {
		version, err := updateVersionFromAsset(asset)
		if err != nil || version != "0.2.1" {
			t.Fatalf("asset = %q, version = %q, error = %v", asset, version, err)
		}
	}
	if _, err := updateVersionFromAsset("luci-app-rule-bot-client-0.2.1-beta-r1_x86_64.apk"); err == nil {
		t.Fatal("prerelease package name was accepted as stable")
	}
}

func TestAutomaticUpdateSettingRoundTrips(t *testing.T) {
	settings := DefaultSettings()
	settings.AutoUpdate = true
	config, err := ParseUCI(RenderUCI(settingsUCI(settings)))
	if err != nil {
		t.Fatal(err)
	}
	if config.Sections[0].Options["auto_update"] != "1" {
		t.Fatal("automatic update setting was not persisted")
	}
}

func assertJSONOmits(t *testing.T, value any, sensitive ...string) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range sensitive {
		if bytesContains(data, item) {
			t.Fatalf("serialized response exposed %q: %s", item, data)
		}
	}
}

func bytesContains(data []byte, value string) bool {
	return strings.Contains(string(data), value)
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
