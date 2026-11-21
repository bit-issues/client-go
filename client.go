// Package clientgo provides shared wire DTOs and an HTTP client for the
// BitIssues REST API.
package clientgo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	restkit "github.com/capcom6/go-restkit"
)

// Client is a STATELESS BitIssues REST API client: configuration is
// immutable after construction, every read takes its credential explicitly
// and the credential-exchange operations (Login, Refresh, Logout) are plain
// in/out calls. A Client holds no token state - the stateful [Session]
// wrapper does. HTTP transport and failure classification come from
// github.com/capcom6/go-restkit.
type Client struct {
	rest *restkit.Client
}

// New returns a client for baseURL; the endpoint paths are appended to it
// verbatim. baseURL must contain the full path prefix (for example
// https://host.example/api/v1), since the server may sit behind a URL rewrite.
// An empty or whitespace baseURL is rejected with ErrInvalidBaseURL and New
// never panics.
func New(baseURL string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, ErrInvalidBaseURL
	}

	cfg := clientConfig{httpClient: &http.Client{}}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{}
	}

	rest, err := restkit.NewClient(restkit.Config{
		BaseURL: baseURL,
		Client:  cfg.httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("create rest client: %w", err)
	}

	return &Client{rest: rest}, nil
}

// Do sends a request for path (joined onto the configured base URL) with the
// optional query parameters and the bearer from auth. A JSON payload is sent
// when payload is not nil and is decoded into response when response is not
// nil and the response has a body; a 204 or empty body is a success without
// any decode attempt. HTTP failures are *restkit.APIError, network failures
// are *restkit.InfrastructureError and request/decode failures are
// *restkit.InternalError. The stateless client never refreshes - a 401 is
// returned as-is; see [Session] for refresh-once semantics.
func (c *Client) Do(
	ctx context.Context,
	auth Auth,
	method, path string,
	query url.Values,
	payload, response any,
) error {
	requestPath := path
	if len(query) > 0 {
		requestPath += "?" + query.Encode()
	}

	headers := http.Header{}
	if auth.AccessToken != "" {
		headers.Set(headerAuthorization, bearerScheme+auth.AccessToken)
	}

	if err := c.rest.Do(ctx, method, requestPath, headers, payload, response); err != nil {
		return fmt.Errorf("api request failed: %w", err)
	}

	return nil
}

// Login exchanges email and password for a token pair plus the user profile.
// Empty or whitespace arguments are rejected with ErrInvalidCredentials
// before any request; the password travels only in the request body and
// never enters an error string. HTTP failures keep their restkit failure
// classification with the status available through restkit.AsAPIError.
func (c *Client) Login(ctx context.Context, email, password string) (LoginResult, error) {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(password) == "" {
		return LoginResult{}, ErrInvalidCredentials
	}

	var resp LoginResponse
	if err := c.Do(
		ctx,
		Auth{AccessToken: ""},
		http.MethodPost,
		pathAuthLogin,
		nil,
		LoginRequest{Email: email, Password: password},
		&resp,
	); err != nil {
		return LoginResult{}, err
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		return LoginResult{}, fmt.Errorf("%w: login response is missing a token", ErrInvalidResponse)
	}

	return LoginResult{
		Tokens: Tokens{AccessToken: resp.AccessToken, RefreshToken: resp.RefreshToken},
		User:   resp.User,
	}, nil
}

// Refresh exchanges refreshToken for a rotated token pair (explicit in/out;
// nothing is stored). A refresh token the backend rejects (401: revoked or
// expired) is classified as ErrSessionExpired; every other failure keeps its
// restkit classification. No token value enters the error.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	var pair RefreshResponse
	err := c.Do(
		ctx,
		Auth{AccessToken: ""},
		http.MethodPost,
		pathAuthRefresh,
		nil,
		RefreshRequest{RefreshToken: refreshToken},
		&pair,
	)
	if err != nil {
		if apiErr, ok := restkit.AsAPIError(err); ok && apiErr.StatusCode == http.StatusUnauthorized {
			return Tokens{}, ErrSessionExpired
		}

		return Tokens{}, err
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		return Tokens{}, fmt.Errorf("%w: refresh response is missing a token", ErrInvalidResponse)
	}

	return Tokens(pair), nil
}

// Logout revokes refreshToken via the public logout endpoint. The API
// answers 204 with an EMPTY body, which is never decoded. An empty
// refreshToken is a no-op that returns nil without any request - there is
// nothing to revoke.
func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}

	return c.Do(
		ctx,
		Auth{AccessToken: ""},
		http.MethodPost,
		pathAuthLogout,
		nil,
		LogoutRequest{RefreshToken: refreshToken},
		nil,
	)
}

