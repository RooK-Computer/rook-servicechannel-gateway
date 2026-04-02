package session

import (
	"context"
	"log/slog"
	"time"

	"rook-servicechannel-gateway/internal/audit"
	"rook-servicechannel-gateway/internal/grants"
	"rook-servicechannel-gateway/internal/sshbridge"
	gatewayws "rook-servicechannel-gateway/internal/websocket"
)

type Manager interface {
	Start(context.Context, StartRequest) (Handle, error)
	CloseAll(context.Context, EndReason) error
}

type StartRequest struct {
	Grant           grants.ValidationResult
	Browser         gatewayws.Connection
	Bridge          sshbridge.Bridge
	SSHAccount      string
	Logger          *slog.Logger
	InitialMessages []gatewayws.Message
	CleanupHook     CleanupHook
}

type CleanupHook func(context.Context, Snapshot)

type RegistryConfig struct {
	KeepaliveInterval  time.Duration
	KeepaliveTimeout   time.Duration
	MaxConcurrent      int
	OutboundQueueDepth int
	AuditSink          audit.Sink
}

type Handle interface {
	ID() string
}

type BrowserState string

type EndReason string

const (
	BrowserStateActive  BrowserState = "active"
	BrowserStateClosing BrowserState = "closing"
	BrowserStateClosed  BrowserState = "closed"

	EndReasonClientClose       EndReason = "client_close"
	EndReasonBrowserDisconnect EndReason = "browser_disconnect"
	EndReasonProtocolViolation EndReason = "protocol_violation"
	EndReasonSlowClient        EndReason = "slow_client"
	EndReasonServerShutdown    EndReason = "server_shutdown"
	EndReasonInternalError     EndReason = "internal_error"
	EndReasonConsoleClosed     EndReason = "console_closed"
	EndReasonSSHError          EndReason = "ssh_error"
	EndReasonAuthorizeTimeout  EndReason = "authorize_timeout"
	EndReasonKeepaliveTimeout  EndReason = "keepalive_timeout"
	EndReasonSessionLimit      EndReason = "session_limit_reached"
)

type Snapshot struct {
	ID            string
	State         BrowserState
	Grant         grants.ValidationResult
	StartedAt     time.Time
	LastActivity  time.Time
	EndedAt       time.Time
	EndReason     EndReason
	OutputBacklog int
}
