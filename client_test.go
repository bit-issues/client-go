package clientgo_test

import (
	"errors"
	"net/http"
	"testing"

	clientgo "github.com/bit-issues/client-go"
)

// Named values for mnd compliance in assertions and fixtures.
const (
	testProjectTotal   = 7
	testProjectCount   = 2
	testTaskID         = 42
	testTaskNumber     = 7
	testAssigneeUserID = 2
	testListLimit      = 20
	testListOffset     = 40
	testListAuthorID   = 7
	testListAssigneeID = 3
)

// TestListProjects checks the base URL join (trailing slash tolerated), the
// /api/v1 prefix, the bearer header and the decoded list payload.
func TestListProjects(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/v1/projects" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/projects")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[` +
			`{"id":"backend-service","name":"Backend Service","repo_url":"https://bitbucket.org/company/backend","created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-02T09:00:00Z"},` +
			`{"id":"frontend","name":"Frontend","repo_url":"https://bitbucket.org/company/frontend","created_at":"2026-04-03T08:00:00Z","updated_at":"2026-04-04T09:00:00Z"}` +
			`],"total":7}`))
	}

	client := clientgo.NewClient(
		clientgo.Config{BaseURL: newStubServer(t, handler) + "/"}.WithJWTAuth("test-token"),
	)

	items, total, err := client.ListProjects(t.Context())

	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if total != testProjectTotal {
		t.Errorf("total = %d, want 7", total)
	}
	if len(items) != testProjectCount {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].ID != "backend-service" {
		t.Errorf("items[0].ID = %q, want %q", items[0].ID, "backend-service")
	}
	if items[0].RepoURL != "https://bitbucket.org/company/backend" {
		t.Errorf("items[0].RepoURL = %q", items[0].RepoURL)
	}
	if items[1].Name != "Frontend" {
		t.Errorf("items[1].Name = %q, want %q", items[1].Name, "Frontend")
	}
}

// TestGetProject checks an unauthenticated GET of a single project and that no
// Content-Type header is sent with a body-less request.
func TestGetProject(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/v1/projects/backend-service" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/projects/backend-service")
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}
		if got := r.Header.Get("Content-Type"); got != "" {
			t.Errorf("Content-Type = %q, want empty", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"backend-service","name":"Backend Service",` +
			`"repo_url":"https://bitbucket.org/company/backend",` +
			`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-02T09:00:00Z"}`))
	})

	project, err := client.GetProject(t.Context(), "backend-service")

	if err != nil {
		t.Fatalf("GetProject() error = %v", err)
	}
	if project.ID != "backend-service" {
		t.Errorf("project.ID = %q, want %q", project.ID, "backend-service")
	}
	if project.Name != "Backend Service" {
		t.Errorf("project.Name = %q, want %q", project.Name, "Backend Service")
	}
	if project.CreatedAt != "2026-04-01T08:00:00Z" {
		t.Errorf("project.CreatedAt = %q, want %q", project.CreatedAt, "2026-04-01T08:00:00Z")
	}
}

