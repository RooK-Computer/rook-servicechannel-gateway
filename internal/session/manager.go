package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	gws "github.com/gorilla/websocket"

	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/sshbridge"
	gatewayws "rook-servicechannel-gateway/internal/websocket"
)

const outboundQueueSize = 16

type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	logger   *slog.Logger
}

type Session struct {
	id         string
	grant      grants.ValidationResult
	browser    gatewayws.Connection
	console    sshbridge.Session
	logger     *slog.Logger
	cleanup    CleanupHook
	manager    *Registry
	outbound   chan gatewayws.Message
	closed     chan struct{}
	closeOnce  sync.Once
	stateMu    sync.RWMutex
	state      BrowserState
	startedAt  time.Time
	lastActive time.Time
	endedAt    time.Time
	endReason  EndReason
}

type loopResult struct {
	reason      EndReason
	closeCode   int
	closeReason string
}

func NewRegistry(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{sessions: make(map[string]*Session), logger: logger}
}

func (r *Registry) Start(ctx context.Context, request StartRequest) (Handle, error) {
	if request.Browser == nil {
		return nil, fmt.Errorf("browser connection is required")
	}
	if request.Bridge == nil {
		return nil, fmt.Errorf("ssh bridge is required")
	}
	if request.Grant.IPAddress == "" {
		return nil, fmt.Errorf("validated grant is missing target ip")
	}

	console, err := request.Bridge.Open(ctx, sshbridge.SessionRequest{
		IPAddress: request.Grant.IPAddress,
		Account:   request.SSHAccount,
		Term:      "xterm-256color",
		Rows:      24,
		Columns:   80,
	})
	if err != nil {
		return nil, fmt.Errorf("open ssh bridge: %w", err)
	}

	now := time.Now().UTC()
	session := &Session{
		id:         newSessionID(),
		grant:      request.Grant,
		browser:    request.Browser,
		console:    console,
		logger:     request.Logger,
		cleanup:    request.CleanupHook,
		manager:    r,
		outbound:   make(chan gatewayws.Message, outboundQueueSize),
		closed:     make(chan struct{}),
		state:      BrowserStateActive,
		startedAt:  now,
		lastActive: now,
	}
	if session.logger == nil {
		session.logger = r.logger
	}

	r.mu.Lock()
	r.sessions[session.id] = session
	r.mu.Unlock()

	go session.run(ctx)
	return session, nil
}

func (r *Registry) CloseAll(ctx context.Context, reason EndReason) error {
	r.mu.RLock()
	sessions := make([]*Session, 0, len(r.sessions))
	for _, current := range r.sessions {
		sessions = append(sessions, current)
	}
	r.mu.RUnlock()

	for _, current := range sessions {
		current.close(reason, gws.CloseGoingAway, string(reason))
	}
	return nil
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

func (s *Session) ID() string {
	return s.id
}

func (s *Session) Snapshot() Snapshot {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return Snapshot{ID: s.id, State: s.state, Grant: s.grant, StartedAt: s.startedAt, LastActivity: s.lastActive, EndedAt: s.endedAt, EndReason: s.endReason, OutputBacklog: len(s.outbound)}
}

func (s *Session) Enqueue(message gatewayws.Message) error {
	select {
	case <-s.closed:
		return fmt.Errorf("session closed")
	default:
	}
	select {
	case s.outbound <- message:
		return nil
	default:
		s.close(EndReasonSlowClient, gws.ClosePolicyViolation, "outbound queue exhausted")
		return fmt.Errorf("outbound queue exhausted")
	}
}

func (s *Session) run(ctx context.Context) {
	writeDone := make(chan struct{})
	results := make(chan loopResult, 2)
	go s.writeLoop(ctx, writeDone)
	go func() { results <- s.outputLoop() }()
	go func() { results <- s.readLoop(ctx) }()

	result := <-results
	s.close(result.reason, result.closeCode, result.closeReason)
	<-writeDone
}

func (s *Session) readLoop(ctx context.Context) loopResult {
	for {
		message, err := s.browser.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return loopResult{reason: EndReasonServerShutdown, closeCode: gws.CloseGoingAway, closeReason: string(EndReasonServerShutdown)}
			}
			if gatewayws.IsPeerClosed(err) {
				return loopResult{reason: EndReasonBrowserDisconnect, closeCode: gws.CloseNormalClosure, closeReason: string(EndReasonBrowserDisconnect)}
			}
			if s.logger != nil {
				s.logger.Error("websocket read failed", "sessionID", s.id, "error", err)
			}
			return loopResult{reason: EndReasonInternalError, closeCode: gws.CloseInternalServerErr, closeReason: "read failed"}
		}

		s.touch()
		parsed, err := gatewayws.ParseClientMessage(message)
		if err != nil {
			var protocolErr *gatewayws.ProtocolError
			if errors.As(err, &protocolErr) {
				s.writeImmediate(ctx, gatewayws.NewServerError(protocolErr.Code, protocolErr.Message))
				s.writeImmediate(ctx, gatewayws.NewServerClose(protocolErr.Message))
				return loopResult{reason: EndReasonProtocolViolation, closeCode: gws.ClosePolicyViolation, closeReason: protocolErr.Message}
			}
			return loopResult{reason: EndReasonInternalError, closeCode: gws.CloseInternalServerErr, closeReason: "message parsing failed"}
		}

		switch parsed.Type {
		case gatewayws.MessageTypeInput:
			payload := parsed.BinaryData
			if payload == nil {
				payload = []byte(parsed.Input)
			}
			if len(payload) == 0 {
				continue
			}
			if _, err := s.console.Write(payload); err != nil {
				return loopResult{reason: EndReasonSSHError, closeCode: gws.CloseInternalServerErr, closeReason: "ssh write failed"}
			}
		case gatewayws.MessageTypeResize:
			if err := s.console.Resize(ctx, sshbridge.PtySize{Rows: parsed.Rows, Columns: parsed.Columns}); err != nil {
				s.writeImmediate(ctx, gatewayws.NewServerError("resize_failed", err.Error()))
				return loopResult{reason: EndReasonSSHError, closeCode: gws.ClosePolicyViolation, closeReason: "resize_failed"}
			}
		case gatewayws.MessageTypeClose:
			reason := parsed.Reason
			if reason == "" {
				reason = string(EndReasonClientClose)
			}
			s.writeImmediate(ctx, gatewayws.NewServerClose(reason))
			return loopResult{reason: EndReasonClientClose, closeCode: gws.CloseNormalClosure, closeReason: reason}
		default:
			s.writeImmediate(ctx, gatewayws.NewServerError("unsupported_message", "message type is not supported in the current plan"))
			s.writeImmediate(ctx, gatewayws.NewServerClose("unsupported_message"))
			return loopResult{reason: EndReasonProtocolViolation, closeCode: gws.ClosePolicyViolation, closeReason: "unsupported_message"}
		}
	}
}

