package clientgo_test

import (
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	clientgo "github.com/bit-issues/client-go"
)

// loginOKFixture is the wire payload of a successful POST /auth/login.
const loginOKFixture = `{"access_token":"ACCESS-1","refresh_token":"REFRESH-0",` +
	`"user":{"id":1,"email":"u@x.example","name":"alice",` +
	`"role":"admin","status":"active",` +
	`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-01T09:00:00Z"}}`

// TestLoginReturnsTokensAndUser checks the stateless Login round trip:
// byte-exact JSON body, no Authorization header, and a LoginResult carrying
// BOTH tokens plus every profile field - no token in any returned error.
func TestLoginReturnsTokensAndUser(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/v1/auth/login" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/auth/login")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		const wantBody = `{"email":"u@x.example","password":"pw-secret"}`
		if string(body) != wantBody {
			t.Errorf("body = %q, want %q", string(body), wantBody)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(loginOKFixture))
	}

	client := mustNew(t, newAPIServer(t, handler))

	result, err := client.Login(t.Context(), "u@x.example", "pw-secret")

	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.Tokens.AccessToken != "ACCESS-1" {
		t.Errorf("Tokens.AccessToken = %q, want %q", result.Tokens.AccessToken, "ACCESS-1")
	}
	if result.Tokens.RefreshToken != "REFRESH-0" {
		t.Errorf("Tokens.RefreshToken = %q, want %q", result.Tokens.RefreshToken, "REFRESH-0")
	}
	if result.User.ID != 1 {
		t.Errorf("User.ID = %d, want 1", result.User.ID)
	}
	if result.User.Email != "u@x.example" {
		t.Errorf("User.Email = %q, want %q", result.User.Email, "u@x.example")
	}
	if result.User.Name != "alice" {
		t.Errorf("User.Name = %q, want %q", result.User.Name, "alice")
	}
	if result.User.Role != "admin" {
		t.Errorf("User.Role = %q, want %q", result.User.Role, "admin")
	}
	if result.User.Status != "active" {
		t.Errorf("User.Status = %q, want %q", result.User.Status, "active")
	}
	if want := "2026-04-01T08:00:00Z"; result.User.CreatedAt.Format("2006-01-02T15:04:05Z07:00") != want {
		t.Errorf("User.CreatedAt = %v, want %s", result.User.CreatedAt, want)
	}
}

// TestLoginRejectsEmptyCredentials checks Login rejects empty or whitespace
// email/password with the typed ErrInvalidCredentials BEFORE any request.
func TestLoginRejectsEmptyCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		email    string
		password string
	}{
		{name: "empty email", email: "", password: "pw-secret"},
		{name: "whitespace email", email: "  ", password: "pw-secret"},
		{name: "empty password", email: "u@x.example", password: ""},
		{name: "whitespace password", email: "u@x.example", password: "\t"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var hit atomic.Bool
			handler := func(w http.ResponseWriter, _ *http.Request) {
				hit.Store(true)
				w.WriteHeader(http.StatusInternalServerError)
			}
			client := mustNew(t, newAPIServer(t, handler))

			result, err := client.Login(t.Context(), tc.email, tc.password)

			if !errors.Is(err, clientgo.ErrInvalidCredentials) {
				t.Errorf("errors.Is(err, ErrInvalidCredentials) = false, err = %v", err)
			}
			if result.Tokens != (clientgo.Tokens{}) {
				t.Errorf("result.Tokens = %+v, want zero", result.Tokens)
			}
			if hit.Load() {
				t.Error("request was sent, want rejection before any HTTP call")
			}
		})
	}
}

// TestLoginFailureTaxonomy checks the shared API error parsing for Login:
// 401 -> ErrUnauthorized, 403 (non-active account) -> ErrForbidden,
// 5xx -> server error.
func TestLoginFailureTaxonomy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     int
		body       string
		wantSent   error
		wantHelper func(error) bool
	}{
		{
			name:       "401 unauthorized",
			status:     http.StatusUnauthorized,
			body:       `{"message":"invalid credentials"}`,
			wantSent:   clientgo.ErrUnauthorized,
			wantHelper: clientgo.IsUnauthorized,
		},
		{
			name:       "403 forbidden",
			status:     http.StatusForbidden,
			body:       `{"message":"user account is not active"}`,
			wantSent:   clientgo.ErrForbidden,
			wantHelper: clientgo.IsForbidden,
		},
		{
			name:       "500 server error",
			status:     http.StatusInternalServerError,
			body:       `{"message":"boom"}`,
			wantSent:   clientgo.ErrServer,
			wantHelper: clientgo.IsServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/auth/login" {
					t.Errorf("unexpected request on %q, want only the login call", r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)

					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}
			client := mustNew(t, newAPIServer(t, handler))

			_, err := client.Login(t.Context(), "u@x.example", "pw-secret")

			if !errors.Is(err, tc.wantSent) {
				t.Fatalf("errors.Is(err, %v) = false, err = %v", tc.wantSent, err)
			}
			if !tc.wantHelper(err) {
				t.Errorf("helper(err) = false, err = %v", err)
			}
		})
	}
}