// ListProjects returns all projects and the total count reported by the API.
func (c *Client) ListProjects(ctx context.Context, auth Auth) ([]Project, int, error) {
	return listProjects(ctx, c.doWith(auth))
}

// GetProject returns the project identified by its slug (the project ID on the wire).
func (c *Client) GetProject(ctx context.Context, auth Auth, slug string) (*Project, error) {
	return getProject(ctx, c.doWith(auth), slug)
}

// ListTasks returns a page of tasks matching params.
func (c *Client) ListTasks(ctx context.Context, auth Auth, params ListTasksParams) (*TaskListResponse, error) {
	return listTasks(ctx, c.doWith(auth), params)
}

// GetTaskByID returns the base task fields of the task with the given ID.
// The endpoint returns the full details payload; the project, comments and
// attachments keys are ignored here - use GetTaskDetails to read them.
func (c *Client) GetTaskByID(ctx context.Context, auth Auth, id int64) (*TaskResponse, error) {
	return getTaskByID(ctx, c.doWith(auth), id)
}

// GetTaskByNumber returns the base task fields of the task identified by its
// project slug and its number within that project.
func (c *Client) GetTaskByNumber(ctx context.Context, auth Auth, slug string, number int) (*TaskResponse, error) {
	return getTaskByNumber(ctx, c.doWith(auth), slug, number)
}

// GetTaskDetails returns the full details payload of the task with the given
// ID, including the project, comments and attachments.
func (c *Client) GetTaskDetails(ctx context.Context, auth Auth, id int64) (*TaskDetailsResponse, error) {
	return getTaskDetails(ctx, c.doWith(auth), id)
}

// doWith binds auth to Do, adapting it to the shared readFunc shape.
func (c *Client) doWith(auth Auth) readFunc {
	return func(ctx context.Context, method, path string, query url.Values, payload, response any) error {
		return c.Do(ctx, auth, method, path, query, payload, response)
	}
}

// readFunc issues one read request that already carries its credential;
// [Client.doWith] adapts a bound Auth to this shape while a [Session] passes
// its own Do, which injects the in-memory token - so the read bodies below
// stay single-sourced for both surfaces.
type readFunc func(ctx context.Context, method, path string, query url.Values, payload, response any) error

// listProjects performs the shared ListProjects read on do.
func listProjects(ctx context.Context, do readFunc) ([]Project, int, error) {
	var body struct {
		Items []Project `json:"items"`
		Total int       `json:"total"`
	}

	if err := do(ctx, http.MethodGet, pathProjects, nil, nil, &body); err != nil {
		return nil, 0, err
	}

	return body.Items, body.Total, nil
}

// getProject performs the shared GetProject read on do.
func getProject(ctx context.Context, do readFunc, slug string) (*Project, error) {
	var project Project

	path := pathProjects + "/" + url.PathEscape(slug)
	if err := do(ctx, http.MethodGet, path, nil, nil, &project); err != nil {
		return nil, err
	}

	return &project, nil
}

// listTasks performs the shared ListTasks read on do; empty params render as
// no query string at all, so the server-side defaults apply.
func listTasks(ctx context.Context, do readFunc, params ListTasksParams) (*TaskListResponse, error) {
	var list TaskListResponse

	if err := do(ctx, http.MethodGet, pathTasks, taskListQuery(params), nil, &list); err != nil {
		return nil, err
	}

	return &list, nil
}

// getTaskByID performs the shared GetTaskByID read on do.
func getTaskByID(ctx context.Context, do readFunc, id int64) (*TaskResponse, error) {
	var task TaskResponse

	if err := do(ctx, http.MethodGet, taskPath(id), nil, nil, &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// getTaskByNumber performs the shared GetTaskByNumber read on do.
func getTaskByNumber(ctx context.Context, do readFunc, slug string, number int) (*TaskResponse, error) {
	var task TaskResponse

	path := pathTasks + "/" + url.PathEscape(slug) + "/" + strconv.Itoa(number)
	if err := do(ctx, http.MethodGet, path, nil, nil, &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// getTaskDetails performs the shared GetTaskDetails read on do.
func getTaskDetails(ctx context.Context, do readFunc, id int64) (*TaskDetailsResponse, error) {
	var details TaskDetailsResponse

	if err := do(ctx, http.MethodGet, taskPath(id), nil, nil, &details); err != nil {
		return nil, err
	}

	return &details, nil
}

// isAuthPath reports whether path is a public auth endpoint; the [Session]
// never runs its refresh/retry flow against those.
func isAuthPath(path string) bool {
	switch path {
	case pathAuthLogin, pathAuthRefresh, pathAuthLogout:
		return true
	default:
		return false
	}
}
