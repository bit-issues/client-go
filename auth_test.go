package clientgo_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	clientgo "github.com/bit-issues/client-go"
)

// alphaLoginFixture is the wire payload of a successful internal login:
// ACCESS-1 becomes the first bearer, REFRESH-0 feeds the refresh flow.
const alphaLoginFixture = `{"access_token":"ACCESS-1","refresh_token":"REFRESH-0",` +
	`"user":{"id":1,"email":"u@x.example","name":"alice",` +
	`"role":"admin","status":"active",` +
	`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-01T09:00:00Z"}}`

// bootLoginFixture is the login payload used by the concurrency tests.
const bootLoginFixture = `{"access_token":"access-token-value","refresh_token":"refresh-token-value",` +
	`"user":{"id":1,"email":"u@x.example","name":"alice",` +
	`"role":"admin","status":"active",` +
	`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-01T09:00:00Z"}}`

// TestLoginAtFirstUse checks the login-per-boot model: with WithLogin set and
// no prior login, the FIRST auth-needing request performs the internal login
// (byte-exact JSON body), keeps the tokens in memory and proceeds with the
// login's access token as bearer. Exactly one login call, exact event order.
func TestLoginAtFirstUse(t *testing.T) {
	t.Parallel()

	events := &eventLog{}

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			events.record("login")
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want %q", got, "application/json")
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
				return
			}
			const wantLoginBody = `{"email":"u@x.example","password":"pw-secret"}`
			if string(body) != wantLoginBody {
				t.Errorf("login body = %q, want %q", string(body), wantLoginBody)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(alphaLoginFixture))
		case "/api/v1/projects/backend-service":
			if got := r.Header.Get("Authorization"); got != "Bearer ACCESS-1" {
				t.Errorf("Authorization = %q, want %q", got, "Bearer ACCESS-1")
			}
			events.record("read-200")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"backend-service","name":"Backend Service",` +
				`"repo_url":"https://bitbucket.org/company/backend",` +
				`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-02T09:00:00Z"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

	project, err := client.GetProject(t.Context(), "backend-service")

	if err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}
	if project == nil || project.ID != "backend-service" {
		t.Errorf("project = %+v, want ID backend-service", project)
	}

	got := events.all()
	if want := []string{"login", "read-200"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	if logins := countEvents(got, "login"); logins != 1 {
		t.Errorf("login calls = %d, want exactly 1", logins)
	}
}

// TestLoginFailure checks the login failure taxonomy through the shared API
// error parsing: 401 -> ErrUnauthorized, 403 (non-active) -> ErrForbidden,
// 5xx -> server error. The read endpoints must never be reached.
func TestLoginFailure(t *testing.T) {
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

			client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

			_, err := client.GetProject(t.Context(), "backend-service")

			if !errors.Is(err, tc.wantSent) {
				t.Fatalf("errors.Is(err, %v) = false, err = %v", tc.wantSent, err)
			}
			if !tc.wantHelper(err) {
				t.Errorf("helper(err) = false, err = %v", err)
			}
		})
	}
}

// TestRefreshOnceAfterFirstUse checks the 401 path after a first-use login:
// the second request 401s, the client refreshes exactly ONCE with the
// in-memory refresh token, swaps both tokens in memory and retries with the
// NEW access token; the first-use login happened before all of it.
func TestRefreshOnceAfterFirstUse(t *testing.T) {
	t.Parallel()

	const (
		wantLoginCalls   = 1
		wantRefreshCalls = 1
	)

	events := &eventLog{}
	var firstBearerReads atomic.Int64

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			events.record("login")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(alphaLoginFixture))
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
		case "/api/v1/projects/backend-service":
			switch r.Header.Get("Authorization") {
			case "Bearer ACCESS-1":
				if firstBearerReads.Add(1) == 1 {
					events.record("read-200")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id":"backend-service","name":"Backend Service",` +
						`"repo_url":"https://bitbucket.org/company/backend",` +
						`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-02T09:00:00Z"}`))

					return
				}
				events.record("read-401")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			case "Bearer ACCESS-2":
				events.record("read-200")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"backend-service","name":"Backend Service",` +
					`"repo_url":"https://bitbucket.org/company/backend",` +
					`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-02T09:00:00Z"}`))
			default:
				t.Errorf("Authorization = %q, want ACCESS-1 or ACCESS-2", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusInternalServerError)
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

	if _, err := client.GetProject(t.Context(), "backend-service"); err != nil {
		t.Fatalf("first GetProject() error = %v", err)
	}
	if _, err := client.GetProject(t.Context(), "backend-service"); err != nil {
		t.Fatalf("second GetProject() error = %v", err)
	}

	got := events.all()
	want := []string{"login", "read-200", "read-401", "refresh", "read-200"}
	if !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	if logins := countEvents(got, "login"); logins != wantLoginCalls {
		t.Errorf("login calls = %d, want %d", logins, wantLoginCalls)
	}
	if refreshes := countEvents(got, "refresh"); refreshes != wantRefreshCalls {
		t.Errorf("refresh calls = %d, want exactly 1", refreshes)
	}
}

