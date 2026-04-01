package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	gws "github.com/gorilla/websocket"

	"rook-servicechannel-gateway/internal/audit"
	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/sshbridge"
	gatewayws "rook-servicechannel-gateway/internal/websocket"
)

const (
	defaultOutboundQueueSize = 16
	defaultIdleTimeout       = 2 * time.Minute
	defaultMaxConcurrent     = 32
	outboundQueueSize        = defaultOutboundQueueSize
)

var ErrSessionLimitReached = errors.New("session limit reached")

type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	logger   *slog.Logger
	cfg      RegistryConfig
}

type Session struct {
	id          string
	grant       grants.ValidationResult
	sshAccount  string
	browser     gatewayws.Connection
	console     sshbridge.Session
	logger      *slog.Logger
	auditSink   audit.Sink
	cleanup     CleanupHook
	manager     *Registry
	outbound    chan gatewayws.Message
	closed      chan struct{}
	closeOnce   sync.Once
	idleTimeout time.Duration
	stateMu     sync.RWMutex
	state       BrowserState
	startedAt   time.Time
	lastActive  time.Time
	endedAt     time.Time
	endReason   EndReason
}

type loopResult struct {
	reason            EndReason
	closeCode         int
	closeReason       string
	clientErrorCode   string
	clientErrorText   string
	clientCloseReason string
}

func NewRegistry(logger *slog.Logger, configs ...RegistryConfig) *Registry {
	if logger == nil {
		logger = slog.Default()
	}

	cfg := RegistryConfig{
		IdleTimeout:        defaultIdleTimeout,
		MaxConcurrent:      defaultMaxConcurrent,
		OutboundQueueDepth: defaultOutboundQueueSize,
	}
	if len(configs) > 0 {
		if configs[0].IdleTimeout > 0 {
			cfg.IdleTimeout = configs[0].IdleTimeout
		}
		if configs[0].MaxConcurrent > 0 {
			cfg.MaxConcurrent = configs[0].MaxConcurrent
		}
		if configs[0].OutboundQueueDepth > 0 {
			cfg.OutboundQueueDepth = configs[0].OutboundQueueDepth
		}
		cfg.AuditSink = configs[0].AuditSink
	}

	return &Registry{sessions: make(map[string]*Session), logger: logger, cfg: cfg}
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

	if r.atCapacity() {
		r.recordRegistryEvent("session_rejected", map[string]string{
			"reason":        string(EndReasonSessionLimit),
			"ipAddress":     request.Grant.IPAddress,
			"maxConcurrent": fmt.Sprintf("%d", r.cfg.MaxConcurrent),
		})
		return nil, ErrSessionLimitReached
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
		id:          newSessionID(),
		grant:       request.Grant,
		sshAccount:  request.SSHAccount,
		browser:     request.Browser,
		console:     console,
		logger:      request.Logger,
		auditSink:   r.cfg.AuditSink,
		cleanup:     request.CleanupHook,
		manager:     r,
		outbound:    make(chan gatewayws.Message, r.cfg.OutboundQueueDepth),
		closed:      make(chan struct{}),
		idleTimeout: r.cfg.IdleTimeout,
		state:       BrowserStateActive,
		startedAt:   now,
		lastActive:  now,
	}
	if session.logger == nil {
		session.logger = r.logger
	}

	r.mu.Lock()
	if r.isAtCapacityLocked() {
		r.mu.Unlock()
		_ = console.Close()
		r.recordRegistryEvent("session_rejected", map[string]string{
			"reason":        string(EndReasonSessionLimit),
			"ipAddress":     request.Grant.IPAddress,
			"maxConcurrent": fmt.Sprintf("%d", r.cfg.MaxConcurrent),
		})
		return nil, ErrSessionLimitReached
	}
	r.sessions[session.id] = session
	r.mu.Unlock()

	session.logStart()
	session.recordAuditEvent(context.Background(), "session_started", map[string]string{
		"ipAddress":             session.grant.IPAddress,
		"sshAccount":            session.sshAccount,
		"idleTimeout":           session.idleTimeout.String(),
		"outboundQueueDepth":    fmt.Sprintf("%d", cap(session.outbound)),
		"maxConcurrentSessions": fmt.Sprintf("%d", r.cfg.MaxConcurrent),
	})

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

func (r *Registry) atCapacity() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isAtCapacityLocked()
}

func (r *Registry) isAtCapacityLocked() bool {
	return r.cfg.MaxConcurrent > 0 && len(r.sessions) >= r.cfg.MaxConcurrent
}

