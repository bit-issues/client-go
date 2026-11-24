package clientgo

import (
	"net/url"
	"strconv"
	"strings"
)

// ListTasksParams holds the optional REST query parameters of
// GET /tasks (appended to the configured base URL). Zero values are omitted
// from the query string so the server-side defaults apply.
type ListTasksParams struct {
	// Limit maps to the "limit" query parameter (page size); 0 omits it.
	Limit int

	// Offset maps to the "offset" query parameter (page offset); 0 omits it.
	Offset int

	// Sort maps to the "sort" query parameter: "created_at" sorts oldest
	// first, "-created_at" newest first. Empty omits the parameter; the API's
	// contract default is "-created_at" (newest first).
	Sort string

	// Project maps to the "project" query parameter (project slug filter).
	Project string

	// Author maps to the "author" query parameter (author user ID); 0 omits it.
	Author int64

	// Assignee maps to the "assignee" query parameter (assignee user ID); 0 omits it.
	Assignee int64

	// Statuses maps to the "statuses" query parameter as a comma-separated
	// list, e.g. "New,In Progress".
	Statuses string

	// Priorities maps to the "priorities" query parameter as a comma-separated
	// list, e.g. "Minor,Critical".
	Priorities string

	// Search maps to the "search" query parameter (full-text filter).
	Search string
}

// taskListQuery renders the set parameters of params as query values.
func taskListQuery(params ListTasksParams) url.Values {
	query := url.Values{}

	if params.Limit > 0 {
		query.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		query.Set("offset", strconv.Itoa(params.Offset))
	}
	if params.Sort != "" {
		query.Set("sort", params.Sort)
	}
	if params.Project != "" {
		query.Set("project", params.Project)
	}
	if params.Author > 0 {
		query.Set("author", strconv.FormatInt(params.Author, 10))
	}
	if params.Assignee > 0 {
		query.Set("assignee", strconv.FormatInt(params.Assignee, 10))
	}
	if params.Statuses != "" {
		query.Set("statuses", params.Statuses)
	}
	if params.Priorities != "" {
		query.Set("priorities", params.Priorities)
	}
	if params.Search != "" {
		query.Set("search", params.Search)
	}

	return query
}

// requestPath returns the canonical leading-slash form of an endpoint path.
func requestPath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}

	return "/" + path
}

// taskPath returns the /tasks/{id} path for a task ID.
func taskPath(id int64) string {
	return pathTasks + "/" + strconv.FormatInt(id, 10)
}
