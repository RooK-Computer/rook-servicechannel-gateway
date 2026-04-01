package grants

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const validationPath = "/api/gateway/1/validateToken"

type Validator interface {
	ValidateToken(ctx context.Context, token string) (ValidationResult, error)
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type TerminalGrant struct {
	Token string `json:"token"`
}

type GrantValidationResponse struct {
	IPAddress string `json:"ipAddress"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationResult struct {
	IPAddress   string
	RawResponse json.RawMessage
}

type TransportError struct {
	Err error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("grant validation transport failed: %v", e.Err)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

type InvalidGrantError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *InvalidGrantError) Error() string {
	return fmt.Sprintf("grant rejected (%d): %s", e.StatusCode, e.Message)
}

type BackendError struct {
	StatusCode int
	Code       string
	Message    string
	Err        error
}

func (e *BackendError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("backend validation failed (%d): %s: %v", e.StatusCode, e.Message, e.Err)
	}
	return fmt.Sprintf("backend validation failed (%d): %s", e.StatusCode, e.Message)
}

func (e *BackendError) Unwrap() error {
	return e.Err
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) ValidateToken(ctx context.Context, token string) (ValidationResult, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return ValidationResult{}, &BackendError{Message: "token must not be empty"}
	}

	payload, err := json.Marshal(TerminalGrant{Token: token})
	if err != nil {
		return ValidationResult{}, fmt.Errorf("marshal terminal grant: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+validationPath, bytes.NewReader(payload))
	if err != nil {
		return ValidationResult{}, fmt.Errorf("build validation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ValidationResult{}, &TransportError{Err: err}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return ValidationResult{}, &BackendError{StatusCode: response.StatusCode, Message: "read backend response", Err: err}
	}

	if response.StatusCode == http.StatusOK {
		var parsed GrantValidationResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return ValidationResult{}, &BackendError{StatusCode: response.StatusCode, Message: "decode validation response", Err: err}
		}
		if strings.TrimSpace(parsed.IPAddress) == "" {
			return ValidationResult{}, &BackendError{StatusCode: response.StatusCode, Message: "validation response missing ipAddress"}
		}

		return ValidationResult{
			IPAddress:   parsed.IPAddress,
			RawResponse: append(json.RawMessage(nil), body...),
		}, nil
	}

	errorResponse := decodeErrorResponse(body)
	if isInvalidGrantStatus(response.StatusCode) {
		return ValidationResult{}, &InvalidGrantError{
			StatusCode: response.StatusCode,
			Code:       errorResponse.Code,
			Message:    errorResponse.Message,
		}
	}

	return ValidationResult{}, &BackendError{
		StatusCode: response.StatusCode,
		Code:       errorResponse.Code,
		Message:    errorResponse.Message,
	}
}

func decodeErrorResponse(body []byte) ErrorResponse {
	var parsed ErrorResponse
	if err := json.Unmarshal(body, &parsed); err == nil && (parsed.Code != "" || parsed.Message != "") {
		return parsed
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(http.StatusBadGateway)
	}

	return ErrorResponse{
		Code:    "backend_error",
		Message: message,
	}
}

func isInvalidGrantStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

func IsTransportError(err error) bool {
	var target *TransportError
	return errors.As(err, &target)
}

func IsInvalidGrantError(err error) bool {
	var target *InvalidGrantError
	return errors.As(err, &target)
}