func (s *Session) outputLoop() loopResult {
	buffer := make([]byte, 4096)
	for {
		count, err := s.console.Read(buffer)
		if count > 0 {
			s.touch()
			if enqueueErr := s.Enqueue(gatewayws.NewServerOutput(buffer[:count])); enqueueErr != nil {
				return loopResult{reason: EndReasonSlowClient, closeCode: gws.ClosePolicyViolation, closeReason: "outbound queue exhausted"}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.writeImmediate(context.Background(), gatewayws.NewServerClose(string(EndReasonConsoleClosed)))
				return loopResult{reason: EndReasonConsoleClosed, closeCode: gws.CloseNormalClosure, closeReason: string(EndReasonConsoleClosed)}
			}
			if s.logger != nil {
				s.logger.Error("ssh output read failed", "sessionID", s.id, "error", err)
			}
			s.writeImmediate(context.Background(), gatewayws.NewServerError("ssh_read_failed", err.Error()))
			return loopResult{reason: EndReasonSSHError, closeCode: gws.CloseInternalServerErr, closeReason: "ssh_read_failed"}
		}
	}
}

func (s *Session) writeLoop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closed:
			return
		case message := <-s.outbound:
			if err := s.browser.WriteMessage(ctx, message); err != nil {
				if s.logger != nil && !gatewayws.IsPeerClosed(err) {
					s.logger.Error("websocket write failed", "sessionID", s.id, "error", err)
				}
				s.close(EndReasonBrowserDisconnect, gws.CloseAbnormalClosure, "write failed")
				return
			}
		}
	}
}

func (s *Session) writeImmediate(ctx context.Context, message gatewayws.Message) {
	if err := s.browser.WriteMessage(ctx, message); err != nil && s.logger != nil && !gatewayws.IsPeerClosed(err) {
		s.logger.Warn("failed to write immediate websocket message", "sessionID", s.id, "error", err)
	}
}

func (s *Session) close(reason EndReason, code int, closeReason string) {
	s.closeOnce.Do(func() {
		now := time.Now().UTC()
		s.stateMu.Lock()
		s.state = BrowserStateClosing
		s.lastActive = now
		s.endedAt = now
		s.endReason = reason
		s.stateMu.Unlock()
		close(s.closed)
		_ = s.browser.Close(code, closeReason)
		_ = s.console.Close()
		s.manager.mu.Lock()
		delete(s.manager.sessions, s.id)
		s.manager.mu.Unlock()
		s.stateMu.Lock()
		s.state = BrowserStateClosed
		s.stateMu.Unlock()
		if s.cleanup != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s.cleanup(cleanupCtx, s.Snapshot())
		}
	})
}

func (s *Session) touch() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.lastActive = time.Now().UTC()
}

func newSessionID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}
