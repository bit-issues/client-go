package clientgo_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientgo "github.com/bit-issues/client-go"
)

// projectFixture is the wire payload of GET /api/v1/projects/{slug}.
const projectFixture = `{"id":"backend-service","name":"Backend Service",` +
	`"repo_url":"https://bitbucket.org/company/backend",` +
	`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-02T09:00:00Z"}`

// mustNewSession builds a Session via clientgo.NewSession and fails the test
// on error.
func mustNewSession(t *testing.T, baseURL string, tokens clientgo.Tokens) *clientgo.Session {
	t.Helper()

	session, err := clientgo.NewSession(baseURL, tokens)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	return session
}

// TestSessionUsesConstructorToken checks (1): the first read goes out with
// the constructor's access token as bearer - no exchange endpoints touched.
func TestSessionUsesConstructorToken(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/backend-service":
			if got := r.Header.Get("Authorization"); got != "Bearer ACCESS-1" {
				t.Errorf("Authorization = %q, want %q", got, "Bearer ACCESS-1")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(projectFixture))
		case "/api/v1/auth/refresh", "/api/v1/auth/login":
			t.Errorf("exchange endpoint %q hit, want the constructor token used directly", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "ACCESS-1",
		RefreshToken: "REFRESH-0",
	})

	project, err := session.GetProject(t.Context(), "backend-service")

	if err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}
	if project == nil || project.ID != "backend-service" {
		t.Errorf("project = %+v, want ID backend-service", project)
	}
}

// TestSessionRefreshesOnceOn401 checks (2): a401 triggers exactly ONE
// refresh (byte-exact body) and the original request is retried exactly once
// with the SWAPPED access token - exact event order, 1 refresh, 2 reads.
func TestSessionRefreshesOnceOn401(t *testing.T) {
	t.Parallel()

	const (
		wantRefreshCalls = 1
		wantReadCalls    = 2
	)

	events := &eventLog{}

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/backend-service":
			switch r.Header.Get("Authorization") {
			case "Bearer ACCESS-1":
				events.record("read-401")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			case "Bearer ACCESS-2":
				events.record("read-200")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(projectFixture))
			default:
				t.Errorf("Authorization = %q, want ACCESS-1 or ACCESS-2", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusInternalServerError)
			}
		case "/api/v1/auth/refresh":
			events.record("refresh")
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
				return
			}
			const wantRefreshBody = `{"refresh_token":"REFRESH-0"}`
			if string(body) != wantRefreshBody {
				t.Errorf("refresh body = %q, want %q", string(body), wantRefreshBody)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-2","refresh_token":"ROTATED"}`))
		case "/api/v1/auth/login":
			t.Error("login endpoint hit, a Session never logs in")
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "ACCESS-1",
		RefreshToken: "REFRESH-0",
	})

	if _, err := session.GetProject(t.Context(), "backend-service"); err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}

	got := events.all()
	if want := []string{"read-401", "refresh", "read-200"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	if refreshes := countEvents(got, "refresh"); refreshes != wantRefreshCalls {
		t.Errorf("refresh calls = %d, want %d", refreshes, wantRefreshCalls)
	}
	if reads := countEvents(got, "read-401") + countEvents(got, "read-200"); reads != wantReadCalls {
		t.Errorf("project reads = %d, want %d (original + one retry)", reads, wantReadCalls)
	}
}

