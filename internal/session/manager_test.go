package session

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/sshbridge"
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

type consoleReadResult struct {
	data []byte
	err  error
}

type stubConsole struct {
	readCh     chan consoleReadResult
	writeMu    sync.Mutex
	writes     [][]byte
	resizes    []sshbridge.PtySize
	closeOnce  sync.Once
	closeCount int
}

type stubBridge struct {
	console *stubConsole
	err     error
}

func newMockBrowser() *mockBrowser {
	return &mockBrowser{readCh: make(chan readResult, 32)}
}

func newStubConsole() *stubConsole {
	return &stubConsole{readCh: make(chan consoleReadResult, 32)}
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

func (c *stubConsole) Read(buffer []byte) (int, error) {
	result, ok := <-c.readCh
	if !ok {
		return 0, io.EOF
	}
	if result.err != nil {
		return 0, result.err
	}
	return copy(buffer, result.data), nil
}

func (c *stubConsole) Write(buffer []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.writes = append(c.writes, append([]byte(nil), buffer...))
	return len(buffer), nil
}

func (c *stubConsole) Resize(_ context.Context, size sshbridge.PtySize) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.resizes = append(c.resizes, size)
	return nil
}

func (c *stubConsole) Close() error {
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		c.closeCount++
		c.writeMu.Unlock()
		close(c.readCh)
	})
	return nil
}

func (c *stubConsole) emit(data []byte) {
	c.readCh <- consoleReadResult{data: append([]byte(nil), data...)}
}

func (b stubBridge) Open(context.Context, sshbridge.SessionRequest) (sshbridge.Session, error) {
	if b.err != nil {
		return nil, b.err
	}
	return b.console, nil
}

func TestRegistryRemovesSessionOnClientClose(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(testLogger())
	browser := newMockBrowser()
	console := newStubConsole()

	handle, err := registry.Start(context.Background(), StartRequest{
		Grant:      grants.ValidationResult{IPAddress: "10.0.0.8"},
		Browser:    browser,
		Bridge:     stubBridge{console: console},
		SSHAccount: "pi",
		Logger:     testLogger(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	browser.push(gatewayws.Message{Type: gatewayws.TextFrame, Data: []byte(`{"type":"close","reason":"client requested close"}`)})

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
	console := newStubConsole()

	handle, err := registry.Start(context.Background(), StartRequest{
		Grant:      grants.ValidationResult{IPAddress: "10.0.0.8"},
		Browser:    browser,
		Bridge:     stubBridge{console: console},
		SSHAccount: "pi",
		Logger:     testLogger(),
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
	console := newStubConsole()
	var mu sync.Mutex
	cleanupCalls := 0

	_, err := registry.Start(context.Background(), StartRequest{
		Grant:      grants.ValidationResult{IPAddress: "10.0.0.8"},
		Browser:    browser,
		Bridge:     stubBridge{console: console},
		SSHAccount: "pi",
		Logger:     testLogger(),
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

func TestResizePropagatesToConsole(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(testLogger())
	browser := newMockBrowser()
	console := newStubConsole()

	_, err := registry.Start(context.Background(), StartRequest{
		Grant:      grants.ValidationResult{IPAddress: "10.0.0.8"},
		Browser:    browser,
		Bridge:     stubBridge{console: console},
		SSHAccount: "pi",
		Logger:     testLogger(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	browser.push(gatewayws.Message{Type: gatewayws.TextFrame, Data: []byte(`{"type":"resize","rows":30,"columns":120}`)})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		console.writeMu.Lock()
		count := len(console.resizes)
		console.writeMu.Unlock()
		if count == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	browser.push(gatewayws.Message{Type: gatewayws.TextFrame, Data: []byte(`{"type":"close"}`)})

	console.writeMu.Lock()
	defer console.writeMu.Unlock()
	if len(console.resizes) != 1 || console.resizes[0].Rows != 30 || console.resizes[0].Columns != 120 {
		t.Fatalf("unexpected resize propagation %#v", console.resizes)
	}
}

func TestConsoleOutputGetsQueuedForBrowser(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(testLogger())
	browser := newMockBrowser()
	console := newStubConsole()

	_, err := registry.Start(context.Background(), StartRequest{
		Grant:      grants.ValidationResult{IPAddress: "10.0.0.8"},
		Browser:    browser,
		Bridge:     stubBridge{console: console},
		SSHAccount: "pi",
		Logger:     testLogger(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	console.emit([]byte("hello from ssh"))
	browser.push(gatewayws.Message{Type: gatewayws.TextFrame, Data: []byte(`{"type":"close"}`)})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		browser.writeMu.Lock()
		count := len(browser.writes)
		browser.writeMu.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	browser.writeMu.Lock()
	defer browser.writeMu.Unlock()
	if len(browser.writes) == 0 {
		t.Fatal("expected browser to receive queued output")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
