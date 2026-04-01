package websocket

import "context"

type Connection interface {
	ReadMessage(context.Context) (Message, error)
	WriteMessage(context.Context, Message) error
	Close(code int, reason string) error
}

type Message struct {
	Type string
	Data []byte
}
