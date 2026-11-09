package clientgo

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrAPIError is the root sentinel: every non-2xx API response wraps it.
var ErrAPIError = errors.New("api error")

// ErrClient classifies 4xx responses without a more specific sentinel.
var ErrClient = fmt.Errorf("%w: client error", ErrAPIError)

// ErrServer classifies 5xx responses.
var ErrServer = fmt.Errorf("%w: server error", ErrAPIError)

// ErrUnauthorized classifies 401 responses.
var ErrUnauthorized = fmt.Errorf("%w: unauthorized", ErrClient)

// ErrForbidden classifies 403 responses.
var ErrForbidden = fmt.Errorf("%w: forbidden", ErrClient)

// ErrNotFound classifies 404 responses.
var ErrNotFound = fmt.Errorf("%w: not found", ErrClient)

// ErrValidation classifies 400 and 422 responses.
var ErrValidation = fmt.Errorf("%w: validation failed", ErrClient)

// ErrTransport classifies network and transport failures such as refused
// connections, DNS failures and timeouts; it wraps the transport error too.
var ErrTransport = errors.New("transport error")

// APIError is the typed error returned for every non-2xx API response, parsed
// from the backend error envelope {message, code?} (details are ignored in v1).
type APIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int

	// Code is the application code from the error envelope; 0 when the
	// envelope omitted it.
	Code int

	// Message is the human-readable message from the error envelope.
	Message string

	// sentinel classifies the error for errors.Is.
	sentinel error
}

// Error reports the status code and the envelope message.
func (e *APIError) Error() string {
	return fmt.Sprintf("api error (status %d): %s", e.StatusCode, e.Message)
}

// Unwrap returns the sentinel that classifies this error.
func (e *APIError) Unwrap() error {
	return e.sentinel
}

// apiErrorSentinel maps an HTTP status code onto its sentinel error; codes
// below 500 without a specific sentinel fall back to ErrClient.
func apiErrorSentinel(statusCode int) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrValidation
	default:
		if statusCode >= http.StatusInternalServerError {
			return ErrServer
		}

		return ErrClient
	}
}

// IsAPIError reports whether err is any API error.
func IsAPIError(err error) bool { return errors.Is(err, ErrAPIError) }

// IsClientError reports whether err is a 4xx API error.
func IsClientError(err error) bool { return errors.Is(err, ErrClient) }

// IsServerError reports whether err is a 5xx API error.
func IsServerError(err error) bool { return errors.Is(err, ErrServer) }

// IsUnauthorized reports whether err is a 401 API error.
func IsUnauthorized(err error) bool { return errors.Is(err, ErrUnauthorized) }

// IsForbidden reports whether err is a 403 API error.
func IsForbidden(err error) bool { return errors.Is(err, ErrForbidden) }

// IsNotFound reports whether err is a 404 API error.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// IsValidation reports whether err is a 400 or 422 API error.
func IsValidation(err error) bool { return errors.Is(err, ErrValidation) }

// IsTransportError reports whether err is a network or transport failure.
func IsTransportError(err error) bool { return errors.Is(err, ErrTransport) }
