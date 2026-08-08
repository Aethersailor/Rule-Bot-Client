package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFingerprintSet(t *testing.T) {
	set := newFingerprintSetAt(1, filepath.Join(t.TempDir(), "dedupe-cache"))
	defer set.Close()
	if added, err := set.Add("example.com"); err != nil || !added {
		t.Fatal("first Add() = false")
	}
	if added, err := set.Add("example.com"); err != nil || added {
		t.Fatal("duplicate Add() = true")
	}
	if added, err := set.Add("other.example"); err != nil || !added || set.Len() != 2 {
		t.Fatalf("Len() = %d", set.Len())
	}
}

func TestFingerprintSetSpillsExactlyAndCleansCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "dedupe-cache")
	set := newFingerprintSetAt(maxInMemoryFingerprints+1, cachePath)
	for index := 0; index <= maxInMemoryFingerprints; index++ {
		if added, err := set.Add(fmt.Sprintf("domain-%d.example", index)); err != nil || !added {
			t.Fatalf("Add(%d) = %t, %v", index, added, err)
		}
	}
	if added, err := set.Add("domain-123.example"); err != nil || added {
		t.Fatalf("spilled duplicate Add() = %t, %v", added, err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("spill cache was not created: %v", err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("spill cache survived Close(): %v", err)
	}
}

func BenchmarkFingerprintSetAdd(b *testing.B) {
	b.StopTimer()
	domains := make([]string, b.N)
	for index := range domains {
		domains[index] = fmt.Sprintf("domain-%d.example", index)
	}
	set := newFingerprintSet(b.N)
	defer set.Close()
	b.ReportAllocs()
	b.StartTimer()
	for index := 0; index < b.N; index++ {
		if _, err := set.Add(domains[index]); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFingerprintSetMemoryGate(t *testing.T) {
	if testing.Short() {
		t.Skip("performance gate")
	}
	for _, test := range []struct {
		name    string
		entries int
		limit   uint64
	}{
		{name: "100k", entries: 100_000, limit: 6 * 1024 * 1024},
		{name: "1m", entries: 1_000_000, limit: 12 * 1024 * 1024},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)

			set := newFingerprintSetAt(test.entries, filepath.Join(t.TempDir(), "dedupe-cache"))
			defer set.Close()
			for index := range test.entries {
				if _, err := set.Add(fmt.Sprintf("domain-%d.example", index)); err != nil {
					t.Fatal(err)
				}
			}
			runtime.GC()
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(set)

			var used uint64
			if after.HeapAlloc > before.HeapAlloc {
				used = after.HeapAlloc - before.HeapAlloc
			}
			t.Logf("%d-domain set retained %d bytes", test.entries, used)
			if used > test.limit {
				t.Fatalf("%d-domain set retained %d bytes; limit is %d", test.entries, used, test.limit)
			}
		})
	}
}
