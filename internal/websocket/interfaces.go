package websocket

import (
	"context"
	"time"
)

type FrameType string

const (
	TextFrame   FrameType = "text"
	BinaryFrame FrameType = "binary"
)

type Connection interface {
	ReadMessage(context.Context) (Message, error)
	WriteMessage(context.Context, Message) error
	WritePing(context.Context) error
	ConfigureKeepalive(time.Duration) error
	Close(code int, reason string) error
}

type Message struct {
	Type FrameType
	Data []byte
}