// TestListTasksSendsAllQueryParams checks every supported REST query parameter
// is forwarded with the right name and value, and that the list decodes.
func TestListTasksSendsAllQueryParams(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/v1/tasks" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/tasks")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
		}

		query := r.URL.Query()
		if len(query) != 9 {
			t.Errorf("query params = %v, want exactly 9", query)
		}
		wantParams := map[string]string{
			"limit":      "20",
			"offset":     "40",
			"sort":       "-created_at",
			"project":    "backend-service",
			"author":     "7",
			"assignee":   "3",
			"statuses":   "New,In Progress",
			"priorities": "Minor,Critical",
			"search":     "nil pointer dereference",
		}
		for name, want := range wantParams {
			if got := query.Get(name); got != want {
				t.Errorf("query[%q] = %q, want %q", name, got, want)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{` +
			`"id":42,"project_slug":"backend-service","number":7,"title":"Fix login crash",` +
			`"description":"Steps to reproduce the crash.",` +
			`"priority":"Major","status":"In Progress","kind":"Bug",` +
			`"author":{"id":1,"name":"alice","role":"admin","created_at":"2026-01-01T00:00:00Z"},` +
			`"assignee":{"id":2,"name":"bob","role":"user","created_at":"2026-01-02T00:00:00Z"},` +
			`"due_date":"2026-05-01",` +
			`"created_at":"2026-04-01T08:00:00Z","updated_at":"2026-04-02T09:00:00Z"` +
			`}],"total":1}`))
	}

	client := clientgo.NewClient(
		clientgo.Config{BaseURL: newStubServer(t, handler)}.WithJWTAuth("test-token"),
	)

	params := clientgo.ListTasksParams{
		Limit:      testListLimit,
		Offset:     testListOffset,
		Sort:       "-created_at",
		Project:    "backend-service",
		Author:     testListAuthorID,
		Assignee:   testListAssigneeID,
		Statuses:   "New,In Progress",
		Priorities: "Minor,Critical",
		Search:     "nil pointer dereference",
	}

	list, err := client.ListTasks(t.Context(), params)

	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if list.Total != 1 {
		t.Errorf("list.Total = %d, want 1", list.Total)
	}
	if len(list.Items) != 1 {
		t.Fatalf("len(list.Items) = %d, want 1", len(list.Items))
	}

	task := list.Items[0]
	if task.ID != testTaskID {
		t.Errorf("task.ID = %d, want 42", task.ID)
	}
	if task.Number != testTaskNumber {
		t.Errorf("task.Number = %d, want 7", task.Number)
	}
	if task.Status != "In Progress" {
		t.Errorf("task.Status = %q, want %q", task.Status, "In Progress")
	}
	if task.Priority != "Major" {
		t.Errorf("task.Priority = %q, want %q", task.Priority, "Major")
	}
	if task.Kind != "Bug" {
		t.Errorf("task.Kind = %q, want %q", task.Kind, "Bug")
	}
	if task.Author.Name != "alice" {
		t.Errorf("task.Author.Name = %q, want %q", task.Author.Name, "alice")
	}
	if task.Assignee == nil || task.Assignee.ID != testAssigneeUserID {
		t.Errorf("task.Assignee = %+v, want ID 2", task.Assignee)
	}
	if task.DueDate == nil || *task.DueDate != "2026-05-01" {
		t.Errorf("task.DueDate = %v, want 2026-05-01", task.DueDate)
	}
}

// TestListTasksOmitsEmptyParams checks that zero-valued parameters are left
// out of the query string entirely.
func TestListTasksOmitsEmptyParams(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if query := r.URL.Query(); len(query) != 0 {
			t.Errorf("query params = %v, want none", query)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}

	client := newTestClient(t, handler)

	list, err := client.ListTasks(t.Context(), clientgo.ListTasksParams{})

	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if list.Total != 0 {
		t.Errorf("list.Total = %d, want 0", list.Total)
	}
	if len(list.Items) != 0 {
		t.Errorf("len(list.Items) = %d, want 0", len(list.Items))
	}
}

// TestGetTaskByIDDecodesBaseFields requests /api/v1/tasks/{id} with a
// details-shaped payload and decodes only the base task fields; the extra
// project/comments/attachments keys must be ignored by the decoder.
func TestGetTaskByIDDecodesBaseFields(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/v1/tasks/42" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/tasks/42")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(taskDetailsFixture))
	})

	task, err := client.GetTaskByID(t.Context(), 42)

	if err != nil {
		t.Fatalf("GetTaskByID() error = %v", err)
	}
	if task.ID != testTaskID {
		t.Errorf("task.ID = %d, want 42", task.ID)
	}
	if task.ProjectSlug != "backend-service" {
		t.Errorf("task.ProjectSlug = %q, want %q", task.ProjectSlug, "backend-service")
	}
	if task.Title != "Fix login crash" {
		t.Errorf("task.Title = %q, want %q", task.Title, "Fix login crash")
	}
	if task.Status != "In Progress" {
		t.Errorf("task.Status = %q, want %q", task.Status, "In Progress")
	}
	if task.Author.ID != 1 || task.Author.Name != "alice" {
		t.Errorf("task.Author = %+v, want ID 1 name alice", task.Author)
	}
	if task.Assignee == nil || task.Assignee.Name != "bob" {
		t.Errorf("task.Assignee = %+v, want name bob", task.Assignee)
	}
	if task.DueDate == nil || *task.DueDate != "2026-05-01" {
		t.Errorf("task.DueDate = %v, want 2026-05-01", task.DueDate)
	}
}

// TestGetTaskByNumber checks the slug + number lookup path.
func TestGetTaskByNumber(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/v1/tasks/backend-service/7" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/tasks/backend-service/7")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(taskDetailsFixture))
	})

	task, err := client.GetTaskByNumber(t.Context(), "backend-service", 7)

	if err != nil {
		t.Fatalf("GetTaskByNumber() error = %v", err)
	}
	if task.Number != testTaskNumber {
		t.Errorf("task.Number = %d, want 7", task.Number)
	}
	if task.ProjectSlug != "backend-service" {
		t.Errorf("task.ProjectSlug = %q, want %q", task.ProjectSlug, "backend-service")
	}
}

// TestGetTaskDetails checks the full details payload with project, comments
// and attachments, including the presigned download URL kept in the DTO.
func TestGetTaskDetails(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/v1/tasks/42" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/tasks/42")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(taskDetailsFixture))
	})

	details, err := client.GetTaskDetails(t.Context(), 42)

	if err != nil {
		t.Fatalf("GetTaskDetails() error = %v", err)
	}
	if details.Project == nil || details.Project.ID != "backend-service" {
		t.Errorf("details.Project = %+v, want ID backend-service", details.Project)
	}
	if len(details.Comments) != 1 {
		t.Fatalf("len(details.Comments) = %d, want 1", len(details.Comments))
	}
	if details.Comments[0].Content != "ping" {
		t.Errorf("comments[0].Content = %q, want %q", details.Comments[0].Content, "ping")
	}
	if len(details.Attachments) != 1 {
		t.Fatalf("len(details.Attachments) = %d, want 1", len(details.Attachments))
	}
	if details.Attachments[0].FileName != "log.txt" {
		t.Errorf("attachments[0].FileName = %q, want %q", details.Attachments[0].FileName, "log.txt")
	}
	if details.Attachments[0].DownloadURL == "" {
		t.Error("attachments[0].DownloadURL is empty, want the presigned URL kept in the DTO")
	}
}

// TestBaseURLWithPathPrefix checks the join is clean when the base URL carries
// a path prefix and a trailing slash.
func TestBaseURLWithPathPrefix(t *testing.T) {
	t.Parallel()

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bit/api/v1/projects" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/bit/api/v1/projects")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}

	client := clientgo.NewClient(clientgo.Config{BaseURL: newStubServer(t, handler) + "/bit/"})

	_, _, err := client.ListProjects(t.Context())

	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
}

// TestEmptyBodyIsSuccess checks a 200 response without a body does not fail.
func TestEmptyBodyIsSuccess(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/projects")
		}
		w.WriteHeader(http.StatusOK)
	})

	items, total, err := client.ListProjects(t.Context())

	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Errorf("total = %d, items = %+v, want zero values", total, items)
	}
}

// TestGetProjectNotFound checks a 404 envelope becomes a typed ErrNotFound.
func TestGetProjectNotFound(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"project not found"}`))
	})

	_, err := client.GetProject(t.Context(), "missing")

	if err == nil {
		t.Fatal("GetProject() error = nil, want error")
	}
	if !errors.Is(err, clientgo.ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}

	var apiErr *clientgo.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(*APIError) = false, err = %v", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, http.StatusNotFound)
	}
	if apiErr.Message != "project not found" {
		t.Errorf("APIError.Message = %q, want %q", apiErr.Message, "project not found")
	}
}