// TestSessionSecond401IsUnauthorized checks (3): when the retried request
// 401s AGAIN after a successful refresh the Session reports ErrUnauthorized
// and never issues a third request or a second refresh.
func TestSessionSecond401IsUnauthorized(t *testing.T) {
	t.Parallel()

	const (
		wantRefreshCalls = 1
		wantReadCalls    = 2
	)

	events := &eventLog{}

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/backend-service":
			events.record("read-401")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		case "/api/v1/auth/refresh":
			events.record("refresh")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-2","refresh_token":"ROTATED"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "ACCESS-1",
		RefreshToken: "REFRESH-0",
	})

	_, err := session.GetProject(t.Context(), "backend-service")

	if !errors.Is(err, clientgo.ErrUnauthorized) {
		t.Fatalf("errors.Is(err, ErrUnauthorized) = false, err = %v", err)
	}
	if !clientgo.IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(err) = false, err = %v", err)
	}

	got := events.all()
	if want := []string{"read-401", "refresh", "read-401"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v (no third call)", got, want)
	}
	if refreshes := countEvents(got, "refresh"); refreshes != wantRefreshCalls {
		t.Errorf("refresh calls = %d, want %d", refreshes, wantRefreshCalls)
	}
	if reads := countEvents(got, "read-401"); reads != wantReadCalls {
		t.Errorf("project reads = %d, want %d", reads, wantReadCalls)
	}
}

// TestSessionRefreshRejectedIsSessionExpired checks (4): a refresh token the
// backend rejects (HTTP 401) surfaces as the typed ErrSessionExpired with no
// retry and no token value in the message.
func TestSessionRefreshRejectedIsSessionExpired(t *testing.T) {
	t.Parallel()

	events := &eventLog{}

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/backend-service":
			events.record("read-401")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		case "/api/v1/auth/refresh":
			events.record("refresh")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"refresh token revoked"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "ACCESS-1",
		RefreshToken: "REFRESH-0",
	})

	_, err := session.GetProject(t.Context(), "backend-service")

	if !errors.Is(err, clientgo.ErrSessionExpired) {
		t.Fatalf("errors.Is(err, ErrSessionExpired) = false, err = %v", err)
	}
	if !strings.Contains(err.Error(), "session expired") {
		t.Errorf("err.Error() = %q, want the session-expired message", err.Error())
	}
	if strings.Contains(err.Error(), "REFRESH-0") {
		t.Errorf("err.Error() leaks the refresh token: %q", err.Error())
	}

	got := events.all()
	if want := []string{"read-401", "refresh"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v (rejected refresh, no retry)", got, want)
	}
}

// TestSessionRefreshServerErrorIsNotExpired checks (5): a transient 5xx from
// the refresh endpoint keeps the ErrServer sentinel - it must NEVER map to
// ErrSessionExpired.
func TestSessionRefreshServerErrorIsNotExpired(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/backend-service":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		case "/api/v1/auth/refresh":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "ACCESS-1",
		RefreshToken: "REFRESH-0",
	})

	_, err := session.GetProject(t.Context(), "backend-service")

	if !errors.Is(err, clientgo.ErrServer) {
		t.Fatalf("errors.Is(err, ErrServer) = false, err = %v", err)
	}
	if errors.Is(err, clientgo.ErrSessionExpired) {
		t.Errorf("errors.Is(err, ErrSessionExpired) = true for a transient failure, err = %v", err)
	}
	if !clientgo.IsServerError(err) {
		t.Errorf("IsServerError(err) = false, err = %v", err)
	}
}

// TestSessionStaticStyleNeverRefreshes checks (6): tokens without a refresh
// token make the Session static-style - a 401 is surfaced as
// ErrUnauthorized and the refresh endpoint is never called.
func TestSessionStaticStyleNeverRefreshes(t *testing.T) {
	t.Parallel()

	events := &eventLog{}

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/backend-service":
			events.record("read-401")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		case "/api/v1/auth/refresh":
			t.Error("refresh endpoint hit for a static-style Session, want none")
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{AccessToken: "ACCESS-1"})

	_, err := session.GetProject(t.Context(), "backend-service")

	if !errors.Is(err, clientgo.ErrUnauthorized) {
		t.Fatalf("errors.Is(err, ErrUnauthorized) = false, err = %v", err)
	}
	if !clientgo.IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(err) = false, err = %v", err)
	}
	if got := events.all(); len(got) != 1 {
		t.Errorf("events = %v, want exactly one401 and no retry", got)
	}
}

