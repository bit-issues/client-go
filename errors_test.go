package clientgo_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	clientgo "github.com/bit-issues/client-go"
)

// newStubServer starts a stub HTTP server for handler and returns its base URL.
// The server is closed automatically when the test finishes.
func newStubServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv.URL
}

// mustNew builds a client via clientgo.New and fails the test on error.
func mustNew(t *testing.T, baseURL string, opts ...clientgo.Option) *clientgo.Client {
	t.Helper()

	client, err := clientgo.New(baseURL, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return client
}

// newTestClient starts a stub HTTP server for handler and returns a client for it.
func newTestClient(t *testing.T, handler http.HandlerFunc) *clientgo.Client {
	t.Helper()

	return mustNew(t, newStubServer(t, handler))
}

// eventLog collects ordered events from HTTP handlers, refresh-token handler
// calls and client lifecycle steps. Every access is mutex-guarded because
// handlers run on server goroutines while handlers/clients run on the test's
// own goroutine.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

// record appends an event to the log.
func (l *eventLog) record(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.events = append(l.events, event)
}

// all returns a snapshot of the recorded events.
func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.events)
}

// TestErrorEnvelopeClassification locks the parsing of the backend error
// envelope {message, code?} (which has NO "errors" field) into a typed
// *APIError, including status classification into the sentinel errors.
func TestErrorEnvelopeClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     int
		body       string
		wantSent   error
		wantHelper func(error) bool
		wantMsg    string
		wantCode   int
	}{
		{
			name:       "400 validation without errors field",
			status:     http.StatusBadRequest,
			body:       `{"message":"title is required","code":400}`,
			wantSent:   clientgo.ErrValidation,
			wantHelper: clientgo.IsValidation,
			wantMsg:    "title is required",
			wantCode:   http.StatusBadRequest,
		},
		{
			name:       "401 unauthorized without code",
			status:     http.StatusUnauthorized,
			body:       `{"message":"missing or expired token"}`,
			wantSent:   clientgo.ErrUnauthorized,
			wantHelper: clientgo.IsUnauthorized,
			wantMsg:    "missing or expired token",
			wantCode:   0,
		},
		{
			name:       "403 inactive user",
			status:     http.StatusForbidden,
			body:       `{"message":"user account is not active","code":403}`,
			wantSent:   clientgo.ErrForbidden,
			wantHelper: clientgo.IsForbidden,
			wantMsg:    "user account is not active",
			wantCode:   http.StatusForbidden,
		},
		{
			name:       "404 not found",
			status:     http.StatusNotFound,
			body:       `{"message":"project not found"}`,
			wantSent:   clientgo.ErrNotFound,
			wantHelper: clientgo.IsNotFound,
			wantMsg:    "project not found",
			wantCode:   0,
		},
		{
			name:       "422 validation with details ignored",
			status:     http.StatusUnprocessableEntity,
			body:       `{"message":"invalid status value","code":422,"details":[{"field":"status"}]}`,
			wantSent:   clientgo.ErrValidation,
			wantHelper: clientgo.IsValidation,
			wantMsg:    "invalid status value",
			wantCode:   http.StatusUnprocessableEntity,
		},
		{
			name:       "409 generic client error",
			status:     http.StatusConflict,
			body:       `{"message":"task already exists"}`,
			wantSent:   clientgo.ErrClient,
			wantHelper: clientgo.IsClientError,
			wantMsg:    "task already exists",
			wantCode:   0,
		},
		{
			name:       "500 server error",
			status:     http.StatusInternalServerError,
			body:       `{"message":"Internal Server Error","code":500}`,
			wantSent:   clientgo.ErrServer,
			wantHelper: clientgo.IsServerError,
			wantMsg:    "Internal Server Error",
			wantCode:   http.StatusInternalServerError,
		},
		{
			name:       "503 server error without code",
			status:     http.StatusServiceUnavailable,
			body:       `{"message":"service unavailable"}`,
			wantSent:   clientgo.ErrServer,
			wantHelper: clientgo.IsServerError,
			wantMsg:    "service unavailable",
			wantCode:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := client.GetProject(t.Context(), "any-project")

			if err == nil {
				t.Fatal("GetProject() error = nil, want error")
			}

			var apiErr *clientgo.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("errors.As(*APIError) = false, err = %v", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Code != tc.wantCode {
				t.Errorf("APIError.Code = %d, want %d", apiErr.Code, tc.wantCode)
			}
			if apiErr.Message != tc.wantMsg {
				t.Errorf("APIError.Message = %q, want %q", apiErr.Message, tc.wantMsg)
			}
			if want := fmt.Sprintf("api error (status %d): %s", tc.status, tc.wantMsg); err.Error() != want {
				t.Errorf("err.Error() = %q, want %q", err.Error(), want)
			}
			if !errors.Is(err, tc.wantSent) {
				t.Errorf("errors.Is(err, %v) = false", tc.wantSent)
			}
			if !errors.Is(err, clientgo.ErrAPIError) {
				t.Error("errors.Is(err, ErrAPIError) = false")
			}
			if !clientgo.IsAPIError(err) {
				t.Error("IsAPIError(err) = false")
			}
			if !tc.wantHelper(err) {
				t.Error("case helper(err) = false, want true")
			}

			wantClientErr := tc.status < http.StatusInternalServerError
			if got := clientgo.IsClientError(err); got != wantClientErr {
				t.Errorf("IsClientError(err) = %v, want %v", got, wantClientErr)
			}
			if got := clientgo.IsServerError(err); got != !wantClientErr {
				t.Errorf("IsServerError(err) = %v, want %v", got, !wantClientErr)
			}
		})
	}
}

