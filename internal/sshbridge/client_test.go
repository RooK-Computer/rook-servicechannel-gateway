package sshbridge

import (
	"testing"
	"time"

	"rook-servicechannel-gateway/internal/config"
)

func TestHostKeyCallbackRequiresInsecureFlagForCurrentMVP(t *testing.T) {
	t.Parallel()

	client, err := NewClient(config.Config{
		HTTP:    config.HTTPConfig{ListenAddress: ":8080", GrantHeaderName: "X-Rook-Terminal-Grant"},
		Backend: config.BackendConfig{BaseURL: "https://backend.example.test", ValidationTimeout: 5 * time.Second},
		Secrets: config.SecretsConfig{SSHPrivateKeyPath: "secrets/gateway_ssh_ed25519", SSHPublicKeyPath: "secrets/gateway_ssh_ed25519.pub"},
		SSH:     config.SSHConfig{Username: "pi", Port: 22, ConnectTimeout: 5 * time.Second, InsecureIgnoreHostKey: false},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.hostKeyCallback(); err == nil {
		t.Fatal("expected host key callback error when insecure flag is disabled")
	}
}
