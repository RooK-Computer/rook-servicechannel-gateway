package session

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"rook-servicechannel-gateway/internal/grants"
	gatewayws "rook-servicechannel-gateway/internal/websocket"
)

type mockBrowser struct {
	readCh      chan readResult
	writeBlock  chan struct{}
	writeMu     sync.Mutex
	writes      []gatewayws.Message
	closeCount  int
	closeCode   int
	closeReason string
}

type readResult struct {
	message gatewayws.Message
	err     error
}

func newMockBrowser() *mockBrowser {
	return &mockBrowser{readCh: make(chan readResult, 32)}
}

func (m *mockBrowser) ReadMessage(context.Context) (gatewayws.Message, error) {
	result := <-m.readCh
	return result.message, result.err
}

func (m *mockBrowser) WriteMessage(context.Context, gatewayws.Message) error {
	if m.writeBlock != nil {
		<-m.writeBlock
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.writes = append(m.writes, gatewayws.Message{})
	return nil
}

func (m *mockBrowser) Close(code int, reason string) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.closeCount++
	m.closeCode = code
	m.closeReason = reason
	return nil
}

func (m *mockBrowser) push(message gatewayws.Message) {
	m.readCh <- readResult{message: message}
}

func (m *mockBrowser) fail(err error) {
	m.readCh <- readResult{err: err}
}

func TestRegistryRemovesSessionOnClientClose(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(testLogger())
	browser := newMockBrowser()

	handle, err := registry.Start(context.Background(), StartRequest{
		Grant:   grants.ValidationResult{IPAddress: "10.0.0.8"},
		Browser: browser,
		Logger:  testLogger(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	browser.push(gatewayws.NewServerClose("client requested close"))
	browser.fail(errors.New("use of closed network connection"))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if registry.Count() == 0 {
			if handle.ID() == "" {
				t.Fatal("expected non-empty session id")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if registry.Count() != 0 {
		t.Fatalf("expected registry to be empty, got %d", registry.Count())
	}
}

func TestSessionQueueOverflowClosesSession(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(testLogger())
	browser := newMockBrowser()
	browser.writeBlock = make(chan struct{})

	handle, err := registry.Start(context.Background(), StartRequest{
		Grant:   grants.ValidationResult{IPAddress: "10.0.0.8"},
		Browser: browser,
		Logger:  testLogger(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	sessionHandle, ok := handle.(*Session)
	if !ok {
		t.Fatalf("expected *Session, got %T", handle)
	}

	var overflowErr error
	for i := 0; i < outboundQueueSize+4; i++ {
		err := sessionHandle.Enqueue(gatewayws.NewServerError("queued", "message"))
		if err != nil {
			overflowErr = err
			break
		}
	}

	if overflowErr == nil {
		t.Fatal("expected queue overflow error")
	}

	close(browser.writeBlock)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if registry.Count() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if registry.Count() != 0 {
		t.Fatalf("expected registry to be empty, got %d", registry.Count())
	}
}

func TestCleanupHookRunsOnce(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(testLogger())
	browser := newMockBrowser()
	var mu sync.Mutex
	cleanupCalls := 0

	_, err := registry.Start(context.Background(), StartRequest{
		Grant:   grants.ValidationResult{IPAddress: "10.0.0.8"},
		Browser: browser,
		Logger:  testLogger(),
		CleanupHook: func(context.Context, Snapshot) {
			mu.Lock()
			defer mu.Unlock()
			cleanupCalls++
		},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	browser.fail(context.Canceled)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		calls := cleanupCalls
		mu.Unlock()
		if calls == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if cleanupCalls != 1 {
		t.Fatalf("expected exactly one cleanup call, got %d", cleanupCalls)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
