package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ConfigVersion = 1

type DomainMode string

const (
	DomainModeHostname          DomainMode = "hostname"
	DomainModeRegistrableDomain DomainMode = "registrable_domain"
)

type Duration time.Duration

func (d Duration) Value() time.Duration { return time.Duration(d) }

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a quoted Go duration such as \"5s\"")
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value, err)
	}
	*d = Duration(parsed)
	return nil
}

type Config struct {
	Version                  int              `json:"version"`
	Output                   string           `json:"output"`
	StatusFile               string           `json:"status_file,omitempty"`
	DomainMode               DomainMode       `json:"domain_mode,omitempty"`
	FlushInterval            Duration         `json:"flush_interval"`
	IncludeFailedConnections bool             `json:"include_failed_connections"`
	IncludeSingleLabelHosts  bool             `json:"include_single_label_hosts"`
	Instances                []InstanceConfig `json:"instances"`
	RuleBot                  RuleBotConfig    `json:"rule_bot,omitempty"`
}

type RuleBotConfig struct {
	Enabled              bool                 `json:"enabled"`
	Endpoint             string               `json:"endpoint,omitempty"`
	Token                string               `json:"token,omitempty"`
	TokenFile            string               `json:"token_file,omitempty"`
	TokenEnv             string               `json:"token_env,omitempty"`
	StateFile            string               `json:"state_file,omitempty"`
	SendExisting         bool                 `json:"send_existing,omitempty"`
	ProxyURL             string               `json:"proxy_url,omitempty"`
	ProxyFromEnvironment bool                 `json:"proxy_from_environment,omitempty"`
	Privacy              RuleBotPrivacyConfig `json:"privacy,omitempty"`
	Retry                ReconnectConfig      `json:"retry,omitempty"`
}

type RuleBotPrivacyConfig struct {
	ReduceToRegistrableDomain *bool    `json:"reduce_to_registrable_domain,omitempty"`
	ExcludeSuffixes           []string `json:"exclude_suffixes,omitempty"`
	ExcludeFile               string   `json:"exclude_file,omitempty"`
}

func (c RuleBotPrivacyConfig) reduceToRegistrableDomain() bool {
	return c.ReduceToRegistrableDomain == nil || *c.ReduceToRegistrableDomain
}

type InstanceConfig struct {
	Name       string          `json:"name"`
	URL        string          `json:"url"`
	Secret     string          `json:"secret,omitempty"`
	SecretFile string          `json:"secret_file,omitempty"`
	SecretEnv  string          `json:"secret_env,omitempty"`
	TLS        TLSConfig       `json:"tls,omitempty"`
	Reconnect  ReconnectConfig `json:"reconnect,omitempty"`
}

type TLSConfig struct {
	ServerName         string `json:"server_name,omitempty"`
	CAFile             string `json:"ca_file,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
}

type ReconnectConfig struct {
	InitialDelay Duration `json:"initial_delay,omitempty"`
	MaxDelay     Duration `json:"max_delay,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	cfg := Config{
		FlushInterval:            Duration(5 * time.Second),
		IncludeFailedConnections: true,
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("decode config: multiple JSON values are not allowed")
		}
		return Config{}, fmt.Errorf("decode config trailing data: %w", err)
	}

	configPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	if err := cfg.validate(filepath.Dir(configPath)); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) validate(configDir string) error {
	if c.Version != ConfigVersion {
		return fmt.Errorf("unsupported config version %d (expected %d)", c.Version, ConfigVersion)
	}
	if strings.TrimSpace(c.Output) == "" {
		return errors.New("output is required")
	}
	if !filepath.IsAbs(c.Output) {
		c.Output = filepath.Join(configDir, c.Output)
	}
	c.Output = filepath.Clean(c.Output)
	if c.StatusFile != "" {
		if !filepath.IsAbs(c.StatusFile) {
			c.StatusFile = filepath.Join(configDir, c.StatusFile)
		}
		c.StatusFile = filepath.Clean(c.StatusFile)
		if c.StatusFile == c.Output {
			return errors.New("status_file must differ from output")
		}
	}
	if c.DomainMode == "" {
		c.DomainMode = DomainModeHostname
	}
	switch c.DomainMode {
	case DomainModeHostname, DomainModeRegistrableDomain:
	default:
		return fmt.Errorf("domain_mode must be %q or %q", DomainModeHostname, DomainModeRegistrableDomain)
	}
	if c.FlushInterval.Value() <= 0 {
		return errors.New("flush_interval must be greater than zero")
	}
	if len(c.Instances) == 0 {
		return errors.New("at least one instance is required")
	}

	names := make(map[string]struct{}, len(c.Instances))
	for index := range c.Instances {
		instance := &c.Instances[index]
		if err := instance.validate(configDir); err != nil {
			return fmt.Errorf("instance %d: %w", index, err)
		}
		if _, exists := names[instance.Name]; exists {
			return fmt.Errorf("duplicate instance name %q", instance.Name)
		}
		names[instance.Name] = struct{}{}
	}
	if err := c.RuleBot.validate(configDir, c.Output); err != nil {
		return fmt.Errorf("rule_bot: %w", err)
	}
	return nil
}

