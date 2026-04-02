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
	"sync"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"

	"rook-servicechannel-gateway/internal/config"
	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/session"
	"rook-servicechannel-gateway/internal/sshbridge"
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

	handler := NewHandler(testConfig(), silentLogger(), stubValidator{}, nil, session.NewRegistry(silentLogger()))
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

	NewHandler(testConfig(), silentLogger(), stubValidator{}, nil, session.NewRegistry(silentLogger())).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUpgradeRequired {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func TestTerminalHandshakeUpgradesWithoutGrantHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{}, nil, session.NewRegistry(silentLogger())))
	defer server.Close()

	conn, response, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/gateway/terminal", nil)
	if err != nil {
		t.Fatalf("Dial() error = %v (status %v)", err, response)
	}
	_ = conn.Close()
}

func TestTerminalHandshakeAllowsMismatchedOrigin(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{}, nil, session.NewRegistry(silentLogger())))
	defer server.Close()

	conn, response, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/gateway/terminal", http.Header{
		"Origin": []string{"https://frontend.example.test"},
	})
	if err != nil {
		t.Fatalf("Dial() error = %v (status %v)", err, response)
	}
	_ = conn.Close()
}

func TestTerminalAuthorizeReturnsForbiddenForInvalidGrant(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		err: &grants.InvalidGrantError{StatusCode: http.StatusForbidden, Code: "grant_revoked", Message: "grant revoked"},
	}, nil, session.NewRegistry(silentLogger())))
	defer server.Close()

	conn, response, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/gateway/terminal", nil)
	if err != nil {
		t.Fatalf("Dial() error = %v (status %v)", err, response)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{"type": "authorize", "token": "grant-123"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	message := readJSONMessage(t, conn)
	if message["type"] != "error" || message["code"] != "grant_revoked" {
		t.Fatalf("unexpected error payload %v", message)
	}

	closeMessage := readJSONMessage(t, conn)
	if closeMessage["type"] != "close" || closeMessage["reason"] != "authorization_failed" {
		t.Fatalf("unexpected close payload %v", closeMessage)
	}
}

func TestTerminalAuthorizeReturnsBadGatewayForBackendFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		err: &grants.BackendError{StatusCode: http.StatusInternalServerError, Code: "backend_failure", Message: "validation failed"},
	}, nil, session.NewRegistry(silentLogger())))
	defer server.Close()

	conn, response, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/gateway/terminal", nil)
	if err != nil {
		t.Fatalf("Dial() error = %v (status %v)", err, response)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{"type": "authorize", "token": "grant-123"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	message := readJSONMessage(t, conn)
	if message["type"] != "error" || message["code"] != "backend_failure" {
		t.Fatalf("unexpected error payload %v", message)
	}
}

func TestTerminalAuthorizeTimesOutWithoutFirstMessage(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Session.AuthorizeTimeout = 80 * time.Millisecond
	server := httptest.NewServer(NewHandler(cfg, silentLogger(), stubValidator{}, nil, session.NewRegistry(silentLogger())))
	defer server.Close()

	conn, response, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/gateway/terminal", nil)
	if err != nil {
		t.Fatalf("Dial() error = %v (status %v)", err, response)
	}
	defer conn.Close()

	message := readJSONMessage(t, conn)
	if message["type"] != "error" || message["code"] != "authorize_timeout" {
		t.Fatalf("unexpected timeout payload %v", message)
	}

	closeMessage := readJSONMessage(t, conn)
	if closeMessage["type"] != "close" || closeMessage["reason"] != string(session.EndReasonAuthorizeTimeout) {
		t.Fatalf("unexpected close payload %v", closeMessage)
	}
}

func TestTerminalHandshakeRejectsInputBeforeAuthorize(t *testing.T) {
	t.Parallel()

	sessions := session.NewRegistry(silentLogger())
	bridge := testBridge{console: newBridgeConsole()}
	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		result: grants.ValidationResult{IPAddress: "10.0.0.8"},
	}, bridge, sessions))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/gateway/terminal"
	conn, response, err := gws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v (status %v)", err, response)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{"type": "input", "data": "hello"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	protocolError := readJSONMessage(t, conn)
	if protocolError["type"] != "error" {
		t.Fatalf("unexpected first message %v", protocolError)
	}

	closeMessage := readJSONMessage(t, conn)
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

