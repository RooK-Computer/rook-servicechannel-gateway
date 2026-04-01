package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	gws "github.com/gorilla/websocket"

	"rook-servicechannel-gateway/internal/audit"
	"rook-servicechannel-gateway/internal/config"
	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/session"
	"rook-servicechannel-gateway/internal/sshbridge"
	gatewayws "rook-servicechannel-gateway/internal/websocket"
)

type Server struct {
	logger     *slog.Logger
	sessions   *session.Registry
	httpServer *http.Server
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(cfg config.Config, logger *slog.Logger, grantValidator grants.Validator, bridge sshbridge.Bridge) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	sessions := newSessionRegistry(cfg, logger)
	handler := NewHandler(cfg, logger, grantValidator, bridge, sessions)
	return &Server{logger: logger, sessions: sessions, httpServer: &http.Server{Addr: cfg.HTTP.ListenAddress, Handler: handler, ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout}}
}

func NewHandler(cfg config.Config, logger *slog.Logger, grantValidator grants.Validator, bridge sshbridge.Bridge, sessions *session.Registry) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if sessions == nil {
		sessions = newSessionRegistry(cfg, logger)
	}

	upgrader := gatewayws.NewUpgrader(gatewayws.UpgraderConfig{
		MaxMessageBytes: cfg.WebSocket.MaxMessageBytes,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Code: "method_not_allowed", Message: "use GET"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Code: "method_not_allowed", Message: "use GET"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("/gateway/terminal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Code: "method_not_allowed", Message: "use GET"})
			return
		}
		if !hasUpgradeHeaders(r.Header) {
			writeJSON(w, http.StatusUpgradeRequired, errorResponse{Code: "upgrade_required", Message: "websocket upgrade headers are required"})
			return
		}

		browserConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			if logger != nil {
				logger.Warn("websocket upgrade failed", "error", err)
			}
			return
		}
		go handleBrowserSocket(cfg, logger, browserConn, sessions, grantValidator, bridge)
	})
	return mux
}

func newSessionRegistry(cfg config.Config, logger *slog.Logger) *session.Registry {
	return session.NewRegistry(logger, session.RegistryConfig{
		IdleTimeout:        cfg.Session.IdleTimeout,
		MaxConcurrent:      cfg.Session.MaxConcurrent,
		OutboundQueueDepth: cfg.Session.OutboundQueueDepth,
		AuditSink:          audit.NewLoggerSink(logger),
	})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		err := s.httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutting down http server")
		_ = s.sessions.CloseAll(context.Background(), session.EndReasonServerShutdown)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
}

func classifyGrantError(err error) (int, errorResponse) {
	var invalidGrant *grants.InvalidGrantError
	if errors.As(err, &invalidGrant) {
		return http.StatusForbidden, errorResponse{Code: invalidGrant.Code, Message: invalidGrant.Message}
	}
	var backendErr *grants.BackendError
	if errors.As(err, &backendErr) {
		message := backendErr.Message
		if strings.TrimSpace(message) == "" {
			message = "backend validation failed"
		}
		code := backendErr.Code
		if strings.TrimSpace(code) == "" {
			code = "backend_validation_failed"
		}
		return http.StatusBadGateway, errorResponse{Code: code, Message: message}
	}
	var transportErr *grants.TransportError
	if errors.As(err, &transportErr) {
		return http.StatusBadGateway, errorResponse{Code: "backend_unreachable", Message: transportErr.Error()}
	}
	return http.StatusBadGateway, errorResponse{Code: "grant_validation_failed", Message: err.Error()}
}

