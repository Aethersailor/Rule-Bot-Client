package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		failed  bool
		single  bool
		want    string
		ok      bool
	}{
		{
			name:    "TCP success",
			payload: "[TCP] 192.0.2.4:1234 --> Example.COM.:443 match Match using PROXY[node]",
			failed:  true,
			want:    "example.com",
			ok:      true,
		},
		{
			name:    "UDP success",
			payload: "[UDP] 192.0.2.4:1234 --> dns.example:53 match Match using DIRECT",
			failed:  true,
			want:    "dns.example",
			ok:      true,
		},
		{
			name:    "failed enabled",
			payload: "[TCP] dial PROXY (match Match/) 192.0.2.4:1234 --> failed.example:443 error: timeout",
			failed:  true,
			want:    "failed.example",
			ok:      true,
		},
		{
			name:    "failed disabled",
			payload: "[TCP] dial PROXY (match Match/) 192.0.2.4:1234 --> failed.example:443 error: timeout",
		},
		{
			name:    "other rule with Match proxy",
			payload: "[TCP] 192.0.2.4:1234 --> other.example:443 match RuleSet(main) using Match[proxy]",
			failed:  true,
		},
		{
			name:    "IP target",
			payload: "[TCP] 192.0.2.4:1234 --> 203.0.113.9:443 match Match using DIRECT",
			failed:  true,
		},
		{
			name:    "IPv6 target",
			payload: "[TCP] 192.0.2.4:1234 --> [2001:db8::1]:443 match Match using DIRECT",
			failed:  true,
		},
		{
			name:    "single label included",
			payload: "[TCP] 192.0.2.4:1234 --> NAS:443 match Match using DIRECT",
			failed:  true,
			single:  true,
			want:    "nas",
			ok:      true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			line, err := json.Marshal(map[string]string{"type": "info", "payload": test.payload})
			if err != nil {
				t.Fatal(err)
			}
			got, ok, err := parseLogLine(line, test.failed, test.single)
			if err != nil {
				t.Fatalf("parseLogLine() error = %v", err)
			}
			if got != test.want || ok != test.ok {
				t.Fatalf("parseLogLine() = %q, %v; want %q, %v", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestParseLogLineRejectsMalformedJSON(t *testing.T) {
	for _, input := range []string{
		`{"type":"info"}`,
		`{"payload":12}`,
		`{"payload":"unterminated}`,
		`garbage "payload":"[TCP] a:1 --> false.example:443 match Match using DIRECT"`,
	} {
		if _, _, err := parseLogLine([]byte(input), true, false); err == nil {
			t.Fatalf("parseLogLine(%q) succeeded", input)
		}
	}
}

func TestReadBoundedLineDrainsOversizedLine(t *testing.T) {
	input := strings.Repeat("x", 100) + "\nnext\n"
	reader := bufio.NewReaderSize(strings.NewReader(input), 8)
	scratch := make([]byte, 0, 8)
	if _, err := readBoundedLine(reader, &scratch, 32); !errors.Is(err, errLineTooLong) {
		t.Fatalf("first read error = %v", err)
	}
	line, err := readBoundedLine(reader, &scratch, 32)
	if err != nil || string(line) != "next" {
		t.Fatalf("second read = %q, %v", line, err)
	}
}

func TestConsumeLogStream(t *testing.T) {
	valid, _ := json.Marshal(map[string]string{
		"type":    "info",
		"payload": "[TCP] a:1 --> stream.example:443 match Match using DIRECT",
	})
	input := bytes.NewBuffer(nil)
	input.WriteString("not-json\n")
	input.Write(valid)
	input.WriteByte('\n')
	var got []string
	malformed := 0
	err := consumeLogStream(input, true, false, func(domain string) bool {
		got = append(got, domain)
		return true
	}, func(error) { malformed++ })
	if !errors.Is(err, io.EOF) {
		t.Fatalf("consumeLogStream() error = %v", err)
	}
	if malformed != 1 || len(got) != 1 || got[0] != "stream.example" {
		t.Fatalf("malformed=%d got=%v", malformed, got)
	}
}

func FuzzParseLogLine(f *testing.F) {
	seeds := []string{
		`{"type":"info","payload":"[TCP] a:1 --> example.com:443 match Match using DIRECT"}`,
		`{"payload":"[TCP] dial DIRECT (match Match/) a:1 --> failed.example:443 error: no route"}`,
		`not json`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_, _, _ = parseLogLine([]byte(input), true, true)
	})
}

func BenchmarkParseLogLineMatch(b *testing.B) {
	line := []byte(`{"type":"info","payload":"[TCP] 192.0.2.4:1234 --> example.com:443 match Match using DIRECT"}`)
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = parseLogLine(line, true, false)
	}
}

func BenchmarkParseLogLineNonMatch(b *testing.B) {
	line := []byte(`{"type":"info","payload":"[TCP] 192.0.2.4:1234 --> example.com:443 match RuleSet(main) using DIRECT"}`)
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = parseLogLine(line, true, false)
	}
}

func TestParserAllocationGate(t *testing.T) {
	match := []byte(`{"type":"info","payload":"[TCP] 192.0.2.4:1234 --> example.com:443 match Match using DIRECT"}`)
	nonMatch := []byte(`{"type":"info","payload":"[TCP] 192.0.2.4:1234 --> example.com:443 match RuleSet(main) using DIRECT"}`)
	if got := testing.AllocsPerRun(1000, func() {
		_, _, _ = parseLogLine(match, true, false)
	}); got > 3 {
		t.Fatalf("matching line allocations = %.1f; limit is 3", got)
	}
	if got := testing.AllocsPerRun(1000, func() {
		_, _, _ = parseLogLine(nonMatch, true, false)
	}); got != 0 {
		t.Fatalf("non-matching line allocations = %.1f; want zero", got)
	}
}