// TestSessionTokensReflectRotation checks (7): Tokens() is a snapshot of the
// CURRENT pair - the constructor's pair before a rotation, the NEW pair
// (including the rotated refresh token) after one. This is the save-on-exit
// bridge.
func TestSessionTokensReflectRotation(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/backend-service":
			switch r.Header.Get("Authorization") {
			case "Bearer ACCESS-1":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			default:
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(projectFixture))
			}
		case "/api/v1/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-2","refresh_token":"ROTATED"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "ACCESS-1",
		RefreshToken: "REFRESH-0",
	})

	if got, want := session.Tokens(), (clientgo.Tokens{AccessToken: "ACCESS-1", RefreshToken: "REFRESH-0"}); got != want {
		t.Errorf("Tokens() before = %+v, want %+v", got, want)
	}

	if _, err := session.GetProject(t.Context(), "backend-service"); err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}

	if got, want := session.Tokens(), (clientgo.Tokens{AccessToken: "ACCESS-2", RefreshToken: "ROTATED"}); got != want {
		t.Errorf("Tokens() after = %+v, want the rotated pair %+v", got, want)
	}
}

// TestSessionLogoutRevokesAndClears checks (8): Logout revokes the in-memory
// refresh token (byte-exact body, 204 EMPTY never decoded), clears BOTH
// tokens in memory, and a second Logout is a no-op without any HTTP call.
func TestSessionLogoutRevokesAndClears(t *testing.T) {
	t.Parallel()

	const wantLogoutCalls = 1

	var logoutCalls atomic.Int64

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

		logoutCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "ACCESS-1",
		RefreshToken: "REFRESH-0",
	})

	if err := session.Logout(t.Context()); err != nil {
		t.Fatalf("Logout() error = %v, want nil for 204 with empty body", err)
	}
	if got := session.Tokens(); got != (clientgo.Tokens{}) {
		t.Errorf("Tokens() after Logout = %+v, want the pair cleared", got)
	}
	if err := session.Logout(t.Context()); err != nil {
		t.Fatalf("second Logout() error = %v, want nil for an empty session", err)
	}
	if got := logoutCalls.Load(); got != wantLogoutCalls {
		t.Errorf("logout calls = %d, want %d (second call must skip the HTTP round trip)", got, wantLogoutCalls)
	}
}

// TestSessionLogoutWaitsForFlightAndRevokesRotatedToken checks (12): Logout
// coordinated with the single-flight refresh waits for a gated rotation to
// complete, revokes the FINAL (rotated) refresh token, and leaves Tokens()
// empty - a completing refresh can never restore credentials after logout.
// The revoked body is the deterministic ordering proof: only a logout that
// waited for the flight can observe the rotated token.
func TestSessionLogoutWaitsForFlightAndRevokesRotatedToken(t *testing.T) {
	t.Parallel()

	var (
		refreshHits    atomic.Int64
		rotatedRevoked atomic.Bool
	)

	refreshStarted := make(chan struct{})
	refreshRelease := make(chan struct{})

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			if refreshHits.Add(1) == 1 {
				close(refreshStarted)
			}
			<-refreshRelease
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-B","refresh_token":"ROTATED"}`))
		case "/api/v1/auth/logout":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
				return
			}
			if string(body) != `{"refresh_token":"ROTATED"}` {
				t.Errorf("logout body = %q, want the rotated token revoked", string(body))
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			rotatedRevoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/projects/backend-service":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "access-token-value",
		RefreshToken: "refresh-token-value",
	})

	// The gate must open before srv.Close waits on the handler, even when
	// the test bails out early: t.Cleanup runs in LIFO order.
	releaseRefresh := sync.OnceFunc(func() { close(refreshRelease) })
	t.Cleanup(releaseRefresh)

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		// The read 401s into a gated refresh; its own result is irrelevant
		// here - the flight it starts is what the logout must wait for.
		_, _ = session.GetProject(t.Context(), "backend-service")
	}()

	// The refresh flight is now gated: start the logout so it overlaps the
	// rotation, then release the gate.
	<-refreshStarted

	logoutDone := make(chan error, 1)
	go func() {
		logoutDone <- session.Logout(t.Context())
	}()

	releaseRefresh()

	if err := <-logoutDone; err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	<-readerDone

	if !rotatedRevoked.Load() {
		t.Error("the rotated refresh token was never revoked")
	}
	if got := session.Tokens(); got != (clientgo.Tokens{}) {
		t.Errorf("Tokens() after Logout = %+v, want the pair cleared", got)
	}
	if got := refreshHits.Load(); got != 1 {
		t.Errorf("refresh calls = %d, want exactly 1", got)
	}
}