func (r *Registry) recordRegistryEvent(name string, fields map[string]string) {
	if r.logger != nil {
		args := []any{"event", name}
		for key, value := range fields {
			args = append(args, key, value)
		}
		r.logger.Warn("session registry event", args...)
	}
	if r.cfg.AuditSink != nil {
		_ = r.cfg.AuditSink.Record(context.Background(), audit.Event{Name: name, Fields: fields})
	}
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
		s.close(EndReasonSlowClient, gws.ClosePolicyViolation, "outbound_queue_exhausted")
		return fmt.Errorf("outbound queue exhausted")
	}
}

func (s *Session) run(ctx context.Context) {
	writeDone := make(chan struct{})
	results := make(chan loopResult, 3)
	go s.writeLoop(ctx, writeDone)
	go func() { results <- s.outputLoop() }()
	go func() { results <- s.readLoop(ctx) }()
	go func() {
		if result, ok := s.idleLoop(ctx); ok {
			results <- result
		}
	}()

	result := <-results
	result.flush(context.Background(), s)
	s.close(result.reason, result.closeCode, result.closeReason)
	<-writeDone
}

func (s *Session) readLoop(ctx context.Context) loopResult {
	for {
		message, err := s.browser.ReadMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return serverShutdownResult()
			}
			if gatewayws.IsPeerClosed(err) {
				return browserDisconnectResult()
			}
			if s.logger != nil {
				s.logger.Error("websocket read failed", "sessionID", s.id, "error", err)
			}
			return internalErrorResult("websocket_read_failed", "websocket read failed")
		}

		s.touch()
		parsed, err := gatewayws.ParseClientMessage(message)
		if err != nil {
			var protocolErr *gatewayws.ProtocolError
			if errors.As(err, &protocolErr) {
				return protocolViolationResult(protocolErr)
			}
			return internalErrorResult("message_parsing_failed", "message parsing failed")
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
				return sshErrorResult("ssh_write_failed", "failed to write input to ssh session")
			}
		case gatewayws.MessageTypeResize:
			if err := s.console.Resize(ctx, sshbridge.PtySize{Rows: parsed.Rows, Columns: parsed.Columns}); err != nil {
				return sshErrorResult("resize_failed", err.Error())
			}
		case gatewayws.MessageTypeClose:
			reason := parsed.Reason
			if reason == "" {
				reason = string(EndReasonClientClose)
			}
			return clientCloseResult(reason)
		default:
			return protocolViolationResult(&gatewayws.ProtocolError{Code: "unsupported_message", Message: "message type is not supported in the current plan"})
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
				return loopResult{reason: EndReasonSlowClient, closeCode: gws.ClosePolicyViolation, closeReason: "outbound_queue_exhausted"}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return consoleClosedResult()
			}
			if s.logger != nil {
				s.logger.Error("ssh output read failed", "sessionID", s.id, "error", err)
			}
			return sshErrorResult("ssh_read_failed", err.Error())
		}
	}
}

