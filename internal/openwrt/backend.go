package openwrt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Aethersailor/Rule-Bot-Client/internal/client"
)

type Backend struct {
	Root    string
	Testing bool
}

type fileSnapshot struct {
	Path   string
	Exists bool
	Mode   os.FileMode
	Data   []byte
}

func (b Backend) Dispatch(ctx context.Context, action string, payload []byte) (any, error) {
	if len(payload) > maxRPCRequestBytes {
		return nil, errors.New("request exceeds 4 MiB")
	}
	switch action {
	case "config":
		settings, err := LoadSettings(b.Root)
		return settingsForRead(settings), err
	case "config_edit":
		settings, err := LoadSettings(b.Root)
		return settingsForEditor(settings), err
	case "initialize":
		return b.initialize()
	case "generate":
		return b.generate(ctx)
	case "save":
		var settings Settings
		if err := decodeStrict(payload, &settings); err != nil {
			return nil, err
		}
		return b.save(ctx, settings)
	case "status":
		return b.status()
	case "probe":
		var request struct {
			ID string `json:"id"`
		}
		if err := decodeStrict(payload, &request); err != nil {
			return nil, err
		}
		return b.probe(ctx, request.ID)
	case "domains":
		var request struct {
			Query  string `json:"query"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
		}
		if err := decodeStrict(payload, &request); err != nil {
			return nil, err
		}
		return b.domains(request.Query, request.Offset, request.Limit)
	case "export":
		return b.exportDomains()
	case "clear":
		var request struct {
			Confirm string `json:"confirm"`
		}
		if err := decodeStrict(payload, &request); err != nil {
			return nil, err
		}
		if request.Confirm != "CLEAR" {
			return nil, errors.New("clear requires exact confirmation")
		}
		return b.clear(ctx)
	case "logs":
		return b.logs()
	case "service":
		var request struct {
			Action string `json:"action"`
		}
		if err := decodeStrict(payload, &request); err != nil {
			return nil, err
		}
		return b.service(request.Action)
	case "backup":
		return b.backup()
	case "restore":
		var request struct {
			Archive string `json:"archive"`
		}
		if err := decodeStrict(payload, &request); err != nil {
			return nil, err
		}
		return b.restore(ctx, request.Archive)
	case "upgrade":
		return b.upgradeInfo()
	case "update_status":
		return b.updateStatus()
	case "update_check":
		return b.checkUpdate(ctx)
	case "update_config":
		var request struct {
			Enabled bool `json:"enabled"`
		}
		if err := decodeStrict(payload, &request); err != nil {
			return nil, err
		}
		return b.setAutomaticUpdates(request.Enabled)
	case "update_start":
		return b.startUpdateWorker()
	case "update_worker":
		return nil, b.runUpdate(ctx)
	case "update_auto":
		return b.autoUpdate(ctx)
	default:
		return nil, errors.New("unsupported fixed action")
	}
}

func decodeStrict(data []byte, destination any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("invalid request: multiple JSON values")
	}
	return nil
}

func (b Backend) generate(ctx context.Context) (map[string]any, error) {
	settings, err := LoadSettings(b.Root)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return map[string]any{"ok": true, "enabled": false}, nil
	}
	generated, err := buildRuntimeConfig(b.Root, settings)
	if err != nil {
		return nil, err
	}
	if err := writeRuntimeConfig(b.Root, generated); err != nil {
		return nil, err
	}
	if err := b.checkConfig(ctx); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "enabled": true, "instances": len(generated.Config.Instances),
		"config": "/var/run/rule-bot-client/config.json", "adapters": generated.Adapters,
	}, nil
}

func (b Backend) save(ctx context.Context, settings Settings) (response SaveResponse, err error) {
	existing, err := LoadSettings(b.Root)
	if err != nil {
		return response, err
	}
	reconcileSensitiveRuleBot(existing.RuleBot, &settings.RuleBot)
	if err := ValidateSettings(&settings); err != nil {
		return response, err
	}
	paths := affectedPaths(b.Root, existing, settings)
	snapshots, err := snapshotFiles(paths)
	if err != nil {
		return response, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = restoreFiles(snapshots)
		if existing.Enabled {
			_, _ = b.generate(context.Background())
			_ = b.runService("reload")
		} else {
			_ = b.runService("stop")
		}
	}()
	if err := b.writeCredentials(existing, &settings); err != nil {
		return response, err
	}
	uciPath := rooted(b.Root, "/etc/config/rule_bot_client")
	if err := atomicWrite(uciPath, RenderUCI(settingsUCI(settings)), 0o600); err != nil {
		return response, fmt.Errorf("commit UCI candidate: %w", err)
	}
	if settings.Enabled {
		if _, err := b.generate(ctx); err != nil {
			return response, fmt.Errorf("candidate generation/check failed: %w", err)
		}
		startedAfter := time.Now()
		if err := b.runService("reload"); err != nil {
			return response, err
		}
		if err := b.confirmHealthy(startedAfter); err != nil {
			return response, err
		}
	} else if err := b.runService("stop"); err != nil {
		return response, err
	}
	if err := removeOrphanCredentials(b.Root, existing, settings); err != nil {
		return response, err
	}
	committed = true
	settings, _ = LoadSettings(b.Root)
	return SaveResponse{OK: true, Config: settingsForEditor(settings), Warnings: settings.Warnings}, nil
}

func settingsForRead(settings Settings) Settings {
	settings.RuleBot.Endpoint = ""
	settings.RuleBot.ProxyURL = ""
	settings.RuleBot.SensitiveRedacted = true
	settings.RuleBot.ProxyCredentialsSet = false
	return settings
}

func settingsForEditor(settings Settings) Settings {
	proxy, credentialsSet := proxyURLForEditor(settings.RuleBot.ProxyURL)
	settings.RuleBot.ProxyURL = proxy
	settings.RuleBot.SensitiveRedacted = false
	settings.RuleBot.ProxyCredentialsSet = credentialsSet
	return settings
}

func proxyURLForEditor(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw, false
	}
	parsed.User = nil
	return parsed.String(), true
}

func reconcileSensitiveRuleBot(existing RuleBot, submitted *RuleBot) {
	if submitted.SensitiveRedacted {
		submitted.Endpoint = existing.Endpoint
		submitted.ProxyURL = existing.ProxyURL
	} else if submitted.ProxyCredentialsSet {
		visibleProxy, credentialsSet := proxyURLForEditor(existing.ProxyURL)
		if credentialsSet && submitted.ProxyURL == visibleProxy {
			submitted.ProxyURL = existing.ProxyURL
		}
	}
	submitted.SensitiveRedacted = false
	submitted.ProxyCredentialsSet = false
}

func affectedPaths(root string, oldSettings, newSettings Settings) []string {
	set := map[string]struct{}{
		rooted(root, "/etc/config/rule_bot_client"):          {},
		rooted(root, "/var/run/rule-bot-client/config.json"): {},
		rooted(root, ruleBotTokenPath()):                     {},
	}
	for _, settings := range []Settings{oldSettings, newSettings} {
		for _, source := range settings.Sources {
			if sourceIDPattern.MatchString(source.ID) {
				set[rooted(root, sourceSecretPath(source.ID))] = struct{}{}
				set[rooted(root, sourceCAPath(source.ID))] = struct{}{}
			}
		}
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	return paths
}

func snapshotFiles(paths []string) ([]fileSnapshot, error) {
	result := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			result = append(result, fileSnapshot{Path: path})
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("transaction target %s is not a regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		result = append(result, fileSnapshot{Path: path, Exists: true, Mode: info.Mode().Perm(), Data: data})
	}
	return result, nil
}

func restoreFiles(snapshots []fileSnapshot) error {
	var first error
	for _, snapshot := range snapshots {
		if snapshot.Exists {
			if err := atomicWrite(snapshot.Path, snapshot.Data, snapshot.Mode); err != nil && first == nil {
				first = err
			}
		} else if err := os.Remove(snapshot.Path); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

func (b Backend) writeCredentials(existing Settings, settings *Settings) error {
	existingByID := map[string]Source{}
	for _, source := range existing.Sources {
		existingByID[source.ID] = source
	}
	for index := range settings.Sources {
		source := &settings.Sources[index]
		old := existingByID[source.ID]
		secretPath := rooted(b.Root, sourceSecretPath(source.ID))
		switch {
		case source.ClearSecret:
			if err := removeManagedFile(secretPath); err != nil {
				return fmt.Errorf("clear source %q secret: %w", source.ID, err)
			}
		case source.Secret != "":
			if err := validateSecret(source.Secret); err != nil {
				return fmt.Errorf("source %q secret: %w", source.ID, err)
			}
			if err := atomicWrite(secretPath, []byte(source.Secret+"\n"), 0o640); err != nil {
				return err
			}
			if err := secureRuntimeReadable(b.Root, sourceSecretPath(source.ID)); err != nil {
				return err
			}
		case source.PreserveSecret || old.SecretSet:
		default:
			if err := removeManagedFile(secretPath); err != nil {
				return fmt.Errorf("remove source %q secret: %w", source.ID, err)
			}
		}
		caPath := rooted(b.Root, sourceCAPath(source.ID))
		switch {
		case source.ClearCA:
			if err := removeManagedFile(caPath); err != nil {
				return fmt.Errorf("clear source %q CA: %w", source.ID, err)
			}
		case strings.TrimSpace(source.CAPEM) != "":
			if err := validateCAPEM(source.CAPEM); err != nil {
				return fmt.Errorf("source %q CA: %w", source.ID, err)
			}
			if err := atomicWrite(caPath, []byte(strings.TrimSpace(source.CAPEM)+"\n"), 0o640); err != nil {
				return err
			}
			if err := secureRuntimeReadable(b.Root, sourceCAPath(source.ID)); err != nil {
				return err
			}
		case source.PreserveCA || old.CASet:
		default:
			if err := removeManagedFile(caPath); err != nil {
				return fmt.Errorf("remove source %q CA: %w", source.ID, err)
			}
		}
		source.Secret = ""
		source.CAPEM = ""
		source.SecretSet = regularNonempty(secretPath)
		source.CASet = regularNonempty(caPath)
	}
	tokenPath := rooted(b.Root, ruleBotTokenPath())
	switch {
	case settings.RuleBot.ClearToken:
		if err := removeManagedFile(tokenPath); err != nil {
			return fmt.Errorf("clear Rule-Bot token: %w", err)
		}
	case settings.RuleBot.Token != "":
		if err := validateSecret(settings.RuleBot.Token); err != nil {
			return fmt.Errorf("Rule-Bot token: %w", err)
		}
		if err := atomicWrite(tokenPath, []byte(settings.RuleBot.Token+"\n"), 0o640); err != nil {
			return err
		}
		if err := secureRuntimeReadable(b.Root, ruleBotTokenPath()); err != nil {
			return err
		}
	case settings.RuleBot.PreserveToken || existing.RuleBot.TokenSet:
	default:
		if err := removeManagedFile(tokenPath); err != nil {
			return fmt.Errorf("remove Rule-Bot token: %w", err)
		}
	}
	settings.RuleBot.Token = ""
	settings.RuleBot.TokenSet = regularNonempty(tokenPath)
	if settings.RuleBot.Enabled && !settings.RuleBot.TokenSet {
		return errors.New("Rule-Bot mode requires a token")
	}
	return nil
}

func validateSecret(value string) error {
	if len(value) > 4096 {
		return errors.New("value exceeds 4096 bytes")
	}
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
		return errors.New("value must be non-empty and contain no surrounding whitespace or control lines")
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] == 0x7f {
			return errors.New("value contains an HTTP control character")
		}
	}
	return nil
}

func removeManagedFile(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func removeOrphanCredentials(root string, oldSettings, newSettings Settings) error {
	keep := map[string]struct{}{}
	for _, source := range newSettings.Sources {
		keep[source.ID] = struct{}{}
	}
	for _, source := range oldSettings.Sources {
		if _, exists := keep[source.ID]; exists || !sourceIDPattern.MatchString(source.ID) {
			continue
		}
		if err := removeManagedFile(rooted(root, sourceSecretPath(source.ID))); err != nil {
			return fmt.Errorf("remove orphan source %q secret: %w", source.ID, err)
		}
		if err := removeManagedFile(rooted(root, sourceCAPath(source.ID))); err != nil {
			return fmt.Errorf("remove orphan source %q CA: %w", source.ID, err)
		}
	}
	return nil
}

func (b Backend) checkConfig(ctx context.Context) error {
	if b.Testing || (b.Root != "" && b.Root != "/") {
		return nil
	}
	command := exec.CommandContext(ctx, "/usr/bin/rule-bot-client", "--config", "/var/run/rule-bot-client/config.json", "--check")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rule-bot-client --check: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (b Backend) runService(action string) error {
	if b.Testing || (b.Root != "" && b.Root != "/") {
		return nil
	}
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		return errors.New("invalid service action")
	}
	output, err := exec.Command("/etc/init.d/rule-bot-client", action).CombinedOutput()
	if err != nil {
		return fmt.Errorf("service %s failed: %s", action, strings.TrimSpace(string(output)))
	}
	return nil
}

func (b Backend) confirmHealthy(startedAfter time.Time) error {
	if b.Testing || (b.Root != "" && b.Root != "/") {
		return nil
	}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if serviceRunning(b.Root, "rule-bot-client") {
			data, err := os.ReadFile("/var/run/rule-bot-client/status/status.json")
			if err == nil && runtimeStatusIsFresh(data, startedAfter, time.Now()) {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("service did not publish a fresh healthy runtime status; candidate was rolled back")
}

func runtimeStatusIsFresh(data []byte, startedAfter, now time.Time) bool {
	var status struct {
		StartedAt time.Time `json:"started_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if json.Unmarshal(data, &status) != nil || status.StartedAt.IsZero() || status.UpdatedAt.IsZero() {
		return false
	}
	if status.StartedAt.Before(startedAfter) || status.UpdatedAt.Before(status.StartedAt) {
		return false
	}
	age := now.Sub(status.UpdatedAt)
	return age >= -2*time.Second && age < 15*time.Second
}

func (b Backend) status() (ServiceStatus, error) {
	settings, err := LoadSettings(b.Root)
	if err != nil {
		return ServiceStatus{}, err
	}
	status := ServiceStatus{
		Version: 1, GeneratedAt: time.Now().UTC(), Config: settingsForRead(settings),
		Service: "stopped", Adapters: map[string]AdapterStatus{}, Runtime: map[string]any{},
		Output: map[string]any{}, RuleBot: map[string]any{"enabled": settings.RuleBot.Enabled}, Storage: map[string]any{"mode": settings.Storage.Mode},
	}
	if serviceRunning(b.Root, "rule-bot-client") {
		status.Service = "running"
	}
	discovered, discoverErr := discoverSources(b.Root, settings)
	status.Adapters = discovered.Adapters
	if discoverErr != nil {
		status.DiscoveryError = discoverErr.Error()
	}
	if data, readErr := os.ReadFile(rooted(b.Root, "/var/run/rule-bot-client/status/status.json")); readErr == nil {
		_ = json.Unmarshal(data, &status.Runtime)
	}
	dataDir, storageErr := resolveDataDir(b.Root, settings.Storage)
	if storageErr != nil {
		status.Storage["available"] = false
		status.Storage["error"] = storageErr.Error()
		return status, nil
	}
	status.Storage["available"] = true
	status.Storage["path"] = dataDir
	status.Storage["free_bytes"] = availableBytes(rooted(b.Root, dataDir))
	outputPath := rooted(b.Root, filepath.ToSlash(filepath.Join(dataDir, "domains.txt")))
	if info, statErr := os.Stat(outputPath); statErr == nil {
		status.Output["path"] = filepath.ToSlash(filepath.Join(dataDir, "domains.txt"))
		status.Output["bytes"] = info.Size()
		status.Output["exists"] = true
	} else {
		status.Output["exists"] = false
	}
	statePath := rooted(b.Root, filepath.ToSlash(filepath.Join(dataDir, "rulebot-state.json")))
	if data, readErr := os.ReadFile(statePath); readErr == nil {
		var state map[string]any
		if json.Unmarshal(data, &state) == nil {
			status.RuleBot["state"] = state
		}
	}
	return status, nil
}

func (b Backend) probe(ctx context.Context, id string) (client.ProbeResult, error) {
	if !sourceIDPattern.MatchString(id) {
		return client.ProbeResult{}, errors.New("invalid source ID")
	}
	settings, err := LoadSettings(b.Root)
	if err != nil {
		return client.ProbeResult{}, err
	}
	generated, err := buildRuntimeConfig(b.Root, settings)
	if err != nil {
		return client.ProbeResult{}, err
	}
	for _, instance := range generated.Config.Instances {
		if instance.Name == id {
			if b.Root != "" && b.Root != "/" {
				return client.ProbeResult{}, errors.New("probe is unavailable in an offline test root")
			}
			return client.ProbeController(ctx, instance)
		}
	}
	return client.ProbeResult{}, errors.New("enabled source is not currently discoverable")
}

func (b Backend) domains(query string, offset, limit int) (map[string]any, error) {
	if len(query) > 128 || strings.ContainsAny(query, "\r\n\x00") || offset < 0 {
		return nil, errors.New("invalid domain query")
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 500 {
		return nil, errors.New("limit must be between 1 and 500")
	}
	path, err := b.outputPath()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{"items": []string{}, "offset": offset, "more": false}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	query = strings.ToLower(query)
	items := make([]string, 0, limit)
	matched := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4096)
	for scanner.Scan() {
		value := scanner.Text()
		if query != "" && !strings.Contains(strings.ToLower(value), query) {
			continue
		}
		if matched >= offset && len(items) < limit {
			items = append(items, value)
		}
		matched++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "offset": offset, "next_offset": offset + len(items), "more": matched > offset+len(items), "matches": matched}, nil
}

func (b Backend) exportDomains() (map[string]any, error) {
	path, err := b.outputPath()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{"filename": "rule-bot-client-domains.txt", "content": ""}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDomainExportSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDomainExportSize {
		return nil, errors.New("domain export exceeds 4 MiB")
	}
	return map[string]any{"filename": "rule-bot-client-domains.txt", "content": string(data)}, nil
}

func (b Backend) outputPath() (string, error) {
	settings, err := LoadSettings(b.Root)
	if err != nil {
		return "", err
	}
	dataDir, err := resolveDataDir(b.Root, settings.Storage)
	if err != nil {
		return "", err
	}
	return rooted(b.Root, filepath.ToSlash(filepath.Join(dataDir, "domains.txt"))), nil
}

func (b Backend) clear(ctx context.Context) (map[string]any, error) {
	settings, err := LoadSettings(b.Root)
	if err != nil {
		return nil, err
	}
	dataDir, err := resolveDataDir(b.Root, settings.Storage)
	if err != nil {
		return nil, err
	}
	output := rooted(b.Root, filepath.ToSlash(filepath.Join(dataDir, "domains.txt")))
	state := rooted(b.Root, filepath.ToSlash(filepath.Join(dataDir, "rulebot-state.json")))
	snapshots, err := snapshotFiles([]string{output, state})
	if err != nil {
		return nil, err
	}
	rollback := func() {
		_ = restoreFiles(snapshots)
		_ = secureRuntimeData(b.Root, settings.Storage)
		_ = b.runService("start")
	}
	if err := b.runService("stop"); err != nil {
		return nil, err
	}
	if err := atomicWrite(output, nil, 0o600); err != nil {
		rollback()
		return nil, err
	}
	if err := secureRuntimeWritable(b.Root, filepath.ToSlash(filepath.Join(dataDir, "domains.txt"))); err != nil {
		rollback()
		return nil, err
	}
	if err := os.Remove(state); err != nil && !errors.Is(err, os.ErrNotExist) {
		rollback()
		return nil, err
	}
	if settings.Enabled {
		if err := b.runService("start"); err != nil {
			rollback()
			return nil, err
		}
	}
	return map[string]any{"ok": true, "output_and_rulebot_state_cleared": true}, nil
}

func (b Backend) logs() (map[string]any, error) {
	if b.Testing || (b.Root != "" && b.Root != "/") {
		return map[string]any{"lines": ""}, nil
	}
	output, err := exec.Command("/sbin/logread", "-e", "rule-bot-client", "-l", "200").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read logs: %w", err)
	}
	if len(output) > 256<<10 {
		output = output[len(output)-(256<<10):]
	}
	return map[string]any{"lines": string(output)}, nil
}

