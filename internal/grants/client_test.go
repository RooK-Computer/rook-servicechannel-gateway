package grants

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientValidateTokenSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != validationPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %q", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ipAddress":"10.0.0.8"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	result, err := client.ValidateToken(context.Background(), "token-123")
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if result.IPAddress != "10.0.0.8" {
		t.Fatalf("unexpected ipAddress %q", result.IPAddress)
	}
	if string(result.RawResponse) != `{"ipAddress":"10.0.0.8"}` {
		t.Fatalf("unexpected raw response %s", string(result.RawResponse))
	}
}

func TestClientValidateTokenInvalidGrant(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"grant_revoked","message":"grant is no longer valid"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	_, err := client.ValidateToken(context.Background(), "token-123")

	var invalidGrantErr *InvalidGrantError
	if !errors.As(err, &invalidGrantErr) {
		t.Fatalf("expected InvalidGrantError, got %v", err)
	}
	if invalidGrantErr.Code != "grant_revoked" {
		t.Fatalf("unexpected code %q", invalidGrantErr.Code)
	}
}

func TestClientValidateTokenBackendError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"backend_unavailable","message":"temporary issue"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, time.Second)
	_, err := client.ValidateToken(context.Background(), "token-123")

	var backendErr *BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("expected BackendError, got %v", err)
	}
	if backendErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("unexpected status code %d", backendErr.StatusCode)
	}
}

func TestClientValidateTokenTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ipAddress":"10.0.0.8"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Millisecond)
	_, err := client.ValidateToken(context.Background(), "token-123")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !IsTransportError(err) {
		t.Fatalf("expected transport error, got %v", err)
	}
}
