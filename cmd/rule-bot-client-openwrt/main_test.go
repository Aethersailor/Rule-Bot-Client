package main

import (
	"errors"
	"strings"
	"testing"
)

type rejectingReader struct{}

func (rejectingReader) Read([]byte) (int, error) {
	return 0, errors.New("stdin must not be read")
}

func TestReadPayloadSkipsStdinForFixedActions(t *testing.T) {
	for _, action := range []string{"initialize", "generate", "update_auto", "unsupported"} {
		t.Run(action, func(t *testing.T) {
			payload, err := readPayload("", []string{"rule-bot-client-openwrt", action}, rejectingReader{})
			if err != nil {
				t.Fatalf("readPayload(%q) error = %v", action, err)
			}
			if len(payload) != 0 {
				t.Fatalf("readPayload(%q) = %q, want empty payload", action, payload)
			}
		})
	}
}

func TestReadPayloadReadsStdinForPayloadActions(t *testing.T) {
	for _, action := range []string{"save", "probe", "domains", "clear", "service", "restore", "update_config"} {
		t.Run(action, func(t *testing.T) {
			payload, err := readPayload("", []string{"rule-bot-client-openwrt", action}, strings.NewReader(`{"value":true}`))
			if err != nil {
				t.Fatalf("readPayload(%q) error = %v", action, err)
			}
			if string(payload) != `{"value":true}` {
				t.Fatalf("readPayload(%q) = %q", action, payload)
			}
		})
	}
}
