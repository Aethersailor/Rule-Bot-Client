package client

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOutputStoreWritesAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domains.txt")
	store, set, err := openOutput(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if set.Len() != 0 {
		t.Fatalf("initial set length = %d", set.Len())
	}
	domains := make(chan string, 2)
	domains <- "example.com"
	domains <- "other.example"
	close(domains)
	if err := store.Run(domains); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "example.com\nother.example\n" {
		t.Fatalf("output = %q", data)
	}

	store, set, err = openOutput(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	duplicate := set.Add("example.com")
	if set.Len() != 2 || duplicate {
		t.Fatalf("reloaded set length=%d duplicate=%v", set.Len(), duplicate)
	}
}

func TestOutputStoreRepairsValidTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domains.txt")
	if err := os.WriteFile(path, []byte("example.com"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, set, err := openOutput(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if set.Len() != 1 {
		t.Fatalf("set length = %d", set.Len())
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "example.com\n" {
		t.Fatalf("output = %q", data)
	}
}

func TestOutputStorePreservesInvalidTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domains.txt")
	const contents = "example.com\npartial/"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openOutput(path, time.Hour); err == nil {
		t.Fatal("openOutput() succeeded")
	}
	data, _ := os.ReadFile(path)
	if string(data) != contents {
		t.Fatalf("output changed to %q", data)
	}
}

func TestOutputStoreExclusiveLock(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux flock behavior")
	}
	path := filepath.Join(t.TempDir(), "domains.txt")
	first, _, err := openOutput(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, _, err := openOutput(path, time.Hour); err == nil {
		second.Close()
		t.Fatal("second openOutput() succeeded")
	} else if !strings.Contains(err.Error(), "lock output") {
		t.Fatalf("second openOutput() error = %v", err)
	}
}