// TestSessionConcurrentRefreshSingleFlight checks (9): N goroutines hitting a
// revoked access token simultaneously cause exactly ONE refresh (the handler
// is held until every goroutine received its 401, so the race window is
// guaranteed) while all requests succeed on their single retry; a Session
// never logs in.
func TestSessionConcurrentRefreshSingleFlight(t *testing.T) {
	t.Parallel()

	const concurrentRequests = 8

	var refreshHits, unauthorized atomic.Int64
	release := make(chan struct{})

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			<-release
			refreshHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-B","refresh_token":"ROTATED"}`))
		case "/api/v1/auth/login":
			t.Error("login endpoint hit, a Session never logs in")
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/projects/backend-service":
			switch r.Header.Get("Authorization") {
			case "Bearer access-token-value":
				if unauthorized.Add(1) == concurrentRequests {
					close(release)
				}
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			case "Bearer ACCESS-B":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(projectFixture))
			default:
				t.Errorf("Authorization = %q, want access-token-value or ACCESS-B", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusInternalServerError)
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "access-token-value",
		RefreshToken: "refresh-token-value",
	})

	errs := make([]error, concurrentRequests)
	var wg sync.WaitGroup
	for i := range concurrentRequests {
		wg.Go(func() {
			_, errs[i] = session.GetProject(t.Context(), "backend-service")
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: GetProject() error = %v", i, err)
		}
	}
	if got := refreshHits.Load(); got != 1 {
		t.Errorf("refresh calls = %d, want exactly 1 (single-flight)", got)
	}
	if got := unauthorized.Load(); got != concurrentRequests {
		t.Errorf("401 responses = %d, want %d (every goroutine raced)", got, concurrentRequests)
	}
}

// TestSessionRefreshLeaderCancellationDoesNotFailSharedFlight adapts the
// owner's gate to the two-client architecture: cancelling the leader while
// the rotation is gated returns the leader immediately WITHOUT aborting the
// shared refresh (detached, bounded flight context), so waiters retry with
// the NEW access token, exactly ONE refresh happens and the rotated pair is
// stored - only the leader's own request fails.
func TestSessionRefreshLeaderCancellationDoesNotFailSharedFlight(t *testing.T) {
	t.Parallel()

	const waiters = 7

	var refreshHits atomic.Int64
	refreshStarted := make(chan struct{})
	refreshRelease := make(chan struct{})

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			if refreshHits.Add(1) == 1 {
				close(refreshStarted)
			}
			<-refreshRelease
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-B","refresh_token":"ROTATED"}`))
		case "/api/v1/auth/login":
			t.Error("login endpoint hit, a Session never logs in")
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/projects/backend-service":
			switch r.Header.Get("Authorization") {
			case "Bearer access-token-value":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			case "Bearer ACCESS-B":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(projectFixture))
			default:
				t.Errorf("Authorization = %q, want access-token-value or ACCESS-B", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusInternalServerError)
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
		AccessToken:  "access-token-value",
		RefreshToken: "refresh-token-value",
	})

	// The gate must open before srv.Close waits on the handler, even when the
	// test bails out early: t.Cleanup runs in LIFO order.
	releaseRefresh := sync.OnceFunc(func() { close(refreshRelease) })
	t.Cleanup(releaseRefresh)

	leaderCtx, leaderCancel := context.WithCancel(t.Context())
	defer leaderCancel()

	leaderDone := make(chan error, 1)
	go func() {
		_, err := session.GetProject(leaderCtx, "backend-service")
		leaderDone <- err
	}()

	// The leader is now inside the refresh handler (the old access token is
	// still the live one): start the waiters so they 401 and join the SAME
	// flight, then cancel only the leader.
	<-refreshStarted

	started := make(chan struct{}, waiters)
	errs := make([]error, waiters)
	var wg sync.WaitGroup
	for i := range waiters {
		wg.Go(func() {
			started <- struct{}{}
			_, errs[i] = session.GetProject(t.Context(), "backend-service")
		})
	}
	for range waiters {
		<-started
	}
	leaderCancel()

	// The cancelled leader must return WHILE the refresh gate is still
	// closed; only then is the early-return guarantee exercised.
	var leaderErr error
	select {
	case leaderErr = <-leaderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled leader still blocked on the gated refresh")
	}
	releaseRefresh()

	wg.Wait()

	if leaderErr == nil {
		t.Errorf("leader GetProject() error = nil, want failure from its cancelled context")
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("waiter %d: GetProject() error = %v", i, err)
		}
	}
	if got := refreshHits.Load(); got != 1 {
		t.Errorf("refresh calls = %d, want exactly 1 (flight survives leader cancellation)", got)
	}
	if got := session.Tokens(); got != (clientgo.Tokens{AccessToken: "ACCESS-B", RefreshToken: "ROTATED"}) {
		t.Errorf("Tokens() after the detached flight = %+v, want the rotated pair", got)
	}
}

