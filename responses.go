package clientgo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// errorEnvelope is the backend error body {message, code?}. Details are
// ignored in v1 and the envelope deliberately has no "errors" field.
type errorEnvelope struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
}

// parseAPIError builds a typed *APIError from a non-2xx response body.
func parseAPIError(statusCode int, body []byte) error {
	var envelope errorEnvelope

	message := ""
	code := 0
	if err := json.Unmarshal(body, &envelope); err == nil {
		message = envelope.Message
		code = envelope.Code
	}

	if message == "" {
		message = apiErrorMessage(statusCode, body)
	}

	return &APIError{
		StatusCode: statusCode,
		Code:       code,
		Message:    message,
		sentinel:   apiErrorSentinel(statusCode),
	}
}

// apiErrorMessage derives a display message when the envelope carries none:
// a non-JSON body is used verbatim, anything else falls back to the status text.
func apiErrorMessage(statusCode int, body []byte) string {
	if !json.Valid(body) {
		trimmed := strings.TrimSpace(string(body))
		if trimmed != "" {
			return trimmed
		}
	}

	return http.StatusText(statusCode)
}

// decodeJSONBody decodes a response body into dst, treating an empty body as
// a success without a decode attempt.
func decodeJSONBody(body []byte, dst any) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("failed to decode response body: %w", err)
	}

	return nil
}
