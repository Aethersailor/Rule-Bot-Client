package openwrt

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var sourceIDPattern = regexp.MustCompile(`^(?:openclash|nikki|src_[a-f0-9]{8})$`)

func LoadSettings(root string) (Settings, error) {
	path := rooted(root, "/etc/config/rule_bot_client")
	config, err := loadUCI(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read Rule-Bot Client UCI: %w", err)
	}
	settings := DefaultSettings()
	settings.Sources = nil
	main := config.section("main", "main")
	if main == nil {
		main = config.section("main", "")
	}
	if main != nil {
		settings.SchemaVersion = intOption(main, "schema_version")
		if settings.SchemaVersion == 0 {
			settings.SchemaVersion = 1
		}
		settings.Enabled = boolOption(main, "enabled", true)
		settings.WorkMode = stringOption(main, "work_mode", WorkModeLocal)
		settings.DomainMode = stringOption(main, "domain_mode", "registrable_domain")
		settings.FlushInterval = stringOption(main, "flush_interval", defaultFlush)
		settings.IncludeFailedConnections = boolOption(main, "include_failed_connections", true)
		settings.IncludeSingleLabelHosts = boolOption(main, "include_single_label_hosts", false)
		settings.AutoUpdate = boolOption(main, "auto_update", false)
		settings.Storage.Mode = stringOption(main, "storage_mode", StoragePersistent)
		settings.Storage.ExternalPath = strings.TrimSpace(main.Options["external_path"])
	}
	for _, section := range config.Sections {
		if section.Type != "source" {
			continue
		}
		source := Source{
			ID:                 section.Name,
			Type:               section.Options["type"],
			Enabled:            boolOption(&section, "enabled", false),
			Name:               section.Options["name"],
			URL:                section.Options["url"],
			TLSServerName:      section.Options["tls_server_name"],
			InsecureSkipVerify: boolOption(&section, "insecure_skip_verify", false),
			PreferTLS:          boolOption(&section, "prefer_tls", false),
		}
		if source.Name == "" {
			source.Name = source.ID
		}
		source.SecretSet = regularNonempty(rooted(root, sourceSecretPath(source.ID)))
		source.CASet = regularNonempty(rooted(root, sourceCAPath(source.ID)))
		settings.Sources = append(settings.Sources, source)
	}
	if len(settings.Sources) == 0 {
		settings.Sources = DefaultSettings().Sources
	}
	ruleBot := config.section("rule_bot", "rule_bot")
	if ruleBot == nil {
		ruleBot = config.section("rule_bot", "")
	}
	if ruleBot != nil {
		settings.RuleBot.Enabled = boolOption(ruleBot, "enabled", false)
		settings.RuleBot.Endpoint = strings.TrimSpace(ruleBot.Options["endpoint"])
		settings.RuleBot.SendExisting = boolOption(ruleBot, "send_existing", false)
		settings.RuleBot.ProxyURL = strings.TrimSpace(ruleBot.Options["proxy_url"])
	}
	settings.RuleBot.TokenSet = regularNonempty(rooted(root, ruleBotTokenPath()))
	return settings, nil
}

func stringOption(section *UCISection, key, fallback string) string {
	if section == nil {
		return fallback
	}
	value := strings.TrimSpace(section.Options[key])
	if value == "" {
		return fallback
	}
	return value
}