// TestAPIErrorMessageFallbacks covers bodies that carry no usable envelope
// message: non-JSON payloads, empty bodies and JSON without a message.
func TestAPIErrorMessageFallbacks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name:   "non JSON body is used verbatim",
			status: http.StatusBadGateway,
			body:   "upstream exploded",
			want:   "upstream exploded",
		},
		{
			name:   "empty body falls back to status text",
			status: http.StatusInternalServerError,
			body:   "",
			want:   http.StatusText(http.StatusInternalServerError),
		},
		{
			name:   "json object without message falls back to status text",
			status: http.StatusInternalServerError,
			body:   `{"code":500}`,
			want:   http.StatusText(http.StatusInternalServerError),
		},
		{
			name:   "blank message falls back to status text",
			status: http.StatusBadGateway,
			body:   `{"message":""}`,
			want:   http.StatusText(http.StatusBadGateway),
		},
		{
			name:   "whitespace only body falls back to status text",
			status: http.StatusServiceUnavailable,
			body:   " \n ",
			want:   http.StatusText(http.StatusServiceUnavailable),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := client.GetProject(t.Context(), "any-project")

			var apiErr *clientgo.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("errors.As(*APIError) = false, err = %v", err)
			}
			if apiErr.Message != tc.want {
				t.Errorf("APIError.Message = %q, want %q", apiErr.Message, tc.want)
			}
		})
	}
}

// TestTransportErrorIsTyped proves that a network failure leaves the client as
// a typed ErrTransport error, never a bare transport error, and is not
// classified as an API error.
func TestTransportErrorIsTyped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	baseURL := srv.URL
	srv.Close()

	client := mustNew(t, baseURL, clientgo.WithHTTPClient(&http.Client{
		Transport: &http.Transport{Proxy: nil},
	}))

	_, err := client.GetProject(t.Context(), "any-project")

	if err == nil {
		t.Fatal("GetProject() error = nil, want transport error")
	}
	if !errors.Is(err, clientgo.ErrTransport) {
		t.Errorf("errors.Is(err, ErrTransport) = false, err = %v", err)
	}
	if !clientgo.IsTransportError(err) {
		t.Errorf("IsTransportError(err) = false, err = %v", err)
	}
	if !strings.Contains(err.Error(), "transport error") {
		t.Errorf("err.Error() = %q, want it to contain %q", err.Error(), "transport error")
	}
	if clientgo.IsAPIError(err) {
		t.Errorf("IsAPIError(err) = true for a transport failure, err = %v", err)
	}

	var apiErr *clientgo.APIError
	if errors.As(err, &apiErr) {
		t.Errorf("errors.As(*APIError) = true for a transport failure: %+v", apiErr)
	}
}