// taskDetailsFixture is the wire payload of GET /api/v1/tasks/{id}: the base
// task fields plus project, comments and attachments.
const taskDetailsFixture = `{
	"id": 42,
	"project_slug": "backend-service",
	"number": 7,
	"title": "Fix login crash",
	"description": "Steps to reproduce the crash.",
	"priority": "Major",
	"status": "In Progress",
	"kind": "Bug",
	"author": {"id": 1, "name": "alice", "role": "admin", "created_at": "2026-01-01T00:00:00Z"},
	"assignee": {"id": 2, "name": "bob", "role": "user", "created_at": "2026-01-02T00:00:00Z"},
	"due_date": "2026-05-01",
	"created_at": "2026-04-01T08:00:00Z",
	"updated_at": "2026-04-02T09:00:00Z",
	"project": {
		"id": "backend-service",
		"name": "Backend Service",
		"repo_url": "https://bitbucket.org/company/backend",
		"created_at": "2026-04-01T08:00:00Z",
		"updated_at": "2026-04-02T09:00:00Z"
	},
	"comments": [
		{
			"id": 10,
			"author": {"id": 1, "name": "alice", "role": "admin", "created_at": "2026-01-01T00:00:00Z"},
			"content": "ping",
			"created_at": "2026-04-01T10:00:00Z",
			"updated_at": "2026-04-01T10:00:00Z"
		}
	],
	"attachments": [
		{
			"id": 5,
			"file_name": "log.txt",
			"size_bytes": 1024,
			"uploaded_by": {"id": 1, "name": "alice", "role": "admin", "created_at": "2026-01-01T00:00:00Z"},
			"uploaded_at": "2026-04-01T11:00:00Z",
			"download_url": "https://minio.example.com/bucket/log.txt?X-Amz-Signature=abc"
		}
	]
}`
