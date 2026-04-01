package websocket

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	gws "github.com/gorilla/websocket"
)

const writeTimeout = 5 * time.Second

type Upgrader struct {
	upgrader gws.Upgrader
}

type GorillaConn struct {
	conn    *gws.Conn
	writeMu sync.Mutex
}

func NewUpgrader() Upgrader {
	return Upgrader{
		upgrader: gws.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

func (u Upgrader) Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (Connection, error) {
	conn, err := u.upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return nil, err
	}

	return &GorillaConn{conn: conn}, nil
}

func (c *GorillaConn) ReadMessage(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return Message{}, ctx.Err()
	default:
	}

	messageType, payload, err := c.conn.ReadMessage()
	if err != nil {
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

	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	deadline, ok := writeCtx.Deadline()
	if ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	} else {
		if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return err
		}
	}
	defer c.conn.SetWriteDeadline(time.Time{})

	return c.conn.WriteMessage(toGorillaType(message.Type), message.Data)
}

func (c *GorillaConn) Close(code int, reason string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	deadline := time.Now().Add(writeTimeout)
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