func (c *RuleBotConfig) validate(configDir, output string) error {
	if !c.Enabled {
		return nil
	}
	parsed, err := url.Parse(c.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("endpoint scheme must be http or https")
	}
	if parsed.Host == "" || parsed.Path == "" || parsed.Path == "/" {
		return errors.New("endpoint must include a host and non-root hidden path")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("endpoint must not contain user information, a query, or a fragment")
	}
	c.Endpoint = parsed.String()
	if c.ProxyURL != "" && c.ProxyFromEnvironment {
		return errors.New("proxy_url and proxy_from_environment are mutually exclusive")
	}
	if c.ProxyURL != "" {
		proxyURL, err := url.Parse(c.ProxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy_url: %w", err)
		}
		switch proxyURL.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return errors.New("proxy_url scheme must be http, https, socks5, or socks5h")
		}
		if proxyURL.Host == "" || proxyURL.Path != "" || proxyURL.RawQuery != "" || proxyURL.Fragment != "" {
			return errors.New("proxy_url must contain only scheme, optional credentials, and host")
		}
		c.ProxyURL = proxyURL.String()
	}

	tokenSources := 0
	for _, source := range []string{c.Token, c.TokenFile, c.TokenEnv} {
		if source != "" {
			tokenSources++
		}
	}
	if tokenSources != 1 {
		return errors.New("exactly one of token, token_file, or token_env is required")
	}
	if c.TokenFile != "" && !filepath.IsAbs(c.TokenFile) {
		c.TokenFile = filepath.Join(configDir, c.TokenFile)
	}
	if c.StateFile == "" {
		c.StateFile = output + ".rulebot-state.json"
	} else if !filepath.IsAbs(c.StateFile) {
		c.StateFile = filepath.Join(configDir, c.StateFile)
	}
	c.StateFile = filepath.Clean(c.StateFile)
	if filepath.Clean(c.StateFile) == filepath.Clean(output) {
		return errors.New("state_file must differ from output")
	}
	for index, suffix := range c.Privacy.ExcludeSuffixes {
		normalized, ok := normalizeDomain(strings.TrimPrefix(strings.TrimSpace(suffix), "."), true)
		if !ok {
			return fmt.Errorf("privacy.exclude_suffixes[%d] is not a valid domain suffix", index)
		}
		c.Privacy.ExcludeSuffixes[index] = normalized
	}
	if c.Privacy.ExcludeFile != "" && !filepath.IsAbs(c.Privacy.ExcludeFile) {
		c.Privacy.ExcludeFile = filepath.Join(configDir, c.Privacy.ExcludeFile)
	}
	if c.Retry.InitialDelay.Value() == 0 {
		c.Retry.InitialDelay = Duration(time.Second)
	}
	if c.Retry.MaxDelay.Value() == 0 {
		c.Retry.MaxDelay = Duration(5 * time.Minute)
	}
	if c.Retry.InitialDelay.Value() <= 0 || c.Retry.MaxDelay.Value() <= 0 {
		return errors.New("retry delays must be greater than zero")
	}
	if c.Retry.InitialDelay.Value() > c.Retry.MaxDelay.Value() {
		return errors.New("retry initial_delay must not exceed max_delay")
	}
	return nil
}

func (c *InstanceConfig) validate(configDir string) error {
	if !validInstanceName(c.Name) {
		return fmt.Errorf("invalid name %q: use 1-64 ASCII letters, digits, dots, underscores, or hyphens", c.Name)
	}
	parsed, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return errors.New("URL host is required")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("URL must not contain user information, a query, or a fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return errors.New("URL must not contain a path")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	c.URL = strings.TrimSuffix(parsed.String(), "/")

	secretSources := 0
	for _, source := range []string{c.Secret, c.SecretFile, c.SecretEnv} {
		if source != "" {
			secretSources++
		}
	}
	if secretSources > 1 {
		return errors.New("secret, secret_file, and secret_env are mutually exclusive")
	}
	if c.SecretFile != "" && !filepath.IsAbs(c.SecretFile) {
		c.SecretFile = filepath.Join(configDir, c.SecretFile)
	}
	if c.TLS.CAFile != "" && !filepath.IsAbs(c.TLS.CAFile) {
		c.TLS.CAFile = filepath.Join(configDir, c.TLS.CAFile)
	}
	if parsed.Scheme != "https" && (c.TLS.CAFile != "" || c.TLS.ServerName != "" || c.TLS.InsecureSkipVerify) {
		return errors.New("TLS settings require an https URL")
	}
	if c.Reconnect.InitialDelay.Value() == 0 {
		c.Reconnect.InitialDelay = Duration(500 * time.Millisecond)
	}
	if c.Reconnect.MaxDelay.Value() == 0 {
		c.Reconnect.MaxDelay = Duration(30 * time.Second)
	}
	if c.Reconnect.InitialDelay.Value() <= 0 || c.Reconnect.MaxDelay.Value() <= 0 {
		return errors.New("reconnect delays must be greater than zero")
	}
	if c.Reconnect.InitialDelay.Value() > c.Reconnect.MaxDelay.Value() {
		return errors.New("reconnect initial_delay must not exceed max_delay")
	}
	return nil
}

func validInstanceName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, char := range []byte(name) {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}
