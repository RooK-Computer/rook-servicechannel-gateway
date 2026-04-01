package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"rook-servicechannel-gateway/internal/config"
	"rook-servicechannel-gateway/internal/grants"
)

type Server struct {
	logger     *slog.Logger
	httpServer *http.Server
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(cfg config.Config, logger *slog.Logger, grantValidator grants.Validator) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	_ = grantValidator

	handler := NewHandler(cfg, logger)

	return &Server{
		logger: logger,
		httpServer: &http.Server{
			Addr:              cfg.HTTP.ListenAddress,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

func NewHandler(cfg config.Config, logger *slog.Logger) http.Handler {
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

		if strings.TrimSpace(r.Header.Get(cfg.HTTP.GrantHeaderName)) == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{Code: "missing_grant_header", Message: "terminal grant header is required"})
			return
		}

		if logger != nil {
			logger.Info("terminal handshake placeholder reached")
		}
		writeJSON(w, http.StatusNotImplemented, errorResponse{Code: "not_implemented", Message: "websocket session handling will be added in plan 02"})
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
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
