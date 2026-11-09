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
)

// Client is a BitIssues REST API client. Its configuration is immutable after
// construction, so a Client may be shared by concurrent callers.
type Client struct {
	config Config
}

// NewClient returns a client for the API described by config; when the config
// carries no HTTP client, the default one is used.
func NewClient(config Config) *Client {
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}

	return &Client{config: config}
}

// Do sends a request for path (joined onto the configured base URL) with the
// optional query parameters. A JSON payload is sent when payload is not nil
// and is decoded into response when response is not nil and the response has
// a body; a 204 or empty body is a success without any decode attempt. Any
// non-2xx response becomes a typed *APIError; network failures wrap
// ErrTransport.
func (c *Client) Do(
	ctx context.Context,
	method, path string,
	query url.Values,
	payload, response any,
) error {
	req, err := newRequest(ctx, method, requestURL(c.config.BaseURL, path, query), payload, c.config.accessToken)
	if err != nil {
		return err
	}

	resp, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTransport, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: failed to read response body: %w", ErrTransport, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return parseAPIError(resp.StatusCode, body)
	}

	if resp.StatusCode == http.StatusNoContent || response == nil {
		return nil
	}

	return decodeJSONBody(body, response)
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

// Login exchanges credentials for an access and refresh token pair plus the user.
func (c *Client) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	var resp LoginResponse

	if err := c.Do(ctx, http.MethodPost, pathAuthLogin, nil, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Refresh exchanges a refresh token for a rotated access and refresh token pair.
func (c *Client) Refresh(ctx context.Context, req RefreshRequest) (*RefreshResponse, error) {
	var resp RefreshResponse

	if err := c.Do(ctx, http.MethodPost, pathAuthRefresh, nil, req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Logout revokes the refresh token; the API answers 204 with no body.
func (c *Client) Logout(ctx context.Context, req LogoutRequest) error {
	return c.Do(ctx, http.MethodPost, pathAuthLogout, nil, req, nil)
}
