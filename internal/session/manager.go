package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	gws "github.com/gorilla/websocket"

	"rook-servicechannel-gateway/internal/grants"
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

	now := time.Now().UTC()
	session := &Session{
		id:         newSessionID(),
		grant:      request.Grant,
		browser:    request.Browser,
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
	return Snapshot{
		ID:            s.id,
		State:         s.state,
		Grant:         s.grant,
		StartedAt:     s.startedAt,
		LastActivity:  s.lastActive,
		EndedAt:       s.endedAt,
		EndReason:     s.endReason,
		OutputBacklog: len(s.outbound),
	}
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
	go s.writeLoop(ctx, writeDone)

	reason, closeCode, closeReason := s.readLoop(ctx)
	s.close(reason, closeCode, closeReason)
	<-writeDone
}

func (s *Session) readLoop(ctx context.Context) (EndReason, int, string) {
	for {
		message, err := s.browser.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return EndReasonServerShutdown, gws.CloseGoingAway, string(EndReasonServerShutdown)
			}
			if gatewayws.IsPeerClosed(err) {
				return EndReasonBrowserDisconnect, gws.CloseNormalClosure, string(EndReasonBrowserDisconnect)
			}
			var protocolErr *gatewayws.ProtocolError
			if errors.As(err, &protocolErr) {
				s.writeImmediate(ctx, gatewayws.NewServerError(protocolErr.Code, protocolErr.Message))
				s.writeImmediate(ctx, gatewayws.NewServerClose(protocolErr.Message))
				return EndReasonProtocolViolation, gws.CloseUnsupportedData, protocolErr.Message
			}
			if s.logger != nil {
				s.logger.Error("websocket read failed", "sessionID", s.id, "error", err)
			}
			return EndReasonInternalError, gws.CloseInternalServerErr, "read failed"
		}

		s.touch()

		parsed, err := gatewayws.ParseClientMessage(message)
		if err != nil {
			var protocolErr *gatewayws.ProtocolError
			if errors.As(err, &protocolErr) {
				s.writeImmediate(ctx, gatewayws.NewServerError(protocolErr.Code, protocolErr.Message))
				s.writeImmediate(ctx, gatewayws.NewServerClose(protocolErr.Message))
				return EndReasonProtocolViolation, gws.ClosePolicyViolation, protocolErr.Message
			}
			return EndReasonInternalError, gws.CloseInternalServerErr, "message parsing failed"
		}

		switch parsed.Type {
		case gatewayws.MessageTypeInput:
			continue
		case gatewayws.MessageTypeResize:
			continue
		case gatewayws.MessageTypeClose:
			reason := parsed.Reason
			if reason == "" {
				reason = string(EndReasonClientClose)
			}
			s.writeImmediate(ctx, gatewayws.NewServerClose(reason))
			return EndReasonClientClose, gws.CloseNormalClosure, reason
		default:
			s.writeImmediate(ctx, gatewayws.NewServerError("unsupported_message", "message type is not supported in the current plan"))
			s.writeImmediate(ctx, gatewayws.NewServerClose("unsupported_message"))
			return EndReasonProtocolViolation, gws.ClosePolicyViolation, "unsupported_message"
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
