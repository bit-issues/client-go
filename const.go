package clientgo

import "time"

// REST endpoint paths, always appended to the configured base URL.
const (
	pathPrefix      = "/api/v1"
	pathProjects    = pathPrefix + "/projects"
	pathTasks       = pathPrefix + "/tasks"
	pathAuthLogin   = pathPrefix + "/auth/login"
	pathAuthRefresh = pathPrefix + "/auth/refresh"
	pathAuthLogout  = pathPrefix + "/auth/logout"
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
