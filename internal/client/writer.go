package client

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const (
	outputBufferSize   = 64 * 1024
	storageCheckWindow = int64(1024 * 1024)
)

type outputStore struct {
	file          *os.File
	buffered      *bufio.Writer
	interval      time.Duration
	durable       atomic.Int64
	dirty         bool
	seen          *fingerprintSet
	storageCheck  func(string, int64) error
	storageBudget int64
}

func openOutput(path string, interval time.Duration, configuredCachePath ...string) (*outputStore, *fingerprintSet, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, nil, fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open output: %w", err)
	}
	var openedSet *fingerprintSet
	fail := func(err error) (*outputStore, *fingerprintSet, error) {
		if openedSet != nil {
			_ = openedSet.Close()
		}
		_ = file.Close()
		return nil, nil, err
	}
	if err := lockFile(file); err != nil {
		return fail(err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = unlockFile(file)
		return fail(fmt.Errorf("secure output permissions: %w", err))
	}
	cachePath := path + ".dedupe-cache"
	if len(configuredCachePath) != 0 && configuredCachePath[0] != "" {
		cachePath = configuredCachePath[0]
	}
	set, repairTail, err := loadExistingDomains(file, cachePath)
	if err != nil {
		_ = unlockFile(file)
		return fail(err)
	}
	openedSet = set
	if repairTail {
		if _, err := file.WriteString("\n"); err != nil {
			_ = unlockFile(file)
			return fail(fmt.Errorf("repair output tail: %w", err))
		}
		if err := file.Sync(); err != nil {
			_ = unlockFile(file)
			return fail(fmt.Errorf("synchronize repaired output: %w", err))
		}
	}
	store := &outputStore{
		file:         file,
		buffered:     bufio.NewWriterSize(file, outputBufferSize),
		interval:     interval,
		seen:         set,
		storageCheck: ensureStorageReserve,
	}
	info, err := file.Stat()
	if err != nil {
		_ = unlockFile(file)
		return fail(fmt.Errorf("stat output: %w", err))
	}
	store.durable.Store(info.Size())
	return store, set, nil
}

func loadExistingDomains(file *os.File, cachePath string) (*fingerprintSet, bool, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, false, fmt.Errorf("seek output: %w", err)
	}
	set := newFingerprintSetAt(1024, cachePath)
	fail := func(err error) (*fingerprintSet, bool, error) {
		_ = set.Close()
		return nil, false, err
	}
	reader := bufio.NewReaderSize(file, 4096)
	scratch := make([]byte, 0, 512)
	lineNumber := 0
	repairTail := false
	for {
		line, err := readBoundedLine(reader, &scratch, 4096)
		if len(line) != 0 {
			lineNumber++
			domain, ok := normalizeDomain(string(line), true)
			if !ok {
				return fail(fmt.Errorf("output line %d is not a valid domain", lineNumber))
			}
			if _, addErr := set.Add(domain); addErr != nil {
				return fail(fmt.Errorf("index output line %d: %w", lineNumber, addErr))
			}
		}
		if errors.Is(err, errLineTooLong) {
			return fail(fmt.Errorf("output line %d exceeds 4096 bytes", lineNumber+1))
		}
		if errors.Is(err, io.EOF) {
			repairTail = len(line) != 0
			break
		}
		if err != nil {
			return fail(fmt.Errorf("read output: %w", err))
		}
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return fail(fmt.Errorf("seek output end: %w", err))
	}
	return set, repairTail, nil
}

func (s *outputStore) Run(domains <-chan string) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case domain, open := <-domains:
			if !open {
				return s.flushAndSync()
			}
			if strings.ContainsAny(domain, "\r\n") {
				return errors.New("refusing to write a domain containing a line break")
			}
			line := domain + "\n"
			growth := int64(len(line))
			if s.storageBudget < growth {
				window := storageCheckWindow
				if growth > window {
					window = growth
				}
				if err := s.storageCheck(s.file.Name(), window); err != nil {
					return fmt.Errorf("preserve output storage reserve: %w", err)
				}
				s.storageBudget = window
			}
			s.storageBudget -= growth
			if _, err := s.buffered.WriteString(line); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			s.dirty = true
		case <-ticker.C:
			if err := s.flushAndSync(); err != nil {
				return err
			}
		}
	}
}

func (s *outputStore) flushAndSync() error {
	if !s.dirty {
		return nil
	}
	if err := s.buffered.Flush(); err != nil {
		return fmt.Errorf("flush output: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("synchronize output: %w", err)
	}
	info, err := s.file.Stat()
	if err != nil {
		return fmt.Errorf("stat synchronized output: %w", err)
	}
	s.durable.Store(info.Size())
	s.dirty = false
	return nil
}

func (s *outputStore) DurableSize() int64 {
	return s.durable.Load()
}

func (s *outputStore) Close() error {
	unlockErr := unlockFile(s.file)
	closeErr := s.file.Close()
	return errors.Join(unlockErr, closeErr, s.seen.Close())
}
