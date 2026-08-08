package client

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	maxInMemoryFingerprints = 64 * 1024
	fingerprintSlotSize     = int64(17)
	minimumFingerprintSlots = int64(128 * 1024)
)

type fingerprint [16]byte

type fingerprintSet struct {
	entries      map[fingerprint]struct{}
	table        *diskFingerprintTable
	cachePath    string
	capacityHint int
	length       int
	storageCheck func(string, int64) error
}

type diskFingerprintTable struct {
	file     *os.File
	path     string
	base     int64
	capacity int64
	count    int
}

func newFingerprintSet(capacity int) *fingerprintSet {
	return newFingerprintSetAt(capacity, "")
}

func newFingerprintSetAt(capacity int, cachePath string) *fingerprintSet {
	if capacity < 0 {
		capacity = 0
	}
	mapCapacity := capacity
	if mapCapacity > maxInMemoryFingerprints {
		mapCapacity = maxInMemoryFingerprints
	}
	return &fingerprintSet{
		entries:      make(map[fingerprint]struct{}, mapCapacity),
		cachePath:    cachePath,
		capacityHint: capacity,
		storageCheck: ensureStorageReserve,
	}
}

func (s *fingerprintSet) Add(domain string) (bool, error) {
	sum := sha256.Sum256([]byte(domain))
	var key fingerprint
	copy(key[:], sum[:len(key)])
	if s.table != nil {
		added, err := s.table.add(key, s.storageCheck)
		if added {
			s.length++
		}
		return added, err
	}
	if _, exists := s.entries[key]; exists {
		return false, nil
	}
	if len(s.entries) < maxInMemoryFingerprints {
		s.entries[key] = struct{}{}
		s.length++
		return true, nil
	}
	if err := s.spill(); err != nil {
		return false, err
	}
	added, err := s.table.add(key, s.storageCheck)
	if added {
		s.length++
	}
	return added, err
}

func (s *fingerprintSet) spill() error {
	expected := s.length + 1
	if s.capacityHint > expected {
		expected = s.capacityHint
	}
	capacity, err := fingerprintTableCapacity(expected)
	if err != nil {
		return err
	}
	table, err := createDiskFingerprintTable(s.cachePath, capacity, s.storageCheck)
	if err != nil {
		return fmt.Errorf("create deduplication cache: %w", err)
	}
	for key := range s.entries {
		if err := table.insertNew(key); err != nil {
			_ = table.closeAndRemove()
			return fmt.Errorf("populate deduplication cache: %w", err)
		}
	}
	s.table = table
	s.entries = nil
	return nil
}

func fingerprintTableCapacity(entries int) (int64, error) {
	if entries < 1 {
		entries = 1
	}
	if uint64(entries) > uint64(math.MaxInt64/10) {
		return 0, errors.New("deduplication cache is too large")
	}
	capacity := minimumFingerprintSlots
	for int64(entries)*10 > capacity*7 {
		if capacity > math.MaxInt64/2 {
			return 0, errors.New("deduplication cache is too large")
		}
		capacity *= 2
	}
	return capacity, nil
}

func createDiskFingerprintTable(path string, capacity int64, storageCheck func(string, int64) error) (*diskFingerprintTable, error) {
	var (
		file *os.File
		err  error
	)
	if path == "" {
		file, err = os.CreateTemp("", "rule-bot-client-dedupe-*")
		if err == nil {
			path = file.Name()
		}
	} else {
		if err = os.MkdirAll(filepath.Dir(path), 0o750); err == nil {
			file, err = os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
		}
	}
	if err != nil {
		return nil, err
	}
	fail := func(failure error) (*diskFingerprintTable, error) {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, failure
	}
	size, err := fingerprintTableBytes(capacity)
	if err != nil {
		return fail(err)
	}
	if err := storageCheck(path, size); err != nil {
		return fail(err)
	}
	if err := file.Truncate(size); err != nil {
		return fail(err)
	}
	return &diskFingerprintTable{file: file, path: path, capacity: capacity}, nil
}

