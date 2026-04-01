package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"rook-servicechannel-gateway/internal/config"
)

func TestNewHandlerHealthAndReady(t *testing.T) {
	t.Parallel()

	handler := NewHandler(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(handler)
	defer server.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, response.StatusCode)
		}
	}
}

func TestTerminalPlaceholderRequiresUpgradeHeaders(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/gateway/terminal", nil)
	recorder := httptest.NewRecorder()

	NewHandler(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil))).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUpgradeRequired {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func TestTerminalPlaceholderRequiresGrantHeader(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/gateway/terminal", nil)
	request.Header.Set("Connection", "keep-alive, Upgrade")
	request.Header.Set("Upgrade", "websocket")
	recorder := httptest.NewRecorder()

	NewHandler(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil))).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func TestTerminalPlaceholderReturnsNotImplementedWhenPrerequisitesPass(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/gateway/terminal", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("X-Rook-Terminal-Grant", "grant-123")
	recorder := httptest.NewRecorder()

	NewHandler(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil))).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func testConfig() config.Config {
	return config.Config{
		HTTP: config.HTTPConfig{
			ListenAddress:   ":0",
			GrantHeaderName: "X-Rook-Terminal-Grant",
		},
		Backend: config.BackendConfig{
			BaseURL:           "https://backend.example.test",
			ValidationTimeout: 5,
		},
		Logging: config.LoggingConfig{},
		Secrets: config.SecretsConfig{
			SSHPrivateKeyPath: "secrets/gateway_ssh_ed25519",
			SSHPublicKeyPath:  "secrets/gateway_ssh_ed25519.pub",
		},
	}
}