// TestSecond401AfterRefreshIsUnauthorized checks the loop guard: when the
// retried request 401s AGAIN after a successful refresh, the client reports
// the typed ErrUnauthorized and never issues a third request or a second
// refresh.
func TestSecond401AfterRefreshIsUnauthorized(t *testing.T) {
	t.Parallel()

	const (
		wantRefreshCalls = 1
		wantReadCalls    = 2
	)

	events := &eventLog{}

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			events.record("login")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(alphaLoginFixture))
		case "/api/v1/auth/refresh":
			events.record("refresh")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-2","refresh_token":"ROTATED"}`))
		case "/api/v1/projects/backend-service":
			events.record("read-401")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

	_, err := client.GetProject(t.Context(), "backend-service")

	if !errors.Is(err, clientgo.ErrUnauthorized) {
		t.Fatalf("errors.Is(err, ErrUnauthorized) = false, err = %v", err)
	}
	if !clientgo.IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(err) = false, err = %v", err)
	}

	got := events.all()
	want := []string{"login", "read-401", "refresh", "read-401"}
	if !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v (no third call)", got, want)
	}
	if refreshes := countEvents(got, "refresh"); refreshes != wantRefreshCalls {
		t.Errorf("refresh calls = %d, want %d", refreshes, wantRefreshCalls)
	}
	if reads := countEvents(got, "read-401"); reads != wantReadCalls {
		t.Errorf("project reads = %d, want %d (original + one retry)", reads, wantReadCalls)
	}
}

// TestStaticWinsOverLogin checks precedence: with BOTH options set the
// client attaches the static bearer and NEVER calls the login or refresh
// endpoints; a 401 is reported as typed ErrUnauthorized.
func TestStaticWinsOverLogin(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			t.Error("login endpoint hit in static-token mode, want no login ever")
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/auth/refresh":
			t.Error("refresh endpoint hit in static-token mode, want no refresh ever")
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/projects/backend-service":
			if got := r.Header.Get("Authorization"); got != "Bearer static-token" {
				t.Errorf("Authorization = %q, want %q", got, "Bearer static-token")
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := mustNew(
		t,
		newStubServer(t, handler),
		clientgo.WithAccessToken("static-token"),
		clientgo.WithLogin("u@x.example", "pw-secret"),
	)

	_, err := client.GetProject(t.Context(), "backend-service")

	if !errors.Is(err, clientgo.ErrUnauthorized) {
		t.Fatalf("errors.Is(err, ErrUnauthorized) = false, err = %v", err)
	}
	if !clientgo.IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(err) = false, err = %v", err)
	}
}

// TestConcurrentFirstUseLogsInOnce fires N goroutines at a cold client: the
// login handler is held open until every goroutine has entered GetProject
// (no access token can exist yet), so they all race into the single-flight
// slot; exactly ONE login may happen and every request must succeed.
func TestConcurrentFirstUseLogsInOnce(t *testing.T) {
	t.Parallel()

	const concurrentRequests = 8

	var loginHits atomic.Int64
	loginStarted := make(chan struct{})
	loginRelease := make(chan struct{})

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			if loginHits.Add(1) == 1 {
				close(loginStarted)
			}
			<-loginRelease
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(bootLoginFixture))
		case "/api/v1/projects":
			if got := r.Header.Get("Authorization"); got != "Bearer access-token-value" {
				t.Errorf("Authorization = %q, want %q", got, "Bearer access-token-value")
				w.WriteHeader(http.StatusInternalServerError)

				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[],"total":0}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

	started := make(chan struct{}, concurrentRequests)
	errs := make([]error, concurrentRequests)
	var wg sync.WaitGroup
	for i := range concurrentRequests {
		wg.Go(func() {
			started <- struct{}{}
			_, _, errs[i] = client.ListProjects(t.Context())
		})
	}

	// The leader is now inside the login handler (no token issued yet) and
	// every goroutine has entered its request: they must all join the same
	// flight. Only then release the handler.
	<-loginStarted
	for range concurrentRequests {
		<-started
	}
	close(loginRelease)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: ListProjects() error = %v", i, err)
		}
	}
	if got := loginHits.Load(); got != 1 {
		t.Errorf("login calls = %d, want exactly 1 (single-flight first use)", got)
	}
}

