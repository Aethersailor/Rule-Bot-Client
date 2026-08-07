package client

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxExcludeFileSize = 1024 * 1024

var domainLogKey = func() [32]byte {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		panic(fmt.Sprintf("initialize privacy log key: %v", err))
	}
	return key
}()

func domainReference(domain string) string {
	digest := hmac.New(sha256.New, domainLogKey[:])
	_, _ = digest.Write([]byte(strings.ToLower(strings.TrimSpace(domain))))
	return hex.EncodeToString(digest.Sum(nil)[:6])
}

func prepareRuleBotDomain(raw string, cfg RuleBotConfig) (string, string, error) {
	domain, ok := normalizeDomain(raw, true)
	if !ok {
		return "", "invalid_domain", nil
	}
	exclusions, err := loadRuleBotExclusions(cfg.Privacy)
	if err != nil {
		return "", "", err
	}
	if isCNDomain(domain) {
		return "", "ignored_cn", nil
	}
	if matchesDomainSuffix(domain, exclusions) {
		return "", "excluded", nil
	}
	if !cfg.Privacy.reduceToRegistrableDomain() {
		return domain, "", nil
	}
	registrable, ok := projectDomain(domain, DomainModeRegistrableDomain)
	if !ok {
		return "", "invalid_domain", nil
	}
	if isCNDomain(registrable) {
		return "", "ignored_cn", nil
	}
	if matchesDomainSuffix(registrable, exclusions) {
		return "", "excluded", nil
	}
	return registrable, "", nil
}

func isCNDomain(domain string) bool {
	return domain == "cn" || strings.HasSuffix(domain, ".cn")
}

func matchesDomainSuffix(domain string, exclusions map[string]struct{}) bool {
	for suffix := range exclusions {
		if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
			return true
		}
	}
	return false
}

func loadRuleBotExclusions(cfg RuleBotPrivacyConfig) (map[string]struct{}, error) {
	exclusions := make(map[string]struct{}, len(cfg.ExcludeSuffixes))
	for _, suffix := range cfg.ExcludeSuffixes {
		exclusions[suffix] = struct{}{}
	}
	if cfg.ExcludeFile == "" {
		return exclusions, nil
	}
	file, err := os.Open(cfg.ExcludeFile)
	if err != nil {
		return nil, fmt.Errorf("open privacy.exclude_file: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, maxExcludeFileSize+1))
	lineNumber := 0
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) != 0 {
			lineNumber++
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				normalized, ok := normalizeDomain(strings.TrimPrefix(trimmed, "."), true)
				if !ok {
					return nil, fmt.Errorf("privacy.exclude_file line %d is not a valid domain suffix", lineNumber)
				}
				exclusions[normalized] = struct{}{}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read privacy.exclude_file: %w", readErr)
		}
	}
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat privacy.exclude_file: %w", err)
	}
	if info.Size() > maxExcludeFileSize {
		return nil, fmt.Errorf("privacy.exclude_file exceeds %d bytes", maxExcludeFileSize)
	}
	return exclusions, nil
}