// TestSessionEmptyTokensIsLegal checks (11): constructing with Tokens{} is
// legal - the first read goes out with no bearer, the 401 surfaces as typed
// ErrUnauthorized, and no exchange endpoint is ever called.
func TestSessionEmptyTokensIsLegal(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/backend-service":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("Authorization = %q, want empty", got)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		case "/api/v1/auth/refresh", "/api/v1/auth/login":
			t.Errorf("exchange endpoint %q hit for an empty session, want none", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{})

	_, err := session.GetProject(t.Context(), "backend-service")

	if !errors.Is(err, clientgo.ErrUnauthorized) {
		t.Fatalf("errors.Is(err, ErrUnauthorized) = false, err = %v", err)
	}
	if !clientgo.IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(err) = false, err = %v", err)
	}
}

// TestErrorStringsLeakNoSecrets checks (10): the password and both token
// values are absent from every error string this suite can provoke, across
// the stateless exchanges and the Session flows.
func TestErrorStringsLeakNoSecrets(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"pw-secret",
		"ACCESS-1", "ACCESS-2", "ACCESS-B",
		"REFRESH-0", "ROTATED",
		"access-token-value", "refresh-token-value",
	}

	assertNoLeak := func(t *testing.T, err error) {
		t.Helper()

		if err == nil {
			t.Fatal("error = nil, want an error to scan")
		}
		for _, secret := range forbidden {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("err.Error() leaks %q: %q", secret, err.Error())
			}
		}
	}

	t.Run("stateless login failure carries the password", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"invalid credentials"}`))
		}
		client := mustNew(t, newStubServer(t, handler))

		_, err := client.Login(t.Context(), "u@x.example", "pw-secret")

		assertNoLeak(t, err)
	})

	t.Run("stateless refresh rejection", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"refresh token revoked"}`))
		}
		client := mustNew(t, newStubServer(t, handler))

		_, err := client.Refresh(t.Context(), "REFRESH-0")

		assertNoLeak(t, err)
	})

	t.Run("session refresh rejected", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/projects/backend-service":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			case "/api/v1/auth/refresh":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"refresh token revoked"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}
		session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
			AccessToken:  "ACCESS-1",
			RefreshToken: "REFRESH-0",
		})

		_, err := session.GetProject(t.Context(), "backend-service")

		assertNoLeak(t, err)
	})

	t.Run("session second 401 after rotation", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/projects/backend-service":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			case "/api/v1/auth/refresh":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"ACCESS-2","refresh_token":"ROTATED"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}
		session := mustNewSession(t, newStubServer(t, handler), clientgo.Tokens{
			AccessToken:  "ACCESS-1",
			RefreshToken: "REFRESH-0",
		})

		_, err := session.GetProject(t.Context(), "backend-service")

		assertNoLeak(t, err)
	})
}