// TestRefreshExchangesTokens checks the explicit in/out refresh: byte-exact
// JSON body with the caller's refresh token, and a Tokens result carrying the
// rotated pair - nothing is stored.
func TestRefreshExchangesTokens(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/v1/auth/refresh" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/auth/refresh")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		const wantBody = `{"refresh_token":"REFRESH-0"}`
		if string(body) != wantBody {
			t.Errorf("body = %q, want %q", string(body), wantBody)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ACCESS-2","refresh_token":"ROTATED"}`))
	}

	client := mustNew(t, newAPIServer(t, handler))

	tokens, err := client.Refresh(t.Context(), "REFRESH-0")

	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if tokens.AccessToken != "ACCESS-2" {
		t.Errorf("AccessToken = %q, want %q", tokens.AccessToken, "ACCESS-2")
	}
	if tokens.RefreshToken != "ROTATED" {
		t.Errorf("RefreshToken = %q, want %q", tokens.RefreshToken, "ROTATED")
	}
}

// TestRefreshRejectedIsSessionExpired checks a refresh token the backend
// rejects (401) classifies as ErrSessionExpired - while a 5xx keeps the
// transient ErrServer sentinel instead.
func TestRefreshRejectedIsSessionExpired(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		status      int
		body        string
		wantSent    error
		wantNotSent error
	}{
		{
			name:     "401 rejected token is session expired",
			status:   http.StatusUnauthorized,
			body:     `{"message":"refresh token revoked"}`,
			wantSent: clientgo.ErrSessionExpired,
		},
		{
			name:        "500 stays a transient server error",
			status:      http.StatusInternalServerError,
			body:        `{"message":"boom"}`,
			wantSent:    clientgo.ErrServer,
			wantNotSent: clientgo.ErrSessionExpired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}
			client := mustNew(t, newAPIServer(t, handler))

			_, err := client.Refresh(t.Context(), "REFRESH-0")

			if !errors.Is(err, tc.wantSent) {
				t.Fatalf("errors.Is(err, %v) = false, err = %v", tc.wantSent, err)
			}
			if tc.wantNotSent != nil && errors.Is(err, tc.wantNotSent) {
				t.Errorf("errors.Is(err, %v) = true, err = %v", tc.wantNotSent, err)
			}
		})
	}
}

// TestLogoutRevokesAndEmptyIsNoOp checks POST /api/v1/auth/logout: the 204
// EMPTY body is never decoded (a decode attempt would surface an error), the
// body is byte-exact, and an empty refresh token is a no-op that sends no
// request at all.
func TestLogoutRevokesAndEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	t.Run("revokes with the given token", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
			}
			if r.URL.Path != "/api/v1/auth/logout" {
				t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/auth/logout")
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
				return
			}
			const wantBody = `{"refresh_token":"REFRESH-0"}`
			if string(body) != wantBody {
				t.Errorf("body = %q, want %q", string(body), wantBody)
			}

			w.WriteHeader(http.StatusNoContent)
		}

		client := mustNew(t, newAPIServer(t, handler))

		if err := client.Logout(t.Context(), "REFRESH-0"); err != nil {
			t.Fatalf("Logout() error = %v, want nil for 204 with empty body", err)
		}
	})

	t.Run("empty token is a no-op", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected request %s %s, want none for an empty token", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}

		client := mustNew(t, newAPIServer(t, handler))

		if err := client.Logout(t.Context(), ""); err != nil {
			t.Fatalf("Logout() error = %v, want nil", err)
		}
	})
}

// countEvents returns how often event appears in events.
func countEvents(events []string, event string) int {
	var count int
	for _, e := range events {
		if e == event {
			count++
		}
	}

	return count
}
