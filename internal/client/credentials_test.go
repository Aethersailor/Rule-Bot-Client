package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestControllerRequestsUseEveryCredentialSource(t *testing.T) {
	directory := t.TempDir()
	secretFile := filepath.Join(directory, "controller.secret")
	if err := os.WriteFile(secretFile, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const secretEnv = "RULE_BOT_CLIENT_TEST_CONTROLLER_HEADER_SECRET"
	t.Setenv(secretEnv, "environment-secret")

	tests := []struct {
		name      string
		configure func(*InstanceConfig)
		want      string
	}{
		{
			name: "inline",
			configure: func(cfg *InstanceConfig) {
				cfg.Secret = "inline-secret"
			},
			want: "inline-secret",
		},
		{
			name: "file",
			configure: func(cfg *InstanceConfig) {
				cfg.SecretFile = secretFile
			},
			want: "file-secret",
		},
		{
			name: "environment",
			configure: func(cfg *InstanceConfig) {
				cfg.SecretEnv = secretEnv
			},
			want: "environment-secret",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if got := request.Header.Get("Authorization"); got != "Bearer "+test.want {
					http.Error(writer, "unauthorized", http.StatusUnauthorized)
					return
				}
				writer.Header().Set("Content-Type", "application/json")
				io.WriteString(writer, `{"connections":[]}`)
			}))
			defer server.Close()

			cfg := InstanceConfig{Name: test.name, URL: server.URL}
			test.configure(&cfg)
			instance, err := buildInstance(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.close()
			response, err := instance.request(context.Background(), "/connections", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("controller returned %s", response.Status)
			}
		})
	}
}

func TestRuleBotRequestsUseEveryCredentialSource(t *testing.T) {
	directory := t.TempDir()
	tokenFile := filepath.Join(directory, "rulebot.token")
	if err := os.WriteFile(tokenFile, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const tokenEnv = "RULE_BOT_CLIENT_TEST_RULEBOT_HEADER_TOKEN"
	t.Setenv(tokenEnv, "environment-token")

	tests := []struct {
		name      string
		configure func(*RuleBotConfig)
		want      string
	}{
		{
			name: "inline",
			configure: func(cfg *RuleBotConfig) {
				cfg.Token = "inline-token"
			},
			want: "inline-token",
		},
		{
			name: "file",
			configure: func(cfg *RuleBotConfig) {
				cfg.TokenFile = tokenFile
			},
			want: "file-token",
		},
		{
			name: "environment",
			configure: func(cfg *RuleBotConfig) {
				cfg.TokenEnv = tokenEnv
			},
			want: "environment-token",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if got := request.Header.Get("Authorization"); got != "Bearer "+test.want {
					http.Error(writer, "unauthorized", http.StatusUnauthorized)
					return
				}
				writer.Header().Set("Content-Type", "application/json")
				io.WriteString(writer, `{"version":1,"status":"exists_rules"}`)
			}))
			defer server.Close()

			cfg := RuleBotConfig{Enabled: true, Endpoint: fmt.Sprintf("%s/hidden", server.URL)}
			test.configure(&cfg)
			sender := &ruleBotSender{config: cfg, client: server.Client()}
			status, err := sender.deliver(context.Background(), "example.com")
			if err != nil {
				t.Fatal(err)
			}
			if status != "exists_rules" {
				t.Fatalf("delivery status = %q", status)
			}
		})
	}
}