func (s *Session) idleLoop(ctx context.Context) (loopResult, bool) {
	interval := s.idleTimeout / 4
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	if interval > time.Second {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return serverShutdownResult(), true
		case <-s.closed:
			return loopResult{}, false
		case <-ticker.C:
			s.stateMu.RLock()
			lastActive := s.lastActive
			s.stateMu.RUnlock()
			if time.Since(lastActive) >= s.idleTimeout {
				return idleTimeoutResult(s.idleTimeout), true
			}
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
				s.close(EndReasonBrowserDisconnect, gws.CloseAbnormalClosure, "write_failed")
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
		s.logEnd(code, closeReason)
		s.recordAuditEvent(context.Background(), "session_ended", map[string]string{
			"ipAddress":      s.grant.IPAddress,
			"sshAccount":     s.sshAccount,
			"endReason":      string(reason),
			"closeCode":      fmt.Sprintf("%d", code),
			"closeReason":    closeReason,
			"durationMillis": fmt.Sprintf("%d", now.Sub(s.startedAt).Milliseconds()),
		})
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

func (s *Session) logStart() {
	if s.logger == nil {
		return
	}
	s.logger.Info("session started",
		"sessionID", s.id,
		"ipAddress", s.grant.IPAddress,
		"sshAccount", s.sshAccount,
		"idleTimeout", s.idleTimeout.String(),
		"outboundQueueDepth", cap(s.outbound),
	)
}

func (s *Session) logEnd(closeCode int, closeReason string) {
	if s.logger == nil {
		return
	}

	args := []any{
		"sessionID", s.id,
		"ipAddress", s.grant.IPAddress,
		"sshAccount", s.sshAccount,
		"endReason", s.endReason,
		"closeCode", closeCode,
		"closeReason", closeReason,
		"duration", s.endedAt.Sub(s.startedAt).String(),
		"outputBacklog", len(s.outbound),
	}

	switch s.endReason {
	case EndReasonInternalError, EndReasonSSHError:
		s.logger.Error("session ended", args...)
	case EndReasonProtocolViolation, EndReasonSlowClient, EndReasonIdleTimeout, EndReasonSessionLimit:
		s.logger.Warn("session ended", args...)
	default:
		s.logger.Info("session ended", args...)
	}
}

func (s *Session) recordAuditEvent(ctx context.Context, name string, fields map[string]string) {
	if s.auditSink == nil {
		return
	}
	payload := map[string]string{
		"ipAddress":  s.grant.IPAddress,
		"sshAccount": s.sshAccount,
	}
	for key, value := range extractGrantAuditFields(s.grant.RawResponse) {
		payload[key] = value
	}
	for key, value := range fields {
		payload[key] = value
	}
	if err := s.auditSink.Record(ctx, audit.Event{Name: name, Session: s.id, Fields: payload}); err != nil && s.logger != nil {
		s.logger.Warn("audit record failed", "sessionID", s.id, "event", name, "error", err)
	}
}

func (r loopResult) flush(ctx context.Context, s *Session) {
	if r.clientErrorCode != "" {
		s.writeImmediate(ctx, gatewayws.NewServerError(r.clientErrorCode, r.clientErrorText))
	}
	if r.clientCloseReason != "" {
		s.writeImmediate(ctx, gatewayws.NewServerClose(r.clientCloseReason))
	}
}

func protocolViolationResult(err *gatewayws.ProtocolError) loopResult {
	return loopResult{
		reason:            EndReasonProtocolViolation,
		closeCode:         gws.ClosePolicyViolation,
		closeReason:       err.Code,
		clientErrorCode:   err.Code,
		clientErrorText:   err.Message,
		clientCloseReason: string(EndReasonProtocolViolation),
	}
}

func internalErrorResult(code, message string) loopResult {
	return loopResult{
		reason:            EndReasonInternalError,
		closeCode:         gws.CloseInternalServerErr,
		closeReason:       code,
		clientErrorCode:   code,
		clientErrorText:   message,
		clientCloseReason: string(EndReasonInternalError),
	}
}

func sshErrorResult(code, message string) loopResult {
	return loopResult{
		reason:            EndReasonSSHError,
		closeCode:         gws.CloseInternalServerErr,
		closeReason:       code,
		clientErrorCode:   code,
		clientErrorText:   message,
		clientCloseReason: string(EndReasonSSHError),
	}
}

func slowClientResult() loopResult {
	return loopResult{
		reason:            EndReasonSlowClient,
		closeCode:         gws.ClosePolicyViolation,
		closeReason:       "outbound_queue_exhausted",
		clientErrorCode:   "slow_client",
		clientErrorText:   "browser output queue exhausted",
		clientCloseReason: string(EndReasonSlowClient),
	}
}

func consoleClosedResult() loopResult {
	return loopResult{
		reason:            EndReasonConsoleClosed,
		closeCode:         gws.CloseNormalClosure,
		closeReason:       string(EndReasonConsoleClosed),
		clientCloseReason: string(EndReasonConsoleClosed),
	}
}

func browserDisconnectResult() loopResult {
	return loopResult{
		reason:      EndReasonBrowserDisconnect,
		closeCode:   gws.CloseNormalClosure,
		closeReason: string(EndReasonBrowserDisconnect),
	}
}

func serverShutdownResult() loopResult {
	return loopResult{
		reason:            EndReasonServerShutdown,
		closeCode:         gws.CloseGoingAway,
		closeReason:       string(EndReasonServerShutdown),
		clientCloseReason: string(EndReasonServerShutdown),
	}
}

func clientCloseResult(reason string) loopResult {
	return loopResult{
		reason:            EndReasonClientClose,
		closeCode:         gws.CloseNormalClosure,
		closeReason:       reason,
		clientCloseReason: reason,
	}
}

func idleTimeoutResult(timeout time.Duration) loopResult {
	return loopResult{
		reason:            EndReasonIdleTimeout,
		closeCode:         gws.ClosePolicyViolation,
		closeReason:       string(EndReasonIdleTimeout),
		clientErrorCode:   "idle_timeout",
		clientErrorText:   fmt.Sprintf("session inactive for %s", timeout),
		clientCloseReason: string(EndReasonIdleTimeout),
	}
}

func extractGrantAuditFields(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	fields := map[string]string{}
	for _, candidate := range []struct {
		target string
		keys   []string
	}{
		{target: "pin", keys: []string{"pin", "supportPin"}},
		{target: "mitarbeiteraccount", keys: []string{"mitarbeiteraccount", "mitarbeiterAccount", "employeeAccount"}},
	} {
		for _, key := range candidate.keys {
			value, ok := payload[key]
			if !ok {
				continue
			}
			text, ok := value.(string)
			if ok && text != "" {
				fields[candidate.target] = text
				break
			}
		}
	}

	if len(fields) == 0 {
		return nil
	}
	return fields
}

func newSessionID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}
