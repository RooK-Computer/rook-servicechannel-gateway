package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"

	"rook-servicechannel-gateway/internal/config"
	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/session"
)

type stubValidator struct {
	result grants.ValidationResult
	err    error
}

func (s stubValidator) ValidateToken(context.Context, string) (grants.ValidationResult, error) {
	if s.err != nil {
		return grants.ValidationResult{}, s.err
	}
	return s.result, nil
}

func TestNewHandlerHealthAndReady(t *testing.T) {
	t.Parallel()

	handler := NewHandler(testConfig(), silentLogger(), stubValidator{}, session.NewRegistry(silentLogger()))
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

func TestTerminalHandshakeRequiresUpgradeHeaders(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/gateway/terminal", nil)
	recorder := httptest.NewRecorder()

	NewHandler(testConfig(), silentLogger(), stubValidator{}, session.NewRegistry(silentLogger())).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUpgradeRequired {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func TestTerminalHandshakeRequiresGrantHeader(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/gateway/terminal", nil)
	request.Header.Set("Connection", "keep-alive, Upgrade")
	request.Header.Set("Upgrade", "websocket")
	recorder := httptest.NewRecorder()

	NewHandler(testConfig(), silentLogger(), stubValidator{}, session.NewRegistry(silentLogger())).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func TestTerminalHandshakeReturnsForbiddenForInvalidGrant(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		err: &grants.InvalidGrantError{StatusCode: http.StatusForbidden, Code: "grant_revoked", Message: "grant revoked"},
	}, session.NewRegistry(silentLogger())))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/gateway/terminal", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("X-Rook-Terminal-Grant", "grant-123")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected status %d", response.StatusCode)
	}
}

func TestTerminalHandshakeReturnsBadGatewayForBackendFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		err: &grants.BackendError{StatusCode: http.StatusInternalServerError, Code: "backend_failure", Message: "validation failed"},
	}, session.NewRegistry(silentLogger())))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/gateway/terminal", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("X-Rook-Terminal-Grant", "grant-123")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("unexpected status %d", response.StatusCode)
	}
}

func TestTerminalHandshakeUpgradesAndRejectsDeprecatedAuthorizeMessage(t *testing.T) {
	t.Parallel()

	sessions := session.NewRegistry(silentLogger())
	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		result: grants.ValidationResult{IPAddress: "10.0.0.8"},
	}, sessions))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/gateway/terminal"
	conn, response, err := gws.DefaultDialer.Dial(wsURL, http.Header{
		"X-Rook-Terminal-Grant": []string{"grant-123"},
	})
	if err != nil {
		t.Fatalf("Dial() error = %v (status %v)", err, response)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{"type": "authorize"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	var protocolError map[string]string
	if err := json.Unmarshal(payload, &protocolError); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if protocolError["type"] != "error" {
		t.Fatalf("unexpected first message %v", protocolError)
	}

	_, payload, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	var closeMessage map[string]string
	if err := json.Unmarshal(payload, &closeMessage); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if closeMessage["type"] != "close" {
		t.Fatalf("unexpected close message %v", closeMessage)
	}

	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected websocket connection to close")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sessions.Count() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sessions.Count() != 0 {
		t.Fatalf("expected session registry to be empty, got %d", sessions.Count())
	}
}

func TestClassifyGrantErrorTransport(t *testing.T) {
	t.Parallel()

	status, payload := classifyGrantError(&grants.TransportError{Err: errors.New("timeout")})
	if status != http.StatusBadGateway {
		t.Fatalf("unexpected status %d", status)
	}
	if payload.Code != "backend_unreachable" {
		t.Fatalf("unexpected code %q", payload.Code)
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
			ValidationTimeout: 5 * time.Second,
		},
		Logging: config.LoggingConfig{},
		Secrets: config.SecretsConfig{
			SSHPrivateKeyPath: "secrets/gateway_ssh_ed25519",
			SSHPublicKeyPath:  "secrets/gateway_ssh_ed25519.pub",
		},
	}
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
