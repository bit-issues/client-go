package clientgo_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	clientgo "github.com/bit-issues/client-go"
	"github.com/capcom6/go-restkit"
)

// newStubServer starts a stub HTTP server for handler and returns its base URL.
// The server is closed automatically when the test finishes.
func newStubServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv.URL
}

// newAPIServer starts a stub HTTP server for handler and returns a base URL
// that already carries the full API path, as a client behind a URL rewrite uses.
func newAPIServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()

	return newStubServer(t, handler) + "/api/v1"
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

	return mustNew(t, newAPIServer(t, handler))
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
// envelope {message, code?} (which has NO "errors" field) via
// restkit.APIError.ParseError, alongside restkit's status classification.
func TestErrorEnvelopeClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     int
		body       string
		wantMsg    string
		wantCode   int
		wantClient bool
	}{
		{
			name:       "400 validation without errors field",
			status:     http.StatusBadRequest,
			body:       `{"message":"title is required","code":400}`,
			wantMsg:    "title is required",
			wantCode:   http.StatusBadRequest,
			wantClient: true,
		},
		{
			name:       "401 unauthorized without code",
			status:     http.StatusUnauthorized,
			body:       `{"message":"missing or expired token"}`,
			wantMsg:    "missing or expired token",
			wantClient: true,
		},
		{
			name:       "403 inactive user",
			status:     http.StatusForbidden,
			body:       `{"message":"user account is not active","code":403}`,
			wantMsg:    "user account is not active",
			wantCode:   http.StatusForbidden,
			wantClient: true,
		},
		{
			name:       "404 not found",
			status:     http.StatusNotFound,
			body:       `{"message":"project not found"}`,
			wantMsg:    "project not found",
			wantClient: true,
		},
		{
			name:       "422 validation with details ignored",
			status:     http.StatusUnprocessableEntity,
			body:       `{"message":"invalid status value","code":422,"details":[{"field":"status"}]}`,
			wantMsg:    "invalid status value",
			wantCode:   http.StatusUnprocessableEntity,
			wantClient: true,
		},
		{
			name:       "409 generic client error",
			status:     http.StatusConflict,
			body:       `{"message":"task already exists"}`,
			wantMsg:    "task already exists",
			wantClient: true,
		},
		{
			name:     "500 server error",
			status:   http.StatusInternalServerError,
			body:     `{"message":"Internal Server Error","code":500}`,
			wantMsg:  "Internal Server Error",
			wantCode: http.StatusInternalServerError,
		},
		{
			name:    "503 server error without code",
			status:  http.StatusServiceUnavailable,
			body:    `{"message":"service unavailable"}`,
			wantMsg: "service unavailable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := client.GetProject(t.Context(), clientgo.Auth{}, "any-project")

			if err == nil {
				t.Fatal("GetProject() error = nil, want error")
			}

			apiErr, ok := restkit.AsAPIError(err)
			if !ok {
				t.Fatalf("restkit.AsAPIError(err) = false, err = %v", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}

			var envelope struct {
				Message string `json:"message"`
				Code    int    `json:"code,omitempty"`
			}
			if perr := apiErr.ParseError(&envelope); perr != nil {
				t.Fatalf("ParseError() error = %v", perr)
			}
			if envelope.Message != tc.wantMsg {
				t.Errorf("envelope.Message = %q, want %q", envelope.Message, tc.wantMsg)
			}
			if envelope.Code != tc.wantCode {
				t.Errorf("envelope.Code = %d, want %d", envelope.Code, tc.wantCode)
			}
			if !restkit.IsAPIError(err) {
				t.Error("restkit.IsAPIError(err) = false")
			}
			if got := restkit.IsClientError(err); got != tc.wantClient {
				t.Errorf("restkit.IsClientError(err) = %v, want %v", got, tc.wantClient)
			}
			if got := restkit.IsServerError(err); got != !tc.wantClient {
				t.Errorf("restkit.IsServerError(err) = %v, want %v", got, !tc.wantClient)
			}
		})
	}
}

// TestTransportErrorIsTyped proves that a network failure leaves the client as
// a restkit infrastructure error and is not classified as an API error.
func TestTransportErrorIsTyped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	baseURL := srv.URL + "/api/v1"
	srv.Close()

	client := mustNew(t, baseURL, clientgo.WithHTTPClient(&http.Client{
		Transport: &http.Transport{Proxy: nil},
	}))

	_, err := client.GetProject(t.Context(), clientgo.Auth{}, "any-project")

	if err == nil {
		t.Fatal("GetProject() error = nil, want transport error")
	}
	if !restkit.IsInfrastructureError(err) {
		t.Errorf("restkit.IsInfrastructureError(err) = false, err = %v", err)
	}
	if !strings.Contains(err.Error(), "infrastructure error") {
		t.Errorf("err.Error() = %q, want it to contain %q", err.Error(), "infrastructure error")
	}
	if restkit.IsAPIError(err) {
		t.Errorf("restkit.IsAPIError(err) = true for a transport failure, err = %v", err)
	}
	if _, ok := restkit.AsAPIError(err); ok {
		t.Errorf("restkit.AsAPIError(err) = true for a transport failure, err = %v", err)
	}
}