// TestConcurrentRefreshSingleFlight fires N goroutines at a revoked access
// token simultaneously: the refresh handler is held until every goroutine
// has received its 401, so the race window is guaranteed, and the atomic
// counter proves exactly ONE refresh (and one first-use login) happened
// while all requests still succeeded on their single retry.
func TestConcurrentRefreshSingleFlight(t *testing.T) {
	t.Parallel()

	const concurrentRequests = 8

	var loginHits, refreshHits, unauthorized atomic.Int64
	release := make(chan struct{})

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(bootLoginFixture))
		case "/api/v1/auth/refresh":
			<-release
			refreshHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-B","refresh_token":"ROTATED"}`))
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
				_, _ = w.Write([]byte(`{"items":[],"total":0}`))
			default:
				t.Errorf("Authorization = %q, want access-token-value or ACCESS-B", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusInternalServerError)
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

	errs := make([]error, concurrentRequests)
	var wg sync.WaitGroup
	for i := range concurrentRequests {
		wg.Go(func() {
			_, errs[i] = client.GetProject(t.Context(), "backend-service")
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: GetProject() error = %v", i, err)
		}
	}
	if got := loginHits.Load(); got != 1 {
		t.Errorf("login calls = %d, want exactly 1", got)
	}
	if got := refreshHits.Load(); got != 1 {
		t.Errorf("refresh calls = %d, want exactly 1 (single-flight)", got)
	}
	if got := unauthorized.Load(); got != concurrentRequests {
		t.Errorf("401 responses = %d, want %d (every goroutine raced)", got, concurrentRequests)
	}
}

// TestNoCredentialsNeverLogin checks the no-credential path: without any
// option the request goes out with no bearer, the 401 is reported as typed
// ErrUnauthorized, and neither the login nor the refresh endpoint is ever
// called.
func TestNoCredentialsNeverLogin(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			t.Error("login endpoint hit without credentials, want none")
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/auth/refresh":
			t.Error("refresh endpoint hit without credentials, want none")
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/projects/backend-service":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("Authorization = %q, want empty", got)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := mustNew(t, newStubServer(t, handler))

	_, err := client.GetProject(t.Context(), "backend-service")

	if !errors.Is(err, clientgo.ErrUnauthorized) {
		t.Fatalf("errors.Is(err, ErrUnauthorized) = false, err = %v", err)
	}
	if !clientgo.IsUnauthorized(err) {
		t.Errorf("IsUnauthorized(err) = false, err = %v", err)
	}
}

// TestNewRejectsEmptyLogin checks New refuses WithLogin with an empty email
// or password (or both) with the typed ErrInvalidCredentials and a nil
// client, instead of failing at the first request.
func TestNewRejectsEmptyLogin(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		email    string
		password string
	}{
		{name: "empty email", email: "", password: "pw-secret"},
		{name: "empty password", email: "u@x.example", password: ""},
		{name: "both empty", email: "", password: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := clientgo.New("http://localhost:3000", clientgo.WithLogin(tc.email, tc.password))

			if client != nil {
				t.Errorf("client = %v, want nil", client)
			}
			if !errors.Is(err, clientgo.ErrInvalidCredentials) {
				t.Errorf("errors.Is(err, ErrInvalidCredentials) = false, err = %v", err)
			}
		})
	}
}

// TestErrorStringsLeakNoSecrets scans every error the suite can provoke
// against the password and both token values: no returned error string may
// contain any of them.
func TestErrorStringsLeakNoSecrets(t *testing.T) {
	t.Parallel()

	forbidden := []string{"pw-secret", "ACCESS-1", "ACCESS-2", "REFRESH-0"}

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

	t.Run("login 401", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"invalid credentials"}`))
		}
		client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

		_, err := client.GetProject(t.Context(), "backend-service")

		assertNoLeak(t, err)
	})

	t.Run("login transport failure", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.NotFoundHandler())
		baseURL := srv.URL
		srv.Close()

		client := mustNew(t, baseURL, clientgo.WithLogin("u@x.example", "pw-secret"),
			clientgo.WithHTTPClient(&http.Client{Transport: &http.Transport{Proxy: nil}}))

		_, err := client.GetProject(t.Context(), "backend-service")

		assertNoLeak(t, err)
	})

	t.Run("refresh 401", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/auth/login":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(alphaLoginFixture))
			case "/api/v1/auth/refresh":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"refresh token rejected"}`))
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			}
		}
		client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

		_, err := client.GetProject(t.Context(), "backend-service")

		assertNoLeak(t, err)
	})

	t.Run("second 401 after refresh", func(t *testing.T) {
		t.Parallel()

		handler := func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/auth/login":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(alphaLoginFixture))
			case "/api/v1/auth/refresh":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"ACCESS-2","refresh_token":"ROTATED"}`))
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"missing or expired token"}`))
			}
		}
		client := mustNew(t, newStubServer(t, handler), clientgo.WithLogin("u@x.example", "pw-secret"))

		_, err := client.GetProject(t.Context(), "backend-service")

		assertNoLeak(t, err)
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