func handleBrowserSocket(cfg config.Config, logger *slog.Logger, browserConn gatewayws.Connection, sessions *session.Registry, grantValidator grants.Validator, bridge sshbridge.Bridge) {
	authorizeCtx, cancel := context.WithTimeout(context.Background(), cfg.Session.IdleTimeout)
	defer cancel()

	message, err := browserConn.ReadMessage(authorizeCtx)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			writeWebSocketFailure(browserConn, gws.ClosePolicyViolation, "authorize_timeout", "authorization message not received before timeout", string(session.EndReasonIdleTimeout))
		case errors.Is(err, context.Canceled), gatewayws.IsPeerClosed(err):
			_ = browserConn.Close(gws.CloseNormalClosure, string(session.EndReasonBrowserDisconnect))
		default:
			if logger != nil {
				logger.Error("websocket authorize read failed", "error", err)
			}
			writeWebSocketFailure(browserConn, gws.CloseInternalServerErr, "websocket_read_failed", "failed to read authorization message", string(session.EndReasonInternalError))
		}
		return
	}

	parsed, err := gatewayws.ParseClientMessage(message)
	if err != nil {
		var protocolErr *gatewayws.ProtocolError
		if errors.As(err, &protocolErr) {
			writeWebSocketFailure(browserConn, gws.ClosePolicyViolation, protocolErr.Code, protocolErr.Message, string(session.EndReasonProtocolViolation))
			return
		}
		writeWebSocketFailure(browserConn, gws.CloseInternalServerErr, "message_parsing_failed", "message parsing failed", string(session.EndReasonInternalError))
		return
	}

	switch parsed.Type {
	case gatewayws.MessageTypeClose:
		reason := parsed.Reason
		if reason == "" {
			reason = string(session.EndReasonClientClose)
		}
		_ = browserConn.WriteMessage(context.Background(), gatewayws.NewServerClose(reason))
		_ = browserConn.Close(gws.CloseNormalClosure, reason)
		return
	case gatewayws.MessageTypeAuthorize:
	default:
		writeWebSocketFailure(browserConn, gws.ClosePolicyViolation, "unexpected_first_message", "authorize must be the first client message", string(session.EndReasonProtocolViolation))
		return
	}

	if grantValidator == nil {
		writeWebSocketFailure(browserConn, gws.CloseInternalServerErr, "grant_validator_unavailable", "grant validation is not configured", string(session.EndReasonInternalError))
		return
	}

	validationResult, err := grantValidator.ValidateToken(authorizeCtx, parsed.Token)
	if err != nil {
		closeCode, closeReason := classifyGrantClose(err)
		payload := classifyGrantErrorPayload(err)
		writeWebSocketFailure(browserConn, closeCode, payload.Code, payload.Message, closeReason)
		return
	}

	handle, err := sessions.Start(context.Background(), session.StartRequest{
		Grant:           validationResult,
		Browser:         browserConn,
		Bridge:          bridge,
		SSHAccount:      cfg.SSH.Username,
		Logger:          logger,
		InitialMessages: []gatewayws.Message{gatewayws.NewServerAuthorized()},
	})
	if err != nil {
		if errors.Is(err, session.ErrSessionLimitReached) {
			writeWebSocketFailure(browserConn, gws.CloseTryAgainLater, "session_limit_reached", "gateway session capacity exhausted", string(session.EndReasonSessionLimit))
			if logger != nil {
				logger.Warn("session start rejected", "reason", session.EndReasonSessionLimit, "ipAddress", validationResult.IPAddress)
			}
			return
		}
		writeWebSocketFailure(browserConn, gws.CloseInternalServerErr, "ssh_bridge_failed", err.Error(), "session_start_failed")
		if logger != nil {
			logger.Error("session start failed", "error", err)
		}
		return
	}
	if logger != nil {
		logger.Info("browser websocket session started", "sessionID", handle.ID(), "ipAddress", validationResult.IPAddress)
	}
}

func classifyGrantErrorPayload(err error) errorResponse {
	_, payload := classifyGrantError(err)
	return payload
}

func classifyGrantClose(err error) (int, string) {
	if grants.IsInvalidGrantError(err) {
		return gws.ClosePolicyViolation, "authorization_failed"
	}
	return gws.CloseInternalServerErr, string(session.EndReasonInternalError)
}

func writeWebSocketFailure(conn gatewayws.Connection, closeCode int, errorCode, message, closeReason string) {
	if errorCode != "" {
		_ = conn.WriteMessage(context.Background(), gatewayws.NewServerError(errorCode, message))
	}
	if closeReason != "" {
		_ = conn.WriteMessage(context.Background(), gatewayws.NewServerClose(closeReason))
	}
	_ = conn.Close(closeCode, closeReason)
}

func hasUpgradeHeaders(header http.Header) bool {
	if !headerContainsToken(header, "Connection", "upgrade") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(header.Get("Upgrade")), "websocket")
}

func headerContainsToken(header http.Header, key, wanted string) bool {
	for _, value := range header.Values(key) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), wanted) {
				return true
			}
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
