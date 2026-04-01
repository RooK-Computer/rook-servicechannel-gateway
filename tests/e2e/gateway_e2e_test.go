package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	testssh "github.com/gliderlabs/ssh"
	gws "github.com/gorilla/websocket"
	cryptossh "golang.org/x/crypto/ssh"

	"rook-servicechannel-gateway/internal/config"
	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/httpserver"
	"rook-servicechannel-gateway/internal/sshbridge"
)

func TestGatewayEndToEndWithMockBackendAndTestSSH(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/gateway/1/validateToken" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ipAddress":"127.0.0.1","pin":"1234","mitarbeiteraccount":"alice"}`)
	}))
	defer backend.Close()

	_, serverSigner := mustSigner(t)
	clientKey, clientSigner := mustSigner(t)
	clientKeyPath := writePrivateKey(t, clientKey)
	authorizedKey := clientSigner.PublicKey()

	sshServer := &testssh.Server{
		Handler: func(sess testssh.Session) {
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

	cfg := testConfig()
	cfg.Backend.BaseURL = backend.URL
	cfg.Secrets.SSHPrivateKeyPath = clientKeyPath
	cfg.SSH.Port = portFromAddr(t, listener.Addr().String())

	bridge, err := sshbridge.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	server := httptest.NewServer(httpserver.NewHandler(cfg, silentLogger(), grants.NewClient(cfg.Backend.BaseURL, cfg.Backend.ValidationTimeout), bridge, nil))
	defer server.Close()

	conn := dialGateway(t, server.URL, "grant-123")
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{"type": "input", "data": "hello e2e\n"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}

	var output map[string]string
	if err := json.Unmarshal(payload, &output); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if output["type"] != "output" || !strings.Contains(output["data"], "hello e2e") {
		t.Fatalf("unexpected output payload %v", output)
	}
}

func TestGatewayHandshakeFailsWhenBackendUnavailable(t *testing.T) {
	t.Parallel()

	closedBaseURL := closedListenerBaseURL(t)
	cfg := testConfig()
	cfg.Backend.BaseURL = closedBaseURL

	server := httptest.NewServer(httpserver.NewHandler(cfg, silentLogger(), grants.NewClient(cfg.Backend.BaseURL, cfg.Backend.ValidationTimeout), nil, nil))
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

	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload["code"] != "backend_unreachable" {
		t.Fatalf("unexpected payload %v", payload)
	}
}

func TestGatewayClosesWebsocketWhenSSHConnectionFails(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ipAddress":"127.0.0.1"}`)
	}))
	defer backend.Close()

	cfg := testConfig()
	cfg.Backend.BaseURL = backend.URL
	cfg.SSH.Port = closedPort(t)

	bridge, err := sshbridge.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	server := httptest.NewServer(httpserver.NewHandler(cfg, silentLogger(), grants.NewClient(cfg.Backend.BaseURL, cfg.Backend.ValidationTimeout), bridge, nil))
	defer server.Close()

	conn := dialGateway(t, server.URL, "grant-123")
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}

	var message map[string]string
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if message["type"] != "error" || message["code"] != "ssh_bridge_failed" {
		t.Fatalf("unexpected payload %v", message)
	}
}

func TestGatewayClosesIdleSession(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ipAddress":"127.0.0.1"}`)
	}))
	defer backend.Close()

	_, serverSigner := mustSigner(t)
	clientKey, clientSigner := mustSigner(t)
	clientKeyPath := writePrivateKey(t, clientKey)
	authorizedKey := clientSigner.PublicKey()

	sshServer := &testssh.Server{
		Handler: func(sess testssh.Session) {
			_, _, _ = sess.Pty()
			select {}
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

	cfg := testConfig()
	cfg.Backend.BaseURL = backend.URL
	cfg.Secrets.SSHPrivateKeyPath = clientKeyPath
	cfg.SSH.Port = portFromAddr(t, listener.Addr().String())
	cfg.Session.IdleTimeout = 150 * time.Millisecond

	bridge, err := sshbridge.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	server := httptest.NewServer(httpserver.NewHandler(cfg, silentLogger(), grants.NewClient(cfg.Backend.BaseURL, cfg.Backend.ValidationTimeout), bridge, nil))
	defer server.Close()

	conn := dialGateway(t, server.URL, "grant-123")
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}

	var message map[string]string
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if message["type"] != "error" || message["code"] != "idle_timeout" {
		t.Fatalf("unexpected payload %v", message)
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
			ValidationTimeout: 2 * time.Second,
		},
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
			IdleTimeout:        2 * time.Second,
			MaxConcurrent:      8,
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

func dialGateway(t *testing.T, baseURL, grant string) *gws.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/gateway/terminal"
	conn, response, err := gws.DefaultDialer.Dial(wsURL, http.Header{
		"X-Rook-Terminal-Grant": []string{grant},
	})
	if err != nil {
		t.Fatalf("Dial() error = %v (status %v)", err, response)
	}
	return conn
}

func closedListenerBaseURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return "http://" + address
}

func closedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	port := portFromAddr(t, listener.Addr().String())
	_ = listener.Close()
	return port
}

func portFromAddr(t *testing.T, addr string) int {
	t.Helper()
	port, err := strconv.Atoi(strings.TrimPrefix(addr[strings.LastIndex(addr, ":"):], ":"))
	if err != nil {
		t.Fatalf("Atoi() error = %v", err)
	}
	return port
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
	privateKeyPath := filepath.Join(t.TempDir(), "id_rsa")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	if err := os.WriteFile(privateKeyPath, pemBytes, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return privateKeyPath
}
