package clientgo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
)

// Session is the STATEFUL wrapper around a [Client]: it keeps BOTH tokens
// in memory (mutex-guarded), injects the access token into every read and
// refreshes ONCE on a 401 before retrying the original request exactly once.
// It performs no credential exchange of its own - the caller seeds tokens
// through [NewSession] and persists rotated ones via [Session.Tokens]; storage
// always lives outside client-go.
type Session struct {
	client       *Client
	mu           sync.Mutex
	accessToken  string
	refreshToken string
	generation   uint64
	inflight     *sessionFlight
	loggedOut    bool
	logoutGate   chan struct{}
}

// NewSession returns a Session for baseURL (validated exactly like New) that
// starts from tokens - either pair may be empty: no access token means the
// first read runs without a bearer, and no refresh token makes the session
// static-style (401 is surfaced as ErrUnauthorized, refresh never attempted).
// A refresh token the backend rejects mid-flight becomes ErrSessionExpired.
func NewSession(baseURL string, tokens Tokens, opts ...Option) (*Session, error) {
	client, err := New(baseURL, opts...)
	if err != nil {
		return nil, err
	}

	return &Session{
		client:       client,
		mu:           sync.Mutex{},
		accessToken:  tokens.AccessToken,
		refreshToken: tokens.RefreshToken,
		generation:   0,
		inflight:     nil,
		loggedOut:    false,
		logoutGate:   make(chan struct{}, 1),
	}, nil
}

// Do sends a request like [Client.Do] but injects the Session's current
// access token. On a 401 the Session refreshes at most once (single-flight)
// and retries the original request exactly once; a second 401 is returned as
// ErrUnauthorized, and a rejected refresh token surfaces as ErrSessionExpired
// without any retry.
func (s *Session) Do(
	ctx context.Context,
	method, path string,
	query url.Values,
	payload, response any,
) error {
	return s.do(ctx, method, path, query, payload, response, false)
}

// Tokens returns a mutex-protected snapshot of the CURRENT in-memory token
// pair (post-rotation) - the caller's bridge for saving state on exit.
func (s *Session) Tokens() Tokens {
	s.mu.Lock()
	defer s.mu.Unlock()

	return Tokens{AccessToken: s.accessToken, RefreshToken: s.refreshToken}
}

// Logout revokes the in-memory refresh token via the stateless client (the
// API answers 204 with an EMPTY body, which is never decoded) and then
// clears BOTH tokens in memory. It is coordinated with the single-flight
// refresh: any in-flight rotation is awaited first (so the FINAL refresh
// token - rotated or original - is the one revoked), and while the logout
// runs no new refresh may fly or store tokens, so a completing rotation can
// never restore credentials after the clear. With no refresh token in
// memory the HTTP call is skipped (stateless no-op) and the memory clear
// still runs. A failed revoke (or an aborted wait) keeps the tokens so the
// caller may retry. Concurrent logouts are serialized by a context-aware gate,
// so the guard has a single owner; a caller cancelled while waiting for that
// ownership returns immediately without changing any state.
func (s *Session) Logout(ctx context.Context) (err error) {
	select {
	case s.logoutGate <- struct{}{}:
		defer func() { <-s.logoutGate }()
	case <-ctx.Done():
		return fmt.Errorf("logout ownership: %w", ctx.Err())
	}

	s.mu.Lock()
	s.loggedOut = true
	flight := s.inflight
	s.mu.Unlock()

	defer func() {
		if err != nil {
			s.mu.Lock()
			s.loggedOut = false
			s.mu.Unlock()
		}
	}()

	if flight != nil {
		// The flight's own result does not change what gets revoked - the
		// in-memory token does - so only an aborted wait returns here.
		select {
		case <-flight.done:
		case <-ctx.Done():
			return fmt.Errorf("wait for auth flow: %w", ctx.Err())
		}
	}

	if err = s.client.Logout(ctx, s.currentRefreshToken()); err != nil {
		return err
	}

	s.setTokens("", "")

	return nil
}

// ListProjects returns all projects and the total count reported by the API.
func (s *Session) ListProjects(ctx context.Context) ([]Project, int, error) {
	return listProjects(ctx, s.Do)
}

// GetProject returns the project identified by its slug (the project ID on the wire).
func (s *Session) GetProject(ctx context.Context, slug string) (*Project, error) {
	return getProject(ctx, s.Do, slug)
}

// ListTasks returns a page of tasks matching params.
func (s *Session) ListTasks(ctx context.Context, params ListTasksParams) (*TaskListResponse, error) {
	return listTasks(ctx, s.Do, params)
}

// GetTaskByID returns the base task fields of the task with the given ID.
// The endpoint returns the full details payload; the project, comments and
// attachments keys are ignored here - use GetTaskDetails to read them.
func (s *Session) GetTaskByID(ctx context.Context, id int64) (*TaskResponse, error) {
	return getTaskByID(ctx, s.Do, id)
}

// GetTaskByNumber returns the base task fields of the task identified by its
// project slug and its number within that project.
func (s *Session) GetTaskByNumber(ctx context.Context, slug string, number int) (*TaskResponse, error) {
	return getTaskByNumber(ctx, s.Do, slug, number)
}

