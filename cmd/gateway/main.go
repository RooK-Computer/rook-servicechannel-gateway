package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"rook-servicechannel-gateway/internal/config"
	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/httpserver"
	"rook-servicechannel-gateway/internal/shutdown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gateway startup failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.Logging.Level)
	logger.Info("starting gateway",
		"listenAddress", cfg.HTTP.ListenAddress,
		"backendBaseURL", cfg.Backend.BaseURL,
		"grantHeaderName", cfg.HTTP.GrantHeaderName,
	)

	validator := grants.NewClient(cfg.Backend.BaseURL, cfg.Backend.ValidationTimeout)
	server := httpserver.New(cfg, logger, validator)

	ctx, stop := shutdown.NotifyContext(context.Background())
	defer stop()

	if err := server.ListenAndServe(ctx); err != nil {
		return err
	}

	logger.Info("gateway stopped")
	return nil
}

func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
