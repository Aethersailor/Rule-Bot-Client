package client

import (
	"net/netip"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

func normalizeDomain(raw string, includeSingleLabel bool) (string, bool) {
	domain, ok := asciiDomain(strings.TrimSpace(raw))
	if !ok {
		return "", false
	}
	domain = strings.TrimSuffix(domain, ".")
	domain = strings.ToLower(domain)
	if domain == "" || len(domain) > 253 {
		return "", false
	}
	if _, err := netip.ParseAddr(domain); err == nil {
		return "", false
	}
	labels := strings.Split(domain, ".")
	if len(labels) == 1 && !includeSingleLabel {
		return "", false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for index := 0; index < len(label); index++ {
			char := label[index]
			if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
				continue
			}
			return "", false
		}
	}
	return domain, true
}

func asciiDomain(domain string) (string, bool) {
	if domain == "" {
		return "", false
	}
	if isASCII(domain) {
		return domain, true
	}
	ascii, err := idna.Lookup.ToASCII(domain)
	return ascii, err == nil
}

func isASCII(value string) bool {
	for index := range len(value) {
		if value[index] >= 0x80 {
			return false
		}
	}
	return true
}

func projectDomain(domain string, mode DomainMode) (string, bool) {
	if mode == DomainModeHostname {
		return domain, true
	}
	canonical, err := idna.Lookup.ToASCII(domain)
	if err != nil {
		return "", false
	}
	registrable, err := publicsuffix.EffectiveTLDPlusOne(canonical)
	if err != nil {
		return "", false
	}
	return strings.ToLower(registrable), true
}
