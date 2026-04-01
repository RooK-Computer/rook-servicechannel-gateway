package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	testssh "github.com/gliderlabs/ssh"
	gws "github.com/gorilla/websocket"
	cryptossh "golang.org/x/crypto/ssh"

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

func TestTerminalHandshakeRequiresGrantHeader(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/gateway/terminal", nil)
	request.Header.Set("Connection", "keep-alive, Upgrade")
	request.Header.Set("Upgrade", "websocket")
	recorder := httptest.NewRecorder()

	NewHandler(testConfig(), silentLogger(), stubValidator{}, nil, session.NewRegistry(silentLogger())).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func TestTerminalHandshakeReturnsForbiddenForInvalidGrant(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		err: &grants.InvalidGrantError{StatusCode: http.StatusForbidden, Code: "grant_revoked", Message: "grant revoked"},
	}, nil, session.NewRegistry(silentLogger())))
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
	}, nil, session.NewRegistry(silentLogger())))
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
	bridge := testBridge{console: newBridgeConsole()}
	server := httptest.NewServer(NewHandler(testConfig(), silentLogger(), stubValidator{
		result: grants.ValidationResult{IPAddress: "10.0.0.8"},
	}, bridge, sessions))
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

func TestTerminalHandshakeBridgesSSHOutputToBrowser(t *testing.T) {
	t.Parallel()

	_, serverSigner := mustSigner(t)
	clientKey, clientSigner := mustSigner(t)
	clientKeyPath := writePrivateKey(t, clientKey)
	authorizedKey := clientSigner.PublicKey()

	var resizeMu sync.Mutex
	var resizeEvents []testssh.Window

	sshServer := &testssh.Server{
		Handler: func(sess testssh.Session) {
			_, winCh, isPty := sess.Pty()
			if !isPty {
				_, _ = io.WriteString(sess, "pty required")
				return
			}
			go func() {
				for win := range winCh {
					resizeMu.Lock()
					resizeEvents = append(resizeEvents, win)
					resizeMu.Unlock()
				}
			}()
			buffer := make([]byte, 1024)
			for {
				count, err := sess.Read(buffer)
				if count > 0 {
					_, _ = sess.Write(buffer[:count])
				}
				if err != nil {
					return
				}
			}
		},
		PublicKeyHandler: func(_ testssh.Context, key testssh.PublicKey) bool {
			return string(key.Marshal()) == string(authorizedKey.Marshal())
		},
	}
	sshServer.AddHostKey(serverSigner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	go sshServer.Serve(listener)
	defer sshServer.Close()

	port, err := strconv.Atoi(strings.TrimPrefix(listener.Addr().String()[strings.LastIndex(listener.Addr().String(), ":"):], ":"))
	if err != nil {
		t.Fatalf("Atoi() error = %v", err)
	}

	cfg := testConfig()
	cfg.Secrets.SSHPrivateKeyPath = clientKeyPath
	cfg.SSH.Port = port
	cfg.SSH.ConnectTimeout = 2 * time.Second
	cfg.SSH.InsecureIgnoreHostKey = true

	bridge, err := sshbridge.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	server := httptest.NewServer(NewHandler(cfg, silentLogger(), stubValidator{
		result: grants.ValidationResult{IPAddress: "127.0.0.1"},
	}, bridge, session.NewRegistry(silentLogger())))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/gateway/terminal"
	conn, _, err := gws.DefaultDialer.Dial(wsURL, http.Header{"X-Rook-Terminal-Grant": []string{"grant-123"}})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

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
		resizeMu.Lock()
		count := len(resizeEvents)
		last := testssh.Window{}
		if count > 0 {
			last = resizeEvents[count-1]
		}
		resizeMu.Unlock()
		if count > 0 {
			if last.Height != 33 || last.Width != 101 {
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
			GrantHeaderName:   "X-Rook-Terminal-Grant",
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
			IdleTimeout:        2 * time.Minute,
			MaxConcurrent:      32,
			OutboundQueueDepth: 16,
		},
		WebSocket: config.WebSocketConfig{
			MaxMessageBytes: 64 * 1024,
		},
	}
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type testBridge struct {
	console *bridgeConsole
}

type bridgeConsole struct {
	readCh chan []byte
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

func (b *bridgeConsole) Write(buffer []byte) (int, error)                { return len(buffer), nil }
func (b *bridgeConsole) Resize(context.Context, sshbridge.PtySize) error { return nil }
func (b *bridgeConsole) Close() error {
	close(b.readCh)
	return nil
}

func mustSigner(t *testing.T) (*rsa.PrivateKey, cryptossh.Signer) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signer, err := cryptossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("NewSignerFromKey() error = %v", err)
	}
	return key, signer
}

func writePrivateKey(t *testing.T, privateKey *rsa.PrivateKey) string {
	t.Helper()
	privateKeyPath := filepath.Join(t.TempDir(), "id_ed25519")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	if err := os.WriteFile(privateKeyPath, pemBytes, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return privateKeyPath
}
