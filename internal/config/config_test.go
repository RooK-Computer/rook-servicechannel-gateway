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

	_, err := Resolve(map[string]string{
		envBackendBaseURL: "https://backend.example.test",
	})
	if err == nil || err.Error() != envListenAddress+" must be set" {
		t.Fatalf("expected missing listen address error, got %v", err)
	}
}

func TestResolveAppliesDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := Resolve(map[string]string{
		envListenAddress:   ":8080",
		envBackendBaseURL:  "https://backend.example.test",
		envBackendTimeout:  "7s",
		envLogLevel:        "debug",
		envGrantHeaderName: "X-Test-Grant",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if cfg.HTTP.ListenAddress != ":8080" {
		t.Fatalf("unexpected listen address %q", cfg.HTTP.ListenAddress)
	}
	if cfg.Backend.ValidationTimeout != 7*time.Second {
		t.Fatalf("unexpected timeout %v", cfg.Backend.ValidationTimeout)
	}
	if cfg.Logging.Level != slog.LevelDebug {
		t.Fatalf("unexpected log level %v", cfg.Logging.Level)
	}
	if cfg.HTTP.GrantHeaderName != "X-Test-Grant" {
		t.Fatalf("unexpected grant header %q", cfg.HTTP.GrantHeaderName)
	}
	if cfg.Secrets.SSHPrivateKeyPath != defaultSSHPrivateKey {
		t.Fatalf("unexpected private key path %q", cfg.Secrets.SSHPrivateKeyPath)
	}
}

func TestLoadMergesConfigFileAndEnvironment(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "gateway.env")
	contents := "GATEWAY_LISTEN_ADDRESS=:7000\nGATEWAY_BACKEND_BASE_URL=https://file.example.test\nGATEWAY_BACKEND_TIMEOUT=3s\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv(envConfigFile, configPath)
	t.Setenv(envBackendBaseURL, "https://env.example.test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTP.ListenAddress != ":7000" {
		t.Fatalf("unexpected listen address %q", cfg.HTTP.ListenAddress)
	}
	if cfg.Backend.BaseURL != "https://env.example.test" {
		t.Fatalf("expected env override, got %q", cfg.Backend.BaseURL)
	}
	if cfg.Backend.ValidationTimeout != 3*time.Second {
		t.Fatalf("unexpected timeout %v", cfg.Backend.ValidationTimeout)
	}
}
