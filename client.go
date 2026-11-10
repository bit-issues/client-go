// Package clientgo provides shared wire DTOs and an HTTP client for the
// BitIssues REST API.
package clientgo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client is a BitIssues REST API client. Configuration is immutable after
// construction; every credential decision (static bearer, per-boot login,
// refresh, single-flight) is delegated to the embedded session, whose state
// lives in memory only, so a Client may be shared by concurrent callers.
type Client struct {
	baseURL    string
	httpClient *http.Client
	session    session
}

// New returns a client for baseURL; the /api/v1/... paths are always appended
// to it. Options configure the HTTP client, a static access token or the
// per-boot login credentials; an empty or whitespace baseURL is rejected
// with ErrInvalidBaseURL and an empty WithLogin credential with
// ErrInvalidCredentials - New never panics.
func New(baseURL string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, ErrInvalidBaseURL
	}

	cfg := clientConfig{
		httpClient:  &http.Client{},
		accessToken: "",
		email:       "",
		password:    "",
		loginSet:    false,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{}
	}
	if cfg.loginSet && (strings.TrimSpace(cfg.email) == "" || strings.TrimSpace(cfg.password) == "") {
		return nil, ErrInvalidCredentials
	}

	return &Client{
		baseURL:    baseURL,
		httpClient: cfg.httpClient,
		session:    newSession(baseURL, cfg),
	}, nil
}

// Do sends a request for path (joined onto the configured base URL) with the
// optional query parameters. A JSON payload is sent when payload is not nil
// and is decoded into response when response is not nil and the response has
// a body; a 204 or empty body is a success without any decode attempt. Any
// non-2xx response becomes a typed *APIError; network failures wrap
// ErrTransport. The bearer credential comes from the session (logging in
// first when needed); on a 401 the session refreshes at most once
// (single-flight) and Do retries the original request exactly once, a
// second 401 is returned as ErrUnauthorized.
func (c *Client) Do(
	ctx context.Context,
	method, path string,
	query url.Values,
	payload, response any,
) error {
	return c.do(ctx, method, path, query, payload, response, false)
}

// do is the retry-aware request core; it delegates bearer and refresh
// decisions to the session and marks the single allowed follow-up after a
// refresh with retried so a second 401 can never loop.
func (c *Client) do(
	ctx context.Context,
	method, path string,
	query url.Values,
	payload, response any,
	retried bool,
) error {
	gen, token, bearerErr := c.session.bearer(ctx, c.roundTrip)
	if bearerErr != nil {
		return bearerErr
	}

	status, body, err := c.roundTrip(ctx, method, requestURL(c.baseURL, path, query), payload, token)
	if err != nil {
		return err
	}

	if status == http.StatusUnauthorized && !isAuthPath(path) && !retried {
		canRetry, refreshErr := c.session.refreshAfterUnauthorized(ctx, gen, c.roundTrip)
		if refreshErr != nil {
			return refreshErr
		}
		if canRetry {
			return c.do(ctx, method, path, query, payload, response, true)
		}
	}

	return finishResponse(status, body, response)
}

// roundTrip performs one HTTP exchange and returns the status code with the
// fully read body; the response body is always closed.
func (c *Client) roundTrip(
	ctx context.Context,
	method, urlStr string,
	payload any,
	accessToken string,
) (int, []byte, error) {
	req, err := newRequest(ctx, method, urlStr, payload, accessToken)
	if err != nil {
		return 0, nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %w", ErrTransport, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read response body: %w", ErrTransport, err)
	}

	return resp.StatusCode, body, nil
}

// finishResponse classifies a non-2xx response or decodes a success body; a
// 204 and a nil response target succeed without any decode attempt.
func finishResponse(status int, body []byte, response any) error {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return parseAPIError(status, body)
	}

	if status == http.StatusNoContent || response == nil {
		return nil
	}

	return decodeJSONBody(body, response)
}

// isAuthPath reports whether path is a public auth endpoint; those never
// take part in the automatic refresh/retry flow.
func isAuthPath(path string) bool {
	switch path {
	case pathAuthLogin, pathAuthRefresh, pathAuthLogout:
		return true
	default:
		return false
	}
}

// ListProjects returns all projects and the total count reported by the API.
func (c *Client) ListProjects(ctx context.Context) ([]Project, int, error) {
	var body struct {
		Items []Project `json:"items"`
		Total int       `json:"total"`
	}

	if err := c.Do(ctx, http.MethodGet, pathProjects, nil, nil, &body); err != nil {
		return nil, 0, err
	}

	return body.Items, body.Total, nil
}

// GetProject returns the project identified by its slug (the project ID on the wire).
func (c *Client) GetProject(ctx context.Context, slug string) (*Project, error) {
	var project Project

	path := pathProjects + "/" + url.PathEscape(slug)
	if err := c.Do(ctx, http.MethodGet, path, nil, nil, &project); err != nil {
		return nil, err
	}

	return &project, nil
}

// ListTasks returns a page of tasks matching params. Empty params are sent as
// no query string at all, so the server-side defaults apply.
func (c *Client) ListTasks(ctx context.Context, params ListTasksParams) (*TaskListResponse, error) {
	var list TaskListResponse

	if err := c.Do(ctx, http.MethodGet, pathTasks, taskListQuery(params), nil, &list); err != nil {
		return nil, err
	}

	return &list, nil
}

// GetTaskByID returns the base task fields of the task with the given ID. The
// endpoint returns the full details payload; the project, comments and
// attachments keys are ignored here - use GetTaskDetails to read them.
func (c *Client) GetTaskByID(ctx context.Context, id int64) (*TaskResponse, error) {
	var task TaskResponse

	if err := c.Do(ctx, http.MethodGet, taskPath(id), nil, nil, &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// GetTaskByNumber returns the base task fields of the task identified by its
// project slug and its number within that project.
func (c *Client) GetTaskByNumber(ctx context.Context, slug string, number int) (*TaskResponse, error) {
	var task TaskResponse

	path := pathTasks + "/" + url.PathEscape(slug) + "/" + strconv.Itoa(number)
	if err := c.Do(ctx, http.MethodGet, path, nil, nil, &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// GetTaskDetails returns the full details payload of the task with the given
// ID, including the project, comments and attachments.
func (c *Client) GetTaskDetails(ctx context.Context, id int64) (*TaskDetailsResponse, error) {
	var details TaskDetailsResponse

	if err := c.Do(ctx, http.MethodGet, taskPath(id), nil, nil, &details); err != nil {
		return nil, err
	}

	return &details, nil
}
