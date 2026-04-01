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

	sessions := session.NewRegistry(logger)
	handler := NewHandler(cfg, logger, grantValidator, bridge, sessions)
	return &Server{logger: logger, sessions: sessions, httpServer: &http.Server{Addr: cfg.HTTP.ListenAddress, Handler: handler, ReadHeaderTimeout: 5 * time.Second}}
}

func NewHandler(cfg config.Config, logger *slog.Logger, grantValidator grants.Validator, bridge sshbridge.Bridge, sessions *session.Registry) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if sessions == nil {
		sessions = session.NewRegistry(logger)
	}

	upgrader := gatewayws.NewUpgrader()
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
		token := strings.TrimSpace(r.Header.Get(cfg.HTTP.GrantHeaderName))
		if token == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{Code: "missing_grant_header", Message: "terminal grant header is required"})
			return
		}
		if grantValidator == nil {
			writeJSON(w, http.StatusBadGateway, errorResponse{Code: "grant_validator_unavailable", Message: "grant validation is not configured"})
			return
		}
		validationResult, err := grantValidator.ValidateToken(r.Context(), token)
		if err != nil {
			status, payload := classifyGrantError(err)
			writeJSON(w, status, payload)
			return
		}

		browserConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			if logger != nil {
				logger.Warn("websocket upgrade failed", "error", err)
			}
			return
		}
		handle, err := sessions.Start(context.Background(), session.StartRequest{Grant: validationResult, Browser: browserConn, Bridge: bridge, SSHAccount: cfg.SSH.Username, Logger: logger})
		if err != nil {
			_ = browserConn.WriteMessage(context.Background(), gatewayws.NewServerError("ssh_bridge_failed", err.Error()))
			_ = browserConn.WriteMessage(context.Background(), gatewayws.NewServerClose("session_start_failed"))
			_ = browserConn.Close(gws.CloseInternalServerErr, "session start failed")
			if logger != nil {
				logger.Error("session start failed", "error", err)
			}
			return
		}
		if logger != nil {
			logger.Info("browser websocket session started", "sessionID", handle.ID(), "ipAddress", validationResult.IPAddress)
		}
	})
	return mux
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
