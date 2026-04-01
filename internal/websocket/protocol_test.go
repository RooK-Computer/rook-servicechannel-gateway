package websocket

import "testing"

func TestParseClientMessageAuthorize(t *testing.T) {
	t.Parallel()

	message, err := ParseClientMessage(Message{Type: TextFrame, Data: []byte(`{"type":"authorize","token":"grant-123"}`)})
	if err != nil {
		t.Fatalf("ParseClientMessage() error = %v", err)
	}
	if message.Type != MessageTypeAuthorize || message.Token != "grant-123" {
		t.Fatalf("unexpected parsed authorize message %#v", message)
	}
}

func TestParseClientMessageRejectsEmptyAuthorizeToken(t *testing.T) {
	t.Parallel()

	_, err := ParseClientMessage(Message{Type: TextFrame, Data: []byte(`{"type":"authorize","token":" "}`)})
	if err == nil {
		t.Fatal("expected parse error")
	}

	protocolErr, ok := err.(*ProtocolError)
	if !ok {
		t.Fatalf("expected ProtocolError, got %T", err)
	}
	if protocolErr.Code != "invalid_authorize" {
		t.Fatalf("unexpected error code %q", protocolErr.Code)
	}
}

func TestParseClientMessageRejectsAuthorizedFromClient(t *testing.T) {
	t.Parallel()

	_, err := ParseClientMessage(Message{Type: TextFrame, Data: []byte(`{"type":"authorized"}`)})
	if err == nil {
		t.Fatal("expected parse error")
	}

	protocolErr, ok := err.(*ProtocolError)
	if !ok {
		t.Fatalf("expected ProtocolError, got %T", err)
	}
	if protocolErr.Code != "unexpected_authorized" {
		t.Fatalf("unexpected error code %q", protocolErr.Code)
	}
}

func TestNewServerAuthorized(t *testing.T) {
	t.Parallel()

	message := NewServerAuthorized()
	if message.Type != TextFrame {
		t.Fatalf("unexpected frame type %q", message.Type)
	}
	if string(message.Data) != `{"type":"authorized"}` {
		t.Fatalf("unexpected payload %s", string(message.Data))
	}
}
