package clientgo

import "time"

// REST endpoint paths, always appended to the configured base URL. The base
// URL carries the full path prefix, so the client adds none of its own.
const (
	pathProjects    = "/projects"
	pathTasks       = "/tasks"
	pathAuthLogin   = "/auth/login"
	pathAuthRefresh = "/auth/refresh"
	pathAuthLogout  = "/auth/logout"
)

// Headers and credential formatting used on requests.
const (
	headerContentType   = "Content-Type"
	headerAuthorization = "Authorization"
	contentTypeJSON     = "application/json"
	bearerScheme        = "Bearer "
)

// authFlightTimeout bounds the detached single-flight login/refresh: the
// flight ignores caller cancellation, so it carries its own deadline.
const authFlightTimeout = 30 * time.Second
