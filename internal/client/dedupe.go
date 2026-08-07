package client

import "crypto/sha256"

type fingerprint [16]byte

type fingerprintSet struct {
	entries map[fingerprint]struct{}
}

func newFingerprintSet(capacity int) *fingerprintSet {
	return &fingerprintSet{entries: make(map[fingerprint]struct{}, capacity)}
}

func (s *fingerprintSet) Add(domain string) bool {
	sum := sha256.Sum256([]byte(domain))
	var key fingerprint
	copy(key[:], sum[:len(key)])
	if _, exists := s.entries[key]; exists {
		return false
	}
	s.entries[key] = struct{}{}
	return true
}

func (s *fingerprintSet) Len() int { return len(s.entries) }
