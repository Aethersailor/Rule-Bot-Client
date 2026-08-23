package client

import (
	"encoding/json"
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
	if cfg.AutoUpdate {
		t.Fatal("AutoUpdate default is true")
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

func TestBundledConfigurationsUseInlineCredentialsAndValidate(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	for _, relativePath := range []string{
		"config.example.json",
		filepath.Join("deploy", "docker", "config.json"),
		filepath.Join("deploy", "systemd", "config.json"),
	} {
		t.Run(filepath.ToSlash(relativePath), func(t *testing.T) {
			path := filepath.Join(repositoryRoot, relativePath)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			for index, instance := range cfg.Instances {
				if instance.Secret == "" || instance.SecretFile != "" || instance.SecretEnv != "" {
					t.Fatalf("instances[%d] does not use one inline secret", index)
				}
			}
			if cfg.RuleBot.Token == "" || cfg.RuleBot.TokenFile != "" || cfg.RuleBot.TokenEnv != "" {
				t.Fatal("rule_bot does not use one inline token")
			}
			if cfg.RuleBot.SendExisting {
				t.Fatal("rule_bot.send_existing must default to false")
			}

			var document map[string]any
			if err := json.Unmarshal(contents, &document); err != nil {
				t.Fatal(err)
			}
			ruleBot, ok := document["rule_bot"].(map[string]any)
			if !ok {
				t.Fatal("example rule_bot is not an object")
			}
			ruleBot["enabled"] = true
			enabled, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			enabledPath := writeTestConfig(t, t.TempDir(), string(enabled))
			enabledConfig, err := LoadConfig(enabledPath)
			if err != nil {
				t.Fatalf("enabled example LoadConfig() error = %v", err)
			}
			if err := CheckConfig(enabledConfig); err != nil {
				t.Fatalf("enabled example CheckConfig() error = %v", err)
			}
		})
	}
}

func TestResolveSecretSupportsEveryCredentialSource(t *testing.T) {
	directory := t.TempDir()
	secretPath := filepath.Join(directory, "controller.secret")
	if err := os.WriteFile(secretPath, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const environmentVariable = "RULE_BOT_CLIENT_TEST_SECRET_SOURCE"
	t.Setenv(environmentVariable, "environment-secret")

	tests := []struct {
		name string
		cfg  InstanceConfig
		want string
	}{
		{name: "none", cfg: InstanceConfig{}, want: ""},
		{name: "inline", cfg: InstanceConfig{Secret: "inline-secret"}, want: "inline-secret"},
		{name: "file", cfg: InstanceConfig{SecretFile: secretPath}, want: "file-secret"},
		{name: "environment", cfg: InstanceConfig{SecretEnv: environmentVariable}, want: "environment-secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveSecret(test.cfg)
			if err != nil {
				t.Fatalf("resolveSecret() error = %v", err)
			}
			if got != test.want {
				t.Fatal("resolveSecret() selected the wrong credential source")
			}
		})
	}
}

func TestResolveSecretRejectsEveryCredentialSourceConflict(t *testing.T) {
	directory := t.TempDir()
	secretPath := filepath.Join(directory, "controller.secret")
	const fileSecret = "file-secret-marker"
	if err := os.WriteFile(secretPath, []byte(fileSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	const environmentVariable = "RULE_BOT_CLIENT_TEST_SECRET_CONFLICT"
	const environmentSecret = "environment-secret-marker"
	t.Setenv(environmentVariable, environmentSecret)
	const inlineSecret = "inline-secret-marker"

	tests := map[string]InstanceConfig{
		"inline and file":        {Secret: inlineSecret, SecretFile: secretPath},
		"inline and environment": {Secret: inlineSecret, SecretEnv: environmentVariable},
		"file and environment":   {SecretFile: secretPath, SecretEnv: environmentVariable},
		"all sources":            {Secret: inlineSecret, SecretFile: secretPath, SecretEnv: environmentVariable},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := resolveSecret(cfg)
			if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("resolveSecret() error = %v", err)
			}
			for _, credential := range []string{inlineSecret, fileSecret, environmentSecret} {
				if strings.Contains(err.Error(), credential) {
					t.Fatal("resolveSecret() error exposed a credential value")
				}
			}
		})
	}
}

func TestResolveSecretErrorsDoNotExposeCredentialValues(t *testing.T) {
	directory := t.TempDir()
	secretPath := filepath.Join(directory, "controller.secret")
	const credentialMarker = "controller-secret-marker"
	if err := os.WriteFile(secretPath, []byte(credentialMarker+"\ninvalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	const environmentVariable = "RULE_BOT_CLIENT_TEST_UNSAFE_SECRET"
	t.Setenv(environmentVariable, credentialMarker+"\ninvalid")

	tests := map[string]InstanceConfig{
		"inline":      {Secret: credentialMarker + "\ninvalid"},
		"file":        {SecretFile: secretPath},
		"environment": {SecretEnv: environmentVariable},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := resolveSecret(cfg)
			if err == nil {
				t.Fatal("resolveSecret() succeeded")
			}
			if strings.Contains(err.Error(), credentialMarker) {
				t.Fatal("resolveSecret() error exposed a credential value")
			}
		})
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

func TestLoadConfigRejectsEveryCredentialSourceConflict(t *testing.T) {
	const inlineMarker = "inline-credential-marker"
	tests := map[string]string{
		"secret inline and file":        `"secret":"` + inlineMarker + `","secret_file":"controller.secret"`,
		"secret inline and environment": `"secret":"` + inlineMarker + `","secret_env":"CONTROLLER_SECRET"`,
		"secret file and environment":   `"secret_file":"controller.secret","secret_env":"CONTROLLER_SECRET"`,
		"secret all sources":            `"secret":"` + inlineMarker + `","secret_file":"controller.secret","secret_env":"CONTROLLER_SECRET"`,
		"token inline and file":         `"token":"` + inlineMarker + `","token_file":"rulebot.token"`,
		"token inline and environment":  `"token":"` + inlineMarker + `","token_env":"RULE_BOT_TOKEN"`,
		"token file and environment":    `"token_file":"rulebot.token","token_env":"RULE_BOT_TOKEN"`,
		"token all sources":             `"token":"` + inlineMarker + `","token_file":"rulebot.token","token_env":"RULE_BOT_TOKEN"`,
	}
	for name, credentialFields := range tests {
		t.Run(name, func(t *testing.T) {
			var input string
			if strings.HasPrefix(name, "secret ") {
				input = `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1",` + credentialFields + `}]}`
			} else {
				input = `{"version":1,"output":"x","instances":[{"name":"a","url":"http://127.0.0.1"}],"rule_bot":{"enabled":true,"endpoint":"https://rule-bot.example/hidden",` + credentialFields + `}}`
			}
			_, err := LoadConfig(writeTestConfig(t, t.TempDir(), input))
			if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if strings.Contains(err.Error(), inlineMarker) {
				t.Fatal("LoadConfig() error exposed a credential value")
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
		"missing token":     `{"enabled":true,"endpoint":"https://rule-bot.example/hidden"}`,
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

func TestRuleBotURLValidationErrorsDoNotEchoPrivateAddresses(t *testing.T) {
	for name, ruleBot := range map[string]string{
		"endpoint": `{"enabled":true,"endpoint":"https://private-rule-bot.example/private/%zz","token":"x"}`,
		"proxy":    `{"enabled":true,"endpoint":"https://rule-bot.example/hidden","token":"x","proxy_url":"http://proxy-user:proxy-password@private-proxy.example/%zz"}`,
	} {
		t.Run(name, func(t *testing.T) {
			input := `{"version":1,"output":"domains.txt","instances":[{"name":"home","url":"http://127.0.0.1:9090"}],"rule_bot":` + ruleBot + `}`
			path := writeTestConfig(t, t.TempDir(), input)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("LoadConfig() succeeded")
			}
			if strings.Contains(err.Error(), "private-rule-bot.example") || strings.Contains(err.Error(), "private-proxy.example") || strings.Contains(err.Error(), "proxy-password") {
				t.Fatalf("validation error exposed private address: %v", err)
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