func TestTerminalHandshakeBridgesSSHOutputToBrowser(t *testing.T) {
	t.Parallel()

	bridgeConsole := newBridgeConsole()
	bridge := testBridge{console: bridgeConsole}

	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		result: grants.ValidationResult{IPAddress: "10.0.0.8"},
	}, bridge, session.NewRegistry(silentLogger())))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/gateway/terminal"
	conn, _, err := gws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{"type": "authorize", "token": "grant-123"}); err != nil {
		t.Fatalf("WriteJSON(authorize) error = %v", err)
	}
	authorized := readJSONMessage(t, conn)
	if authorized["type"] != "authorized" {
		t.Fatalf("unexpected authorized message %v", authorized)
	}

	if err := conn.WriteJSON(map[string]any{"type": "resize", "rows": 33, "columns": 101}); err != nil {
		t.Fatalf("WriteJSON(resize) error = %v", err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "input", "data": "hello ssh\n"}); err != nil {
		t.Fatalf("WriteJSON(input) error = %v", err)
	}

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	var outputMessage map[string]string
	if err := json.Unmarshal(payload, &outputMessage); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if outputMessage["type"] != "output" || !strings.Contains(outputMessage["data"], "hello ssh") {
		t.Fatalf("unexpected output message %v", outputMessage)
	}

	resizeDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(resizeDeadline) {
		bridgeConsole.mu.Lock()
		count := len(bridgeConsole.resizes)
		last := sshbridge.PtySize{}
		if count > 0 {
			last = bridgeConsole.resizes[count-1]
		}
		bridgeConsole.mu.Unlock()
		if count > 0 {
			if last.Rows != 33 || last.Columns != 101 {
				t.Fatalf("unexpected resize event %+v", last)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected resize event to reach ssh server")
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
			ListenAddress:     ":0",
			ReadHeaderTimeout: 5 * time.Second,
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
		SSH: config.SSHConfig{
			Username:              "pi",
			Port:                  22,
			ConnectTimeout:        2 * time.Second,
			InsecureIgnoreHostKey: true,
		},
		Session: config.SessionConfig{
			AuthorizeTimeout:   2 * time.Minute,
			MaxConcurrent:      32,
			OutboundQueueDepth: 16,
		},
		WebSocket: config.WebSocketConfig{
			MaxMessageBytes:   64 * 1024,
			KeepaliveInterval: 30 * time.Second,
			KeepaliveTimeout:  75 * time.Second,
		},
	}
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func readJSONMessage(t *testing.T, conn *gws.Conn) map[string]string {
	t.Helper()

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}

	var message map[string]string
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return message
}

type testBridge struct {
	console *bridgeConsole
}

type bridgeConsole struct {
	readCh  chan []byte
	mu      sync.Mutex
	resizes []sshbridge.PtySize
	closed  bool
}

func newBridgeConsole() *bridgeConsole {
	return &bridgeConsole{readCh: make(chan []byte, 8)}
}

func (b testBridge) Open(context.Context, sshbridge.SessionRequest) (sshbridge.Session, error) {
	return b.console, nil
}

func (b *bridgeConsole) Read(buffer []byte) (int, error) {
	payload, ok := <-b.readCh
	if !ok {
		return 0, io.EOF
	}
	return copy(buffer, payload), nil
}

func (b *bridgeConsole) Write(buffer []byte) (int, error) {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return 0, io.EOF
	}
	b.readCh <- append([]byte(nil), buffer...)
	return len(buffer), nil
}

func (b *bridgeConsole) Resize(_ context.Context, size sshbridge.PtySize) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resizes = append(b.resizes, size)
	return nil
}

func (b *bridgeConsole) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	close(b.readCh)
	return nil
}
