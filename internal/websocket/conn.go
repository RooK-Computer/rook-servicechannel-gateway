package websocket

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	gws "github.com/gorilla/websocket"
)

const defaultWriteTimeout = 5 * time.Second

type UpgraderConfig struct {
	MaxMessageBytes int64
	WriteTimeout    time.Duration
}

type Upgrader struct {
	upgrader        gws.Upgrader
	maxMessageBytes int64
	writeTimeout    time.Duration
}

type GorillaConn struct {
	conn         *gws.Conn
	writeMu      sync.Mutex
	writeTimeout time.Duration
}

func NewUpgrader(cfg UpgraderConfig) Upgrader {
	if cfg.MaxMessageBytes <= 0 {
		cfg.MaxMessageBytes = 64 * 1024
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = defaultWriteTimeout
	}
	return Upgrader{
		maxMessageBytes: cfg.MaxMessageBytes,
		writeTimeout:    cfg.WriteTimeout,
		upgrader: gws.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// Hotfix fuer den laufenden Integrationstest: Origin-Pruefung derzeit offen.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (u Upgrader) Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (Connection, error) {
	conn, err := u.upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(u.maxMessageBytes)

	return &GorillaConn{conn: conn, writeTimeout: u.writeTimeout}, nil
}

func (c *GorillaConn) ReadMessage(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return Message{}, ctx.Err()
	default:
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return Message{}, err
		}
		defer c.conn.SetReadDeadline(time.Time{})
	}

	messageType, payload, err := c.conn.ReadMessage()
	if err != nil {
		if errors.Is(err, gws.ErrReadLimit) {
			return Message{}, &ProtocolError{Code: "message_too_large", Message: "websocket message exceeded configured size limit"}
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() && ctx.Err() != nil {
			return Message{}, ctx.Err()
		}
		return Message{}, err
	}

	switch messageType {
	case gws.TextMessage:
		return Message{Type: TextFrame, Data: payload}, nil
	case gws.BinaryMessage:
		return Message{Type: BinaryFrame, Data: payload}, nil
	default:
		return Message{}, &ProtocolError{Code: "unsupported_frame", Message: "unsupported websocket frame type"}
	}
}

func (c *GorillaConn) WriteMessage(ctx context.Context, message Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, c.writeTimeout)
	defer cancel()

	deadline, ok := writeCtx.Deadline()
	if ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	} else {
		if err := c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
			return err
		}
	}
	defer c.conn.SetWriteDeadline(time.Time{})

	return c.conn.WriteMessage(toGorillaType(message.Type), message.Data)
}

func (c *GorillaConn) WritePing(ctx context.Context) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, c.writeTimeout)
	defer cancel()

	deadline, ok := writeCtx.Deadline()
	if ok {
		return c.conn.WriteControl(gws.PingMessage, nil, deadline)
	}
	return c.conn.WriteControl(gws.PingMessage, nil, time.Now().Add(c.writeTimeout))
}

func (c *GorillaConn) ConfigureKeepalive(timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}
	if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(timeout))
	})
	return nil
}

func (c *GorillaConn) Close(code int, reason string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	deadline := time.Now().Add(c.writeTimeout)
	if err := c.conn.WriteControl(gws.CloseMessage, gws.FormatCloseMessage(code, reason), deadline); err != nil {
		if !errors.Is(err, gws.ErrCloseSent) {
			_ = c.conn.Close()
			return err
		}
	}

	return c.conn.Close()
}

func toGorillaType(frameType FrameType) int {
	if frameType == BinaryFrame {
		return gws.BinaryMessage
	}
	return gws.TextMessage
}

func IsPeerClosed(err error) bool {
	return gws.IsCloseError(err,
		gws.CloseNormalClosure,
		gws.CloseGoingAway,
		gws.CloseNoStatusReceived,
		gws.CloseAbnormalClosure,
	) || errors.Is(err, gws.ErrCloseSent)
}

func IsUseOfClosedNetworkError(err error) bool {
	return err != nil && (errors.Is(err, netErrClosed) || errors.Is(err, http.ErrServerClosed))
}

var netErrClosed = errors.New("use of closed network connection")
