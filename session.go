package clientgo

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// executor performs one HTTP exchange; the client's roundTrip implements it,
// so request construction and execution stay in client.go.
type executor func(ctx context.Context, method, urlStr string, payload any, accessToken string) (int, []byte, error)

// sessionFlight is the shared result of one single-flight login or refresh;
// its fields are written by the leader before done is closed, so waiters
// read them safely after receiving from done.
type sessionFlight struct {
	done chan struct{}
	err  error
}

// session owns every credential decision of a Client: the login credentials,
// BOTH tokens (struct fields, memory only), the mutex/single-flight state
// and the login/refresh flows. client.go delegates to bearer and
// refreshAfterUnauthorized and contains no auth parsing at all; nothing
// about credentials ever touches the filesystem inside client-go.
type session struct {
	baseURL     string
	staticToken string
	email       string
	password    string

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	generation   uint64
	inflight     *sessionFlight
}

// newSession builds the credential state for a client from its config; all
// mutable fields start zeroed and the mutex is its zero value.
func newSession(baseURL string, cfg clientConfig) session {
	return session{
		baseURL:      baseURL,
		staticToken:  cfg.accessToken,
		email:        cfg.email,
		password:     cfg.password,
		mu:           sync.Mutex{},
		accessToken:  "",
		refreshToken: "",
		generation:   0,
		inflight:     nil,
	}
}

// bearer returns the generation of the access token and the bearer
// credential to send, performing the internal login first when WithLogin is
// configured and no access token exists yet. Concurrent first uses share
// exactly ONE login through the single-flight slot; a static token always
// wins and never logs in.
func (s *session) bearer(ctx context.Context, exec executor) (uint64, string, error) {
	if s.staticToken != "" {
		return 0, s.staticToken, nil
	}

	s.mu.Lock()
	gen, token := s.generation, s.accessToken
	s.mu.Unlock()

	if token != "" {
		return gen, token, nil
	}
	// New guarantees both fields are non-empty when WithLogin was used, so
	// an empty email means no login mode at all.
	if s.email == "" {
		return gen, "", nil
	}

	if err := s.loginOnce(ctx, exec); err != nil {
		return 0, "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.generation, s.accessToken, nil
}

// loginOnce performs at most one login while no access token exists: the
// first caller becomes the leader and performs it, concurrent callers wait
// on the same flight and share its result (stdlib [sync.Mutex] only).
func (s *session) loginOnce(ctx context.Context, exec executor) error {
	s.mu.Lock()

	if s.accessToken != "" {
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

	flight.err = s.performLogin(ctx, exec)

	s.mu.Lock()
	s.inflight = nil
	close(flight.done)
	s.mu.Unlock()

	return flight.err
}

// performLogin POSTs /api/v1/auth/login with the configured credentials
// (byte-exact JSON body) and keeps BOTH tokens in memory. The failure
// taxonomy is the shared API error parsing: 401 -> ErrUnauthorized,
// 403 -> ErrForbidden, 5xx -> server errors, transport -> ErrTransport; no
// password and no token value can enter the returned error.
func (s *session) performLogin(ctx context.Context, exec executor) error {
	status, body, err := exec(
		ctx,
		http.MethodPost,
		requestURL(s.baseURL, pathAuthLogin, nil),
		LoginRequest{Email: s.email, Password: s.password},
		"",
	)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return parseAPIError(status, body)
	}

	var resp LoginResponse
	if decodeErr := decodeJSONBody(body, &resp); decodeErr != nil {
		return decodeErr
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		return fmt.Errorf("%w: login response is missing a token", ErrUnauthorized)
	}

	s.setTokens(resp.AccessToken, resp.RefreshToken)

	return nil
}

// refreshAfterUnauthorized refreshes after a 401 (single-flight) and reports
// whether the caller may retry its original request exactly once. Static-token
// mode and a client without a login-issued refresh token both decline, so the
// caller surfaces its original 401 as ErrUnauthorized instead; a failed
// refresh propagates its typed error and blocks the retry.
func (s *session) refreshAfterUnauthorized(
	ctx context.Context,
	gen uint64,
	exec executor,
) (bool, error) {
	if s.staticToken != "" || s.currentRefreshToken() == "" {
		return false, nil
	}

	if err := s.refreshOnce(ctx, gen, exec); err != nil {
		return false, err
	}

	return true, nil
}

// refreshOnce runs at most one refresh per token generation: the first
// caller becomes the leader and performs it, concurrent callers wait on the
// same flight and share its result. An already-advanced generation means
// another refresh landed after the caller's request started, so its result
// is reused without a new call.
func (s *session) refreshOnce(ctx context.Context, gen uint64, exec executor) error {
	s.mu.Lock()

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

	flight.err = s.performRefresh(ctx, exec)

	s.mu.Lock()
	s.inflight = nil
	close(flight.done)
	s.mu.Unlock()

	return flight.err
}

// performRefresh exchanges the in-memory refresh token for a new pair via
// POST /api/v1/auth/refresh and swaps BOTH tokens in memory before the
// caller retries (the in-memory swap is trivially ordered before the retry).
// The backend ROTATES - it revokes the OLD refresh token BEFORE responding -
// and the failure taxonomy matches performLogin through parseAPIError.
func (s *session) performRefresh(ctx context.Context, exec executor) error {
	status, body, err := exec(
		ctx,
		http.MethodPost,
		requestURL(s.baseURL, pathAuthRefresh, nil),
		RefreshRequest{RefreshToken: s.currentRefreshToken()},
		"",
	)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return parseAPIError(status, body)
	}

	var pair RefreshResponse
	if decodeErr := decodeJSONBody(body, &pair); decodeErr != nil {
		return decodeErr
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		return fmt.Errorf("%w: refresh response is missing a token", ErrUnauthorized)
	}

	s.setTokens(pair.AccessToken, pair.RefreshToken)

	return nil
}

// setTokens replaces BOTH in-memory tokens and bumps the generation so
// requests already in flight can detect the change.
func (s *session) setTokens(accessToken, refreshToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.accessToken = accessToken
	s.refreshToken = refreshToken
	s.generation++
}

// currentRefreshToken returns the in-memory refresh token.
func (s *session) currentRefreshToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.refreshToken
}

// waitFlight waits for an in-flight login or refresh and shares its result;
// the waiting is cancelled by the caller's context.
func waitFlight(ctx context.Context, flight *sessionFlight) error {
	select {
	case <-flight.done:
		return flight.err
	case <-ctx.Done():
		return fmt.Errorf("wait for auth flow: %w", ctx.Err())
	}
}