// GetTaskDetails returns the full details payload of the task with the given
// ID, including the project, comments and attachments.
func (s *Session) GetTaskDetails(ctx context.Context, id int64) (*TaskDetailsResponse, error) {
	return getTaskDetails(ctx, s.Do, id)
}

// do is the retry-aware request core of the Session; retried marks the
// single allowed follow-up after a refresh so a second 401 can never loop.
func (s *Session) do(
	ctx context.Context,
	method, path string,
	query url.Values,
	payload, response any,
	retried bool,
) error {
	endpoint := requestPath(path)

	gen, token := s.credentialState()

	status, body, err := s.client.roundTrip(ctx, method, requestURL(s.client.baseURL, endpoint, query), payload, token)
	if err != nil {
		return err
	}

	if status == http.StatusUnauthorized && !retried && !isAuthPath(endpoint) {
		canRetry, refreshErr := s.refreshAfterUnauthorized(ctx, gen)
		if refreshErr != nil {
			return refreshErr
		}
		if canRetry {
			return s.do(ctx, method, path, query, payload, response, true)
		}
	}

	return finishResponse(status, body, response)
}

// credentialState returns the generation of the in-memory access token and
// the bearer to send.
func (s *Session) credentialState() (uint64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.generation, s.accessToken
}

// refreshAfterUnauthorized refreshes after a 401 (single-flight) and reports
// whether the caller may retry its original request exactly once. Without an
// in-memory refresh token (static-style session) it declines, so the caller
// surfaces its original 401 as ErrUnauthorized instead; a failed refresh -
// ErrSessionExpired, a 5xx or a transport failure - propagates its typed
// error and blocks the retry.
func (s *Session) refreshAfterUnauthorized(ctx context.Context, gen uint64) (bool, error) {
	if s.currentRefreshToken() == "" {
		return false, nil
	}

	if err := s.refreshOnce(ctx, gen); err != nil {
		return false, err
	}

	return true, nil
}

// refreshOnce runs at most one refresh per token generation: the first
// caller becomes the leader and starts the flight, concurrent callers wait
// on the same flight and share its result (stdlib [sync.Mutex] only). An
// already-advanced generation means another rotation landed after the
// caller's request started, so its result is reused without a new call. A
// logout in progress (or completed) declines new flights so no rotation can
// race the revoke or restore credentials after the clear. The flight runs on
// a detached, bounded context (see [flightContext]) and every caller waits
// through [waitFlight]: a cancelled leader returns immediately without
// failing the waiters, and the rotation still completes and stores its
// tokens - the Session is never left holding a revoked refresh token.
func (s *Session) refreshOnce(ctx context.Context, gen uint64) error {
	s.mu.Lock()

	if s.loggedOut {
		s.mu.Unlock()

		return fmt.Errorf("%w: session is being revoked", ErrSessionExpired)
	}

	if s.generation != gen {
		s.mu.Unlock()
		return nil
	}
	if s.inflight != nil {
		flight := s.inflight
		s.mu.Unlock()

		return waitFlight(ctx, flight)
	}

	flight := &sessionFlight{done: make(chan struct{}), err: nil}
	s.inflight = flight
	s.mu.Unlock()

	flightCtx, cancel := flightContext(ctx)
	go func() {
		defer cancel()

		flight.err = s.performRefresh(flightCtx)

		s.mu.Lock()
		s.inflight = nil
		close(flight.done)
		s.mu.Unlock()
	}()

	return waitFlight(ctx, flight)
}

// performRefresh exchanges the in-memory refresh token through the stateless
// [Client.Refresh] - the refresh HTTP call itself lives in client.go - and
// swaps BOTH tokens in memory before the caller retries (the in-memory swap
// is trivially ordered before the retry). The backend ROTATES: it revokes the
// OLD refresh token BEFORE responding. ErrSessionExpired from a rejected
// token and every transient failure pass through unmodified.
func (s *Session) performRefresh(ctx context.Context) error {
	tokens, err := s.client.Refresh(ctx, s.currentRefreshToken())
	if err != nil {
		return err
	}

	s.setTokens(tokens.AccessToken, tokens.RefreshToken)

	return nil
}

// setTokens replaces BOTH in-memory tokens and bumps the generation so
// requests already in flight can detect the change.
func (s *Session) setTokens(accessToken, refreshToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.accessToken = accessToken
	s.refreshToken = refreshToken
	s.generation++
}

// currentRefreshToken returns the in-memory refresh token.
func (s *Session) currentRefreshToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.refreshToken
}

// sessionFlight is the shared result of one single-flight refresh; its
// fields are written by the leader before done is closed, so waiters read
// them safely after receiving from done.
type sessionFlight struct {
	done chan struct{}
	err  error
}

// flightContext derives the bounded context a shared refresh runs on: values
// are inherited from the leader's context but its cancellation is detached,
// so a leader that gives up cannot fail concurrent waiters or abort the
// flight before rotated tokens are stored. authFlightTimeout bounds the
// detached call so a hung flight cannot block auth forever.
func flightContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), authFlightTimeout)
}

// waitFlight waits for an in-flight refresh and shares its result; the
// waiting is cancelled by the caller's own context.
func waitFlight(ctx context.Context, flight *sessionFlight) error {
	select {
	case <-flight.done:
		return flight.err
	case <-ctx.Done():
		return fmt.Errorf("wait for auth flow: %w", ctx.Err())
	}
}
