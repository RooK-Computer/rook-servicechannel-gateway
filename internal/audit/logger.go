package audit

import (
	"context"
	"log/slog"
)

type LoggerSink struct {
	logger *slog.Logger
}

func NewLoggerSink(logger *slog.Logger) *LoggerSink {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoggerSink{logger: logger}
}

func (s *LoggerSink) Record(ctx context.Context, event Event) error {
	if s == nil || s.logger == nil {
		return nil
	}
	s.logger.InfoContext(ctx, "audit event",
		"event", event.Name,
		"sessionID", event.Session,
		"fields", event.Fields,
	)
	return nil
}
