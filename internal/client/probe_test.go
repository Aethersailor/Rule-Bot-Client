package client

import (
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeControllerUsesSecretAndCustomCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer probe-secret" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/version":
			io.WriteString(writer, `{"version":"test-mihomo"}`)
		case "/configs", "/connections":
			io.WriteString(writer, `{}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ProbeController(context.Background(), InstanceConfig{
		Name: "tls", URL: server.URL, Secret: "probe-secret", TLS: TLSConfig{CAFile: caPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Version["version"] != "test-mihomo" || !result.ConfigsOK || !result.Connections {
		t.Fatalf("ProbeController() = %+v", result)
	}
}