func regularNonempty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func settingsUCI(settings Settings) UCIConfig {
	config := UCIConfig{Sections: []UCISection{{
		Type: "main", Name: "main", Options: map[string]string{
			"schema_version":             strconvItoa(settings.SchemaVersion),
			"enabled":                    boolString(settings.Enabled),
			"work_mode":                  settings.WorkMode,
			"domain_mode":                settings.DomainMode,
			"flush_interval":             settings.FlushInterval,
			"include_failed_connections": boolString(settings.IncludeFailedConnections),
			"include_single_label_hosts": boolString(settings.IncludeSingleLabelHosts),
			"auto_update":                boolString(settings.AutoUpdate),
			"storage_mode":               settings.Storage.Mode,
		}, Lists: map[string][]string{},
	}}}
	if settings.Storage.Mode == StorageExternal {
		config.Sections[0].Options["external_path"] = settings.Storage.ExternalPath
	}
	for _, source := range settings.Sources {
		options := map[string]string{
			"type":    source.Type,
			"enabled": boolString(source.Enabled),
			"name":    source.Name,
		}
		if source.Type == SourceManual {
			options["url"] = source.URL
		}
		if source.TLSServerName != "" {
			options["tls_server_name"] = source.TLSServerName
		}
		if source.InsecureSkipVerify {
			options["insecure_skip_verify"] = "1"
		}
		if source.PreferTLS {
			options["prefer_tls"] = "1"
		}
		config.Sections = append(config.Sections, UCISection{
			Type: "source", Name: source.ID, Options: options, Lists: map[string][]string{},
		})
	}
	config.Sections = append(config.Sections, UCISection{
		Type: "rule_bot", Name: "rule_bot", Options: map[string]string{
			"enabled":       boolString(settings.RuleBot.Enabled),
			"send_existing": boolString(settings.RuleBot.SendExisting),
		}, Lists: map[string][]string{},
	})
	ruleBot := &config.Sections[len(config.Sections)-1]
	if settings.RuleBot.Endpoint != "" {
		ruleBot.Options["endpoint"] = settings.RuleBot.Endpoint
	}
	if settings.RuleBot.ProxyURL != "" {
		ruleBot.Options["proxy_url"] = settings.RuleBot.ProxyURL
	}
	return config
}

func ValidateSettings(settings *Settings) error {
	if err := validateCredentialEdits(*settings); err != nil {
		return err
	}
	settings.SchemaVersion = SchemaVersion
	switch settings.WorkMode {
	case WorkModeLocal:
		settings.RuleBot.Enabled = false
	case WorkModeRuleBot:
		settings.RuleBot.Enabled = true
	default:
		return errors.New("work_mode must be local or rulebot")
	}
	switch settings.DomainMode {
	case "hostname", "registrable_domain":
	default:
		return errors.New("domain_mode must be hostname or registrable_domain")
	}
	duration, err := time.ParseDuration(settings.FlushInterval)
	if err != nil || duration < 100*time.Millisecond || duration > 10*time.Minute {
		return errors.New("flush_interval must be between 100ms and 10m")
	}
	switch settings.Storage.Mode {
	case StoragePersistent, StorageTemporary:
		settings.Storage.ExternalPath = ""
	case StorageExternal:
		path := filepath.Clean(settings.Storage.ExternalPath)
		if !filepath.IsAbs(path) || path == "/mnt" || !strings.HasPrefix(filepath.ToSlash(path), "/mnt/") {
			return errors.New("external storage path must be an absolute directory below /mnt")
		}
		settings.Storage.ExternalPath = filepath.ToSlash(path)
	default:
		return errors.New("storage mode must be persistent, temporary, or external")
	}
	if len(settings.Sources) == 0 {
		return errors.New("at least one source is required")
	}
	ids := map[string]struct{}{}
	names := map[string]string{}
	urls := map[string]string{}
	enabled := 0
	for index := range settings.Sources {
		source := &settings.Sources[index]
		if source.ID == "" && source.Type == SourceManual {
			id, err := newSourceID()
			if err != nil {
				return err
			}
			source.ID = id
		}
		if !sourceIDPattern.MatchString(source.ID) {
			return fmt.Errorf("source %d has invalid stable ID", index)
		}
		if _, exists := ids[source.ID]; exists {
			return fmt.Errorf("duplicate source ID %q", source.ID)
		}
		ids[source.ID] = struct{}{}
		switch source.Type {
		case SourceOpenClash:
			if source.ID != SourceOpenClash {
				return errors.New("OpenClash source must use ID openclash")
			}
			source.URL = ""
		case SourceNikki:
			if source.ID != SourceNikki {
				return errors.New("Nikki source must use ID nikki")
			}
			source.URL = ""
		case SourceManual:
			normalized, err := normalizeControllerURL(source.URL)
			if err != nil {
				return fmt.Errorf("source %q: %w", source.ID, err)
			}
			source.URL = normalized
			key := strings.ToLower(normalized)
			if previous, exists := urls[key]; exists {
				return fmt.Errorf("duplicate controller address used by %q and %q", previous, source.ID)
			}
			urls[key] = source.ID
		default:
			return fmt.Errorf("source %q has unsupported type %q", source.ID, source.Type)
		}
		name := strings.TrimSpace(source.Name)
		if name == "" || len(name) > 64 || strings.ContainsAny(name, "\r\n\x00") {
			return fmt.Errorf("source %q must have a 1-64 character display name", source.ID)
		}
		source.Name = name
		nameKey := strings.ToLower(name)
		if previous, exists := names[nameKey]; exists {
			return fmt.Errorf("duplicate source name used by %q and %q", previous, source.ID)
		}
		names[nameKey] = source.ID
		if source.TLSServerName != "" && strings.ContainsAny(source.TLSServerName, "\r\n/\\") {
			return fmt.Errorf("source %q has invalid TLS server name", source.ID)
		}
		if source.InsecureSkipVerify && strings.TrimSpace(source.CAPEM) != "" {
			return fmt.Errorf("source %q cannot combine custom CA with skip verify", source.ID)
		}
		if strings.TrimSpace(source.CAPEM) != "" {
			if err := validateCAPEM(source.CAPEM); err != nil {
				return fmt.Errorf("source %q: %w", source.ID, err)
			}
		}
		if source.Enabled {
			enabled++
		}
	}
	if settings.Enabled && enabled == 0 {
		return errors.New("an enabled service requires at least one enabled source")
	}
	return validateRuleBot(&settings.RuleBot)
}

