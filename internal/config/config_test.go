package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveRequiresListenAddress(t *testing.T) {
	t.Parallel()

	_, err := Resolve(map[string]string{envBackendBaseURL: "https://backend.example.test"})
	if err == nil || err.Error() != envListenAddress+" must be set" {
		t.Fatalf("expected missing listen address error, got %v", err)
	}
}

func TestResolveAppliesDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := Resolve(map[string]string{
		envListenAddress:            ":8080",
		envHTTPReadHeaderTimeout:    "6s",
		envBackendBaseURL:           "https://backend.example.test",
		envBackendTimeout:           "7s",
		envLogLevel:                 "debug",
		envGrantHeaderName:          "X-Test-Grant",
		envSSHPort:                  "2022",
		envSSHConnectTimeout:        "9s",
		envSSHUsername:              "alice",
		envSSHInsecureIgnoreHostKey: "true",
		envSessionIdleTimeout:       "45s",
		envSessionMaxConcurrent:     "5",
		envSessionOutboundQueue:     "24",
		envWebSocketMaxMessageBytes: "131072",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if cfg.HTTP.ListenAddress != ":8080" || cfg.HTTP.ReadHeaderTimeout != 6*time.Second || cfg.Backend.ValidationTimeout != 7*time.Second || cfg.Logging.Level != slog.LevelDebug {
		t.Fatalf("unexpected config %#v", cfg)
	}
	if cfg.HTTP.GrantHeaderName != "X-Test-Grant" || cfg.Secrets.SSHPrivateKeyPath != defaultSSHPrivateKey {
		t.Fatalf("unexpected config %#v", cfg)
	}
	if cfg.SSH.Port != 2022 || cfg.SSH.ConnectTimeout != 9*time.Second || cfg.SSH.Username != "alice" || !cfg.SSH.InsecureIgnoreHostKey {
		t.Fatalf("unexpected ssh config %#v", cfg.SSH)
	}
	if cfg.Session.IdleTimeout != 45*time.Second || cfg.Session.MaxConcurrent != 5 || cfg.Session.OutboundQueueDepth != 24 {
		t.Fatalf("unexpected session config %#v", cfg.Session)
	}
	if cfg.WebSocket.MaxMessageBytes != 131072 {
		t.Fatalf("unexpected websocket config %#v", cfg.WebSocket)
	}
}

func TestResolveRejectsInvalidSSHPort(t *testing.T) {
	t.Parallel()

	_, err := Resolve(map[string]string{
		envListenAddress:  ":8080",
		envBackendBaseURL: "https://backend.example.test",
		envSSHPort:        "70000",
	})
	if err == nil || err.Error() != envSSHPort+" must be between 1 and 65535" {
		t.Fatalf("expected ssh port validation error, got %v", err)
	}
}

func TestLoadMergesConfigFileAndEnvironment(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "gateway.env")
	contents := "GATEWAY_LISTEN_ADDRESS=:7000\nGATEWAY_BACKEND_BASE_URL=https://file.example.test\nGATEWAY_BACKEND_TIMEOUT=3s\nGATEWAY_SSH_PORT=2222\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv(envConfigFile, configPath)
	t.Setenv(envBackendBaseURL, "https://env.example.test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTP.ListenAddress != ":7000" || cfg.Backend.BaseURL != "https://env.example.test" || cfg.Backend.ValidationTimeout != 3*time.Second || cfg.SSH.Port != 2222 {
		t.Fatalf("unexpected config %#v", cfg)
	}
}

func TestResolveRejectsInvalidSessionLimit(t *testing.T) {
	t.Parallel()

	_, err := Resolve(map[string]string{
		envListenAddress:        ":8080",
		envBackendBaseURL:       "https://backend.example.test",
		envSessionMaxConcurrent: "0",
	})
	if err == nil || err.Error() != envSessionMaxConcurrent+" must be greater than zero" {
		t.Fatalf("expected session max concurrent validation error, got %v", err)
	}
}