func fingerprintTableBytes(capacity int64) (int64, error) {
	if capacity <= 0 || capacity > math.MaxInt64/fingerprintSlotSize {
		return 0, errors.New("deduplication cache is too large")
	}
	return capacity * fingerprintSlotSize, nil
}

func (t *diskFingerprintTable) add(key fingerprint, storageCheck func(string, int64) error) (bool, error) {
	found, slot, err := t.lookup(key)
	if err != nil || found {
		return false, err
	}
	if int64(t.count+1)*10 > t.capacity*7 {
		if err := t.grow(storageCheck); err != nil {
			return false, err
		}
		found, slot, err = t.lookup(key)
		if err != nil || found {
			return false, err
		}
	}
	if err := t.writeSlot(slot, key); err != nil {
		return false, err
	}
	t.count++
	return true, nil
}

func (t *diskFingerprintTable) insertNew(key fingerprint) error {
	found, slot, err := t.lookup(key)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	if err := t.writeSlot(slot, key); err != nil {
		return err
	}
	t.count++
	return nil
}

func (t *diskFingerprintTable) lookup(key fingerprint) (bool, int64, error) {
	start := int64(binary.LittleEndian.Uint64(key[:8]) & uint64(t.capacity-1))
	var slotData [fingerprintSlotSize]byte
	for probe := int64(0); probe < t.capacity; probe++ {
		slot := (start + probe) & (t.capacity - 1)
		if _, err := t.file.ReadAt(slotData[:], t.base+slot*fingerprintSlotSize); err != nil {
			return false, 0, fmt.Errorf("read deduplication cache: %w", err)
		}
		if slotData[0] == 0 {
			return false, slot, nil
		}
		var stored fingerprint
		copy(stored[:], slotData[1:])
		if stored == key {
			return true, slot, nil
		}
	}
	return false, 0, errors.New("deduplication cache is full")
}

func (t *diskFingerprintTable) writeSlot(slot int64, key fingerprint) error {
	var data [fingerprintSlotSize]byte
	data[0] = 1
	copy(data[1:], key[:])
	if _, err := t.file.WriteAt(data[:], t.base+slot*fingerprintSlotSize); err != nil {
		return fmt.Errorf("write deduplication cache: %w", err)
	}
	return nil
}

func (t *diskFingerprintTable) grow(storageCheck func(string, int64) error) error {
	if t.capacity > math.MaxInt64/2 {
		return errors.New("deduplication cache is too large")
	}
	newCapacity := t.capacity * 2
	additional, err := fingerprintTableBytes(newCapacity)
	if err != nil {
		return err
	}
	info, err := t.file.Stat()
	if err != nil {
		return fmt.Errorf("stat deduplication cache: %w", err)
	}
	if info.Size() > math.MaxInt64-additional {
		return errors.New("deduplication cache is too large")
	}
	if err := storageCheck(t.path, additional); err != nil {
		return err
	}
	newBase := info.Size()
	if err := t.file.Truncate(newBase + additional); err != nil {
		return fmt.Errorf("grow deduplication cache: %w", err)
	}
	next := diskFingerprintTable{file: t.file, path: t.path, base: newBase, capacity: newCapacity}
	var slotData [fingerprintSlotSize]byte
	for slot := int64(0); slot < t.capacity; slot++ {
		if _, err := t.file.ReadAt(slotData[:], t.base+slot*fingerprintSlotSize); err != nil {
			return fmt.Errorf("rehash deduplication cache: %w", err)
		}
		if slotData[0] == 0 {
			continue
		}
		var key fingerprint
		copy(key[:], slotData[1:])
		if err := next.insertNew(key); err != nil {
			return fmt.Errorf("rehash deduplication cache: %w", err)
		}
	}
	t.base = next.base
	t.capacity = next.capacity
	t.count = next.count
	return nil
}

func (s *fingerprintSet) Len() int { return s.length }

func (s *fingerprintSet) Close() error {
	if s.table == nil {
		return nil
	}
	err := s.table.closeAndRemove()
	s.table = nil
	return err
}

func (t *diskFingerprintTable) closeAndRemove() error {
	return errors.Join(t.file.Close(), os.Remove(t.path))
}
