package audit

import "context"

type Sink interface {
	Record(context.Context, Event) error
}

type Event struct {
	Name    string
	Session string
	Fields  map[string]string
}
