package session

import (
	"context"

	"rook-servicechannel-gateway/internal/grants"
)

type Manager interface {
	Start(context.Context, StartRequest) (Handle, error)
	Close(context.Context, string, string) error
}

type StartRequest struct {
	Grant grants.ValidationResult
}

type Handle interface {
	ID() string
}
