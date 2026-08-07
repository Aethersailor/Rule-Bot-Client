package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRuleBotDomainMinimizesBeforeDelivery(t *testing.T) {
	config := RuleBotConfig{Privacy: RuleBotPrivacyConfig{
		ExcludeSuffixes: []string{"private.example"},
	}}
	tests := []struct {
		input  string
		want   string
		status string
	}{
		{input: "account.api.example.com", want: "example.com"},
		{input: "service.example.co.uk", want: "example.co.uk"},
		{input: "a.private.example", status: "excluded"},
		{input: "www.example.cn", status: "ignored_cn"},
		{input: "localhost", status: "invalid_domain"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, status, err := prepareRuleBotDomain(test.input, config)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || status != test.status {
				t.Fatalf("prepareRuleBotDomain() = %q, %q; want %q, %q", got, status, test.want, test.status)
			}
		})
	}
}

func TestPrepareRuleBotDomainReloadsExcludeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exclude.txt")
	if err := os.WriteFile(path, []byte("# local privacy list\n.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := RuleBotConfig{Privacy: RuleBotPrivacyConfig{ExcludeFile: path}}
	if domain, status, err := prepareRuleBotDomain("a.example.com", config); err != nil || domain != "" || status != "excluded" {
		t.Fatalf("excluded result = %q, %q, %v", domain, status, err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if domain, status, err := prepareRuleBotDomain("a.example.com", config); err != nil || domain != "example.com" || status != "" {
		t.Fatalf("reloaded result = %q, %q, %v", domain, status, err)
	}
}

func TestDomainReferenceDoesNotRevealInput(t *testing.T) {
	first := domainReference("Sensitive.Example")
	second := domainReference("sensitive.example")
	if first != second || len(first) != 12 || strings.Contains(first, "sensitive") {
		t.Fatalf("domain references = %q, %q", first, second)
	}
}
