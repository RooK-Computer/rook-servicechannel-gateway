package websocket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type MessageType string

const (
	MessageTypeInput  MessageType = "input"
	MessageTypeOutput MessageType = "output"
	MessageTypeResize MessageType = "resize"
	MessageTypeError  MessageType = "error"
	MessageTypeClose  MessageType = "close"
)

type ProtocolError struct {
	Code    string
	Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type ClientMessage struct {
	Type       MessageType
	Input      string
	BinaryData []byte
	Rows       int
	Columns    int
	Reason     string
}

type controlMessage struct {
	Type string `json:"type"`
}

type inputMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type resizeMessage struct {
	Type    string `json:"type"`
	Rows    int    `json:"rows"`
	Columns int    `json:"columns"`
}

type errorMessage struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type closeMessage struct {
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"`
}

func ParseClientMessage(message Message) (ClientMessage, error) {
	if message.Type == BinaryFrame {
		return ClientMessage{Type: MessageTypeInput, BinaryData: append([]byte(nil), message.Data...)}, nil
	}

	var base controlMessage
	if err := decodeStrict(message.Data, &base); err != nil {
		return ClientMessage{}, &ProtocolError{Code: "invalid_json", Message: "control message must be valid JSON"}
	}

	switch MessageType(strings.TrimSpace(base.Type)) {
	case MessageTypeInput:
		var parsed inputMessage
		if err := decodeStrict(message.Data, &parsed); err != nil {
			return ClientMessage{}, &ProtocolError{Code: "invalid_input", Message: "input message is malformed"}
		}
		return ClientMessage{Type: MessageTypeInput, Input: parsed.Data}, nil
	case MessageTypeResize:
		var parsed resizeMessage
		if err := decodeStrict(message.Data, &parsed); err != nil {
			return ClientMessage{}, &ProtocolError{Code: "invalid_resize", Message: "resize message is malformed"}
		}
		if parsed.Rows <= 0 || parsed.Columns <= 0 {
			return ClientMessage{}, &ProtocolError{Code: "invalid_resize", Message: "resize rows and columns must be greater than zero"}
		}
		return ClientMessage{Type: MessageTypeResize, Rows: parsed.Rows, Columns: parsed.Columns}, nil
	case MessageTypeClose:
		var parsed closeMessage
		if err := decodeStrict(message.Data, &parsed); err != nil {
			return ClientMessage{}, &ProtocolError{Code: "invalid_close", Message: "close message is malformed"}
		}
		return ClientMessage{Type: MessageTypeClose, Reason: strings.TrimSpace(parsed.Reason)}, nil
	case MessageTypeOutput:
		return ClientMessage{}, &ProtocolError{Code: "unexpected_output", Message: "output messages are server-to-client only"}
	case MessageTypeError:
		return ClientMessage{}, &ProtocolError{Code: "unexpected_error", Message: "error messages are server-to-client only"}
	case "authorize", "authorized":
		return ClientMessage{}, &ProtocolError{Code: "deprecated_authorization_message", Message: "authorization messages are not part of the active protocol; use the handshake header"}
	default:
		return ClientMessage{}, &ProtocolError{Code: "unknown_message_type", Message: "unsupported control message type"}
	}
}

func NewServerError(code, message string) Message {
	payload := errorMessage{Type: string(MessageTypeError), Code: code, Message: message}
	return Message{Type: TextFrame, Data: mustJSON(payload)}
}

func NewServerClose(reason string) Message {
	payload := closeMessage{Type: string(MessageTypeClose), Reason: reason}
	return Message{Type: TextFrame, Data: mustJSON(payload)}
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected trailing content")
	}
	return nil
}

func mustJSON(payload any) []byte {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return encoded
}
