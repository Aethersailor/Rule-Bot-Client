package client

import (
	"fmt"
	"runtime"
	"testing"
)

func TestFingerprintSet(t *testing.T) {
	set := newFingerprintSet(1)
	if !set.Add("example.com") {
		t.Fatal("first Add() = false")
	}
	if set.Add("example.com") {
		t.Fatal("duplicate Add() = true")
	}
	if !set.Add("other.example") || set.Len() != 2 {
		t.Fatalf("Len() = %d", set.Len())
	}
}

func BenchmarkFingerprintSetAdd(b *testing.B) {
	b.StopTimer()
	domains := make([]string, b.N)
	for index := range domains {
		domains[index] = fmt.Sprintf("domain-%d.example", index)
	}
	set := newFingerprintSet(b.N)
	b.ReportAllocs()
	b.StartTimer()
	for index := 0; index < b.N; index++ {
		set.Add(domains[index])
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
		{name: "1m", entries: 1_000_000, limit: 40 * 1024 * 1024},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)

			set := newFingerprintSet(test.entries)
			for index := range test.entries {
				set.Add(fmt.Sprintf("domain-%d.example", index))
			}
			runtime.GC()
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			runtime.KeepAlive(set)

			if after.HeapAlloc < before.HeapAlloc {
				t.Fatalf("heap accounting moved backwards: before=%d after=%d", before.HeapAlloc, after.HeapAlloc)
			}
			used := after.HeapAlloc - before.HeapAlloc
			t.Logf("%d-domain set retained %d bytes", test.entries, used)
			if used > test.limit {
				t.Fatalf("%d-domain set retained %d bytes; limit is %d", test.entries, used, test.limit)
			}
		})
	}
}
