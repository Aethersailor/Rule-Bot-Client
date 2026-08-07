package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaultsAndRelativePaths(t *testing.T) {
	directory := t.TempDir()
	secretPath := filepath.Join(directory, "controller.secret")
	if err := os.WriteFile(secretPath, []byte("secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeTestConfig(t, directory, `{
  "version": 1,
  "output": "domains.txt",
  "instances": [{"name":"home","url":"http://127.0.0.1:9090/","secret_file":"controller.secret"}]
}`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Output != filepath.Join(directory, "domains.txt") {
		t.Fatalf("Output = %q", cfg.Output)
	}
	if got := cfg.FlushInterval.Value(); got != 5*time.Second {
		t.Fatalf("FlushInterval = %s", got)
	}
	if cfg.DomainMode != DomainModeHostname {
		t.Fatalf("DomainMode = %q", cfg.DomainMode)
	}
	if !cfg.IncludeFailedConnections {
		t.Fatal("IncludeFailedConnections default is false")
	}
	if cfg.Instances[0].URL != "http://127.0.0.1:9090" {
		t.Fatalf("URL = %q", cfg.Instances[0].URL)
	}
	if cfg.Instances[0].SecretFile != secretPath {
		t.Fatalf("SecretFile = %q", cfg.Instances[0].SecretFile)
	}
	if err := CheckConfig(cfg); err != nil {
		t.Fatalf("CheckConfig() error = %v", err)
	}
}

func TestLoadConfigRejectsInvalidInput(t *testing.T) {
	tests := map[string]string{
		"unknown field":    `{"version":1,"output":"x","extra":true,"instances":[{"name":"a","url":"http://127.0.0.1"}]}`,
		"wrong version":    `{"version":2,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1"}]}`,
		"no output":        `{"version":1,"instances":[{"name":"a","url":"http://127.0.0.1"}]}`,
		"no instances":     `{"version":1,"output":"x","instances":[]}`,
		"duplicate name":   `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1"},{"name":"a","url":"http://127.0.0.2"}]}`,
		"URL path":         `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1/api"}]}`,
		"URL query":        `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1?token=x"}]}`,
		"bad scheme":       `{"version":1,"output":"x","instances":[{"name":"a","url":"ftp://127.0.0.1"}]}`,
		"multiple secrets": `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1","secret":"x","secret_env":"Y"}]}`,
		"TLS over HTTP":    `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1","tls":{"server_name":"x"}}]}`,
		"bad reconnect":    `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1","reconnect":{"initial_delay":"2s","max_delay":"1s"}}]}`,
		"bad domain mode":  `{"version":1,"output":"x","domain_mode":"last_two_labels","instances":[{"name":"a","url":"http://127.0.0.1"}]}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			path := writeTestConfig(t, t.TempDir(), input)
			if _, err := LoadConfig(path); err == nil {
				t.Fatal("LoadConfig() succeeded, want error")
			}
		})
	}
}

func TestCheckConfigRejectsMissingSecretEnvironment(t *testing.T) {
	const variable = "RULE_BOT_CLIENT_TEST_MISSING_SECRET"
	t.Setenv(variable, "")
	if err := os.Unsetenv(variable); err != nil {
		t.Fatal(err)
	}
	path := writeTestConfig(t, t.TempDir(), `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1","secret_env":"`+variable+`"}]}`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckConfig(cfg); err == nil || !strings.Contains(err.Error(), "is not set") {
		t.Fatalf("CheckConfig() error = %v", err)
	}
}

func TestCheckConfigRejectsUnsafeSecrets(t *testing.T) {
	for name, secret := range map[string]string{
		"empty":   "",
		"newline": "secret\nvalue",
	} {
		t.Run(name, func(t *testing.T) {
			variable := "RULE_BOT_CLIENT_TEST_SECRET_" + strings.ToUpper(name)
			t.Setenv(variable, secret)
			path := writeTestConfig(t, t.TempDir(), `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1","secret_env":"`+variable+`"}]}`)
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := CheckConfig(cfg); err == nil {
				t.Fatal("CheckConfig() succeeded")
			}
		})
	}
}

func TestLoadConfigValidatesOptionalRuleBotDelivery(t *testing.T) {
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "rulebot.token")
	if err := os.WriteFile(tokenPath, []byte("token-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(directory, "exclude.txt")
	if err := os.WriteFile(excludePath, []byte("private.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeTestConfig(t, directory, `{
  "version":1,
  "output":"domains.txt",
  "instances":[{"name":"home","url":"http://127.0.0.1:9090"}],
  "rule_bot":{
    "enabled":true,
    "endpoint":"https://rule-bot.example/api/hidden/path",
    "token_file":"rulebot.token",
    "state_file":"rulebot-state.json",
    "proxy_url":"socks5h://127.0.0.1:7891",
    "privacy":{"exclude_file":"exclude.txt","exclude_suffixes":[".local.example"]}
  }
}`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuleBot.TokenFile != tokenPath {
		t.Fatalf("TokenFile = %q", cfg.RuleBot.TokenFile)
	}
	if cfg.RuleBot.StateFile != filepath.Join(directory, "rulebot-state.json") {
		t.Fatalf("StateFile = %q", cfg.RuleBot.StateFile)
	}
	if cfg.RuleBot.Privacy.ExcludeFile != excludePath {
		t.Fatalf("ExcludeFile = %q", cfg.RuleBot.Privacy.ExcludeFile)
	}
	if !cfg.RuleBot.Privacy.reduceToRegistrableDomain() {
		t.Fatal("ReduceToRegistrableDomain default is false")
	}
	if cfg.RuleBot.Retry.InitialDelay.Value() != time.Second || cfg.RuleBot.Retry.MaxDelay.Value() != 5*time.Minute {
		t.Fatalf("Retry = %+v", cfg.RuleBot.Retry)
	}
	if err := CheckConfig(cfg); err != nil {
		t.Fatalf("CheckConfig() error = %v", err)
	}
}

func TestLoadConfigRejectsUnsafeRuleBotConfiguration(t *testing.T) {
	tests := map[string]string{
		"root endpoint":     `{"enabled":true,"endpoint":"https://rule-bot.example/","token":"x"}`,
		"endpoint query":    `{"enabled":true,"endpoint":"https://rule-bot.example/hidden?q=x","token":"x"}`,
		"multiple tokens":   `{"enabled":true,"endpoint":"https://rule-bot.example/hidden","token":"x","token_env":"TOKEN"}`,
		"multiple proxies":  `{"enabled":true,"endpoint":"https://rule-bot.example/hidden","token":"x","proxy_url":"http://127.0.0.1:8080","proxy_from_environment":true}`,
		"bad proxy scheme":  `{"enabled":true,"endpoint":"https://rule-bot.example/hidden","token":"x","proxy_url":"ftp://127.0.0.1:21"}`,
		"proxy path":        `{"enabled":true,"endpoint":"https://rule-bot.example/hidden","token":"x","proxy_url":"http://127.0.0.1:8080/path"}`,
		"invalid exclusion": `{"enabled":true,"endpoint":"https://rule-bot.example/hidden","token":"x","privacy":{"exclude_suffixes":["bad domain"]}}`,
	}
	for name, ruleBot := range tests {
		t.Run(name, func(t *testing.T) {
			input := `{"version":1,"output":"domains.txt","instances":[{"name":"home","url":"http://127.0.0.1:9090"}],"rule_bot":` + ruleBot + `}`
			path := writeTestConfig(t, t.TempDir(), input)
			if _, err := LoadConfig(path); err == nil {
				t.Fatal("LoadConfig() succeeded")
			}
		})
	}
}

func writeTestConfig(t *testing.T, directory, contents string) string {
	t.Helper()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