func validateCredentialEdits(settings Settings) error {
	for _, source := range settings.Sources {
		if source.ClearSecret && source.Secret != "" {
			return fmt.Errorf("source %q cannot set and clear its secret in the same request", source.ID)
		}
		if source.ClearCA && source.CAPEM != "" {
			return fmt.Errorf("source %q cannot set and clear its custom CA in the same request", source.ID)
		}
	}
	if settings.RuleBot.ClearToken && settings.RuleBot.Token != "" {
		return errors.New("Rule-Bot token cannot be set and cleared in the same request")
	}
	return nil
}

func validateRuleBot(ruleBot *RuleBot) error {
	if !ruleBot.Enabled {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(ruleBot.Endpoint))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path == "" || parsed.Path == "/" {
		return errors.New("Rule-Bot endpoint must be a complete HTTP(S) URL with a non-root path")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Rule-Bot endpoint must not include credentials, query, or fragment")
	}
	ruleBot.Endpoint = parsed.String()
	if ruleBot.ProxyURL != "" {
		proxy, err := url.Parse(ruleBot.ProxyURL)
		if err != nil || proxy.Host == "" || proxy.Path != "" || proxy.RawQuery != "" || proxy.Fragment != "" {
			return errors.New("Rule-Bot proxy URL is invalid")
		}
		switch proxy.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return errors.New("Rule-Bot proxy scheme must be http, https, socks5, or socks5h")
		}
		ruleBot.ProxyURL = proxy.String()
	}
	return nil
}

func normalizeControllerURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("controller URL must use http or https and include a host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("controller URL must not include credentials, path, query, or fragment")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "0.0.0.0" {
		hostname = "127.0.0.1"
	} else if hostname == "::" {
		hostname = "::1"
	}
	port := parsed.Port()
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Path = ""
	parsed.RawPath = ""
	return parsed.String(), nil
}

func validateCAPEM(value string) error {
	rest := []byte(value)
	valid := 0
	for {
		block, remainder := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remainder
		if block.Type != "CERTIFICATE" {
			return errors.New("custom CA may contain certificates only")
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return errors.New("custom CA contains an invalid certificate")
		}
		valid++
	}
	if valid == 0 || strings.TrimSpace(string(rest)) != "" {
		return errors.New("custom CA is not valid PEM")
	}
	return nil
}

func newSourceID() (string, error) {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate source ID: %w", err)
	}
	return "src_" + hex.EncodeToString(buffer), nil
}

func sourceSecretPath(id string) string { return "/etc/rule-bot-client/credentials/" + id + ".secret" }
func sourceCAPath(id string) string     { return "/etc/rule-bot-client/certs/" + id + "-ca.pem" }
func ruleBotTokenPath() string          { return "/etc/rule-bot-client/credentials/rulebot.token" }

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func strconvItoa(value int) string { return fmt.Sprintf("%d", value) }

func sortedSources(sources []Source) []Source {
	copySources := append([]Source(nil), sources...)
	sort.SliceStable(copySources, func(i, j int) bool {
		return copySources[i].ID < copySources[j].ID
	})
	return copySources
}
