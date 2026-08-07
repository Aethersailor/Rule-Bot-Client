package client

import "testing"

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		includeSingle bool
		want          string
		ok            bool
	}{
		{name: "lowercase root", input: " Example.COM. ", want: "example.com", ok: true},
		{name: "punycode", input: "XN--BCHER-KVA.EXAMPLE", want: "xn--bcher-kva.example", ok: true},
		{name: "IPv4", input: "192.0.2.1"},
		{name: "IPv6", input: "2001:db8::1"},
		{name: "single excluded", input: "nas"},
		{name: "single included", input: "NAS", includeSingle: true, want: "nas", ok: true},
		{name: "empty label", input: "a..example"},
		{name: "bad character", input: "bad/name.example"},
		{name: "Unicode IDN", input: "BÜCHER.Example.", want: "xn--bcher-kva.example", ok: true},
		{name: "Unicode dot", input: "bücher。example", want: "xn--bcher-kva.example", ok: true},
		{name: "underscore", input: "_service.example", want: "_service.example", ok: true},
		{name: "leading hyphen", input: "-bad.example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := normalizeDomain(test.input, test.includeSingle)
			if got != test.want || ok != test.ok {
				t.Fatalf("normalizeDomain(%q) = %q, %v; want %q, %v", test.input, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestProjectDomain(t *testing.T) {
	tests := []struct {
		name  string
		input string
		mode  DomainMode
		want  string
		ok    bool
	}{
		{name: "compatibility hostname", input: "update.windhawk.net", mode: DomainModeHostname, want: "update.windhawk.net", ok: true},
		{name: "windhawk registrable", input: "update.windhawk.net", mode: DomainModeRegistrableDomain, want: "windhawk.net", ok: true},
		{name: "lencr registrable", input: "yr2.c.lencr.org", mode: DomainModeRegistrableDomain, want: "lencr.org", ok: true},
		{name: "amazontrust registrable", input: "crt.rootg2.amazontrust.com", mode: DomainModeRegistrableDomain, want: "amazontrust.com", ok: true},
		{name: "letsencrypt registrable", input: "acme-v02.api.letsencrypt.org", mode: DomainModeRegistrableDomain, want: "letsencrypt.org", ok: true},
		{name: "multi label public suffix", input: "service.example.co.uk", mode: DomainModeRegistrableDomain, want: "example.co.uk", ok: true},
		{name: "private suffix tenant", input: "assets.alice.github.io", mode: DomainModeRegistrableDomain, want: "alice.github.io", ok: true},
		{name: "IDN", input: "xn--bcher-kva.de", mode: DomainModeRegistrableDomain, want: "xn--bcher-kva.de", ok: true},
		{name: "public suffix only", input: "co.uk", mode: DomainModeRegistrableDomain},
		{name: "private suffix only", input: "github.io", mode: DomainModeRegistrableDomain},
		{name: "localhost", input: "localhost", mode: DomainModeRegistrableDomain},
		{name: "invalid underscore", input: "bad_name.example.com", mode: DomainModeRegistrableDomain},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := projectDomain(test.input, test.mode)
			if got != test.want || ok != test.ok {
				t.Fatalf("projectDomain(%q, %q) = %q, %v; want %q, %v", test.input, test.mode, got, ok, test.want, test.ok)
			}
		})
	}
}
