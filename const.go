package clientgo

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