func (b Backend) service(action string) (map[string]any, error) {
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		return nil, errors.New("invalid service action")
	}
	if err := b.runService(action); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "action": action}, nil
}

func (b Backend) upgradeInfo() (map[string]any, error) {
	keepPath := rooted(b.Root, "/lib/upgrade/keep.d/rule-bot-client")
	keep, err := os.ReadFile(keepPath)
	if err != nil {
		return nil, err
	}
	manager := "none"
	arch := ""
	if _, err := os.Stat(rooted(b.Root, "/usr/bin/apk")); err == nil {
		manager = "apk"
		if b.Root == "" || b.Root == "/" {
			output, _ := exec.Command("/usr/bin/apk", "--print-arch").Output()
			arch = strings.TrimSpace(string(output))
		}
	} else if _, err := os.Stat(rooted(b.Root, "/bin/opkg")); err == nil {
		manager = "opkg"
		if b.Root == "" || b.Root == "/" {
			output, _ := exec.Command("/bin/opkg", "print-architecture").Output()
			arch = strings.TrimSpace(string(output))
		}
	}
	required := []string{"/etc/config/rule_bot_client", "/etc/rule-bot-client/credentials/", "/etc/rule-bot-client/certs/", "/etc/rule-bot-client/exclude.list", "/etc/rule-bot-client/data/", "/etc/rule-bot-client/recover.sh"}
	missing := []string{}
	for _, path := range required {
		if !strings.Contains(string(keep), path) {
			missing = append(missing, path)
		}
	}
	return map[string]any{"package_manager": manager, "architecture": arch, "keep_list": string(keep), "complete": len(missing) == 0, "missing": missing}, nil
}
