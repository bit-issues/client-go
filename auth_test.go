package clientgo_test

import (
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	clientgo "github.com/bit-issues/client-go"
)

// TestLogin checks the login round trip: POST /api/v1/auth/login with the
// byte-exact JSON body, no bearer header when none is configured, and the
// decoded LoginResponse including the nested user.
func TestLogin(t *testing.T) {
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
		const wantBody = `{"email":"alice@example.com","password":"password123"}`
		if string(body) != wantBody {
			t.Errorf("body = %q, want %q", string(body), wantBody)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-token-value",` +
			`"refresh_token":"refresh-token-value",` +
			`"user":{"id":1,"email":"alice@example.com","name":"alice",` +
			`"role":"admin","status":"active",` +
			`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-01T09:00:00Z"}}`))
	}

	client := newTestClient(t, handler)

	resp, err := client.Login(t.Context(), clientgo.LoginRequest{
		Email:    "alice@example.com",
		Password: "password123",
	})

	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if resp.AccessToken != "access-token-value" {
		t.Errorf("AccessToken = %q, want %q", resp.AccessToken, "access-token-value")
	}
	if resp.RefreshToken != "refresh-token-value" {
		t.Errorf("RefreshToken = %q, want %q", resp.RefreshToken, "refresh-token-value")
	}
	if resp.User.ID != 1 {
		t.Errorf("User.ID = %d, want 1", resp.User.ID)
	}
	if resp.User.Email != "alice@example.com" {
		t.Errorf("User.Email = %q, want %q", resp.User.Email, "alice@example.com")
	}
	if resp.User.Role != "admin" {
		t.Errorf("User.Role = %q, want %q", resp.User.Role, "admin")
	}
	if resp.User.Status != "active" {
		t.Errorf("User.Status = %q, want %q", resp.User.Status, "active")
	}
	if want := time.Date(2026, time.April, 1, 8, 0, 0, 0, time.UTC); !resp.User.CreatedAt.Equal(want) {
		t.Errorf("User.CreatedAt = %v, want %v", resp.User.CreatedAt, want)
	}
	if want := time.Date(2026, time.April, 1, 9, 0, 0, 0, time.UTC); !resp.User.UpdatedAt.Equal(want) {
		t.Errorf("User.UpdatedAt = %v, want %v", resp.User.UpdatedAt, want)
	}
}

// TestLoginAttachesBearerWhenConfigured locks the decided behaviour: a
// configured access token is attached to every request, including the public
// auth endpoints (harmless there).
func TestLoginAttachesBearerWhenConfigured(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer pre-set-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer pre-set-token")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh",` +
			`"user":{"id":1,"email":"alice@example.com","name":"alice",` +
			`"role":"admin","status":"active",` +
			`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-01T09:00:00Z"}}`))
	}

	client := clientgo.NewClient(
		clientgo.Config{}.WithBaseURL(newStubServer(t, handler)).WithJWTAuth("pre-set-token"),
	)

	_, err := client.Login(t.Context(), clientgo.LoginRequest{
		Email:    "alice@example.com",
		Password: "password123",
	})

	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
}

// TestLoginInactiveUserForbidden checks a non-active login becomes a typed
// ErrForbidden with the envelope message.
func TestLoginInactiveUserForbidden(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"user account is not active","code":403}`))
	})

	_, err := client.Login(t.Context(), clientgo.LoginRequest{
		Email:    "bob@example.com",
		Password: "password123",
	})

	if err == nil {
		t.Fatal("Login() error = nil, want error")
	}
	if !errors.Is(err, clientgo.ErrForbidden) {
		t.Errorf("errors.Is(err, ErrForbidden) = false, err = %v", err)
	}
	if !clientgo.IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false, err = %v", err)
	}

	var apiErr *clientgo.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(*APIError) = false, err = %v", err)
	}
	if apiErr.Message != "user account is not active" {
		t.Errorf("APIError.Message = %q, want %q", apiErr.Message, "user account is not active")
	}
}

// TestRefresh checks the refresh round trip: POST /api/v1/auth/refresh with
// the byte-exact JSON body and the rotated token pair.
func TestRefresh(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/v1/auth/refresh" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/auth/refresh")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		const wantBody = `{"refresh_token":"refresh-token-value"}`
		if string(body) != wantBody {
			t.Errorf("body = %q, want %q", string(body), wantBody)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"rotated-access","refresh_token":"rotated-refresh"}`))
	}

	client := newTestClient(t, handler)

	resp, err := client.Refresh(t.Context(), clientgo.RefreshRequest{
		RefreshToken: "refresh-token-value",
	})

	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if resp.AccessToken != "rotated-access" {
		t.Errorf("AccessToken = %q, want %q", resp.AccessToken, "rotated-access")
	}
	if resp.RefreshToken != "rotated-refresh" {
		t.Errorf("RefreshToken = %q, want %q", resp.RefreshToken, "rotated-refresh")
	}
}

// TestLogoutNoContent checks POST /api/v1/auth/logout: the backend answers
// 204 with no body and the client must not attempt a decode (a decode attempt
// would surface [io.EOF] as an error).
func TestLogoutNoContent(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/v1/auth/logout" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/auth/logout")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		const wantBody = `{"refresh_token":"refresh-token-value"}`
		if string(body) != wantBody {
			t.Errorf("body = %q, want %q", string(body), wantBody)
		}

		w.WriteHeader(http.StatusNoContent)
	}

	client := newTestClient(t, handler)

	err := client.Logout(t.Context(), clientgo.LogoutRequest{
		RefreshToken: "refresh-token-value",
	})

	if err != nil {
		t.Fatalf("Logout() error = %v, want nil for 204 with empty body", err)
	}
}
