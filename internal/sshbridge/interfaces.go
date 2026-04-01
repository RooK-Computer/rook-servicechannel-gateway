package sshbridge

import "context"

type Bridge interface {
	Open(context.Context, SessionRequest) (Session, error)
}

type SessionRequest struct {
	IPAddress string
	Account   string
	Term      string
	Rows      int
	Columns   int
}

type Session interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(context.Context, PtySize) error
	Close() error
}

type PtySize struct {
	Rows    int
	Columns int
}
