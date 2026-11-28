package clientgo_test

import (
	"reflect"
	"strings"
	"testing"

	clientgo "github.com/bit-issues/client-go"
)

func taggedFields(t reflect.Type, tagName string) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	var out []string
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous {
			out = append(out, taggedFields(field.Type, tagName)...)
			continue
		}

		tag, _, _ := strings.Cut(field.Tag.Get(tagName), ",")
		if tag == "" || tag == "-" {
			continue
		}
		out = append(out, tag)
	}

	return out
}

func TestWireDTOJSONFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		typ  reflect.Type
		want []string
	}{
		{"RegisterRequest", reflect.TypeFor[clientgo.RegisterRequest](), []string{"email", "password"}},
		{
			"ChangePasswordRequest",
			reflect.TypeFor[clientgo.ChangePasswordRequest](),
			[]string{"old_password", "new_password"},
		},
		{"ProjectRequest", reflect.TypeFor[clientgo.ProjectRequest](), []string{"name", "repo_url"}},
		{"ProjectUpdateRequest", reflect.TypeFor[clientgo.ProjectUpdateRequest](), []string{"name", "repo_url"}},
		{"ProjectListResponse", reflect.TypeFor[clientgo.ProjectListResponse](), []string{"items", "total"}},
		{
			"TaskCreateRequest",
			reflect.TypeFor[clientgo.TaskCreateRequest](),
			[]string{"project_slug", "title", "description", "priority", "kind", "assignee_id", "due_date"},
		},
		{
			"TaskUpdateRequest",
			reflect.TypeFor[clientgo.TaskUpdateRequest](),
			[]string{"title", "description", "priority", "status", "kind", "assignee_id", "due_date", "comment"},
		},
		{
			"AttachmentUploadRequest",
			reflect.TypeFor[clientgo.AttachmentUploadRequest](),
			[]string{"file_name", "size_bytes"},
		},
		{
			"AttachmentUploadResponse",
			reflect.TypeFor[clientgo.AttachmentUploadResponse](),
			[]string{"id", "file_name", "size_bytes", "upload_url"},
		},
		{
			"AttachmentDownloadResponse",
			reflect.TypeFor[clientgo.AttachmentDownloadResponse](),
			[]string{"download_url"},
		},
		{"CommentCreateRequest", reflect.TypeFor[clientgo.CommentCreateRequest](), []string{"content"}},
		{"CommentUpdateRequest", reflect.TypeFor[clientgo.CommentUpdateRequest](), []string{"content"}},
		{"UserBriefList", reflect.TypeFor[clientgo.UserBriefList](), []string{"items", "total"}},
		{"UpdateRequest", reflect.TypeFor[clientgo.UpdateRequest](), []string{"status", "role"}},
		{"ListResponse", reflect.TypeFor[clientgo.ListResponse](), []string{"items", "total"}},
		{
			"HistoryChangeResponse",
			reflect.TypeFor[clientgo.HistoryChangeResponse](),
			[]string{"field", "old_value", "new_value"},
		},
		{
			"HistoryEntryResponse",
			reflect.TypeFor[clientgo.HistoryEntryResponse](),
			[]string{"id", "action", "actor", "changes", "created_at"},
		},
		{"FeedResponse", reflect.TypeFor[clientgo.FeedResponse](), []string{"items", "total"}},
		{"CredentialResponse", reflect.TypeFor[clientgo.CredentialResponse](), []string{"id", "name", "created_at"}},
		{"RenameRequest", reflect.TypeFor[clientgo.RenameRequest](), []string{"name"}},
		{"AuthorizeResponse", reflect.TypeFor[clientgo.AuthorizeResponse](), []string{"url"}},
		{
			"StatusResponse",
			reflect.TypeFor[clientgo.StatusResponse](),
			[]string{"connected", "connected_at", "expires_at", "scopes"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := taggedFields(tc.typ, "json"); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("json fields = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWireDTOQueryFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		typ  reflect.Type
		want []string
	}{
		{"PaginationQuery", reflect.TypeFor[clientgo.PaginationQuery](), []string{"limit", "offset"}},
		{"SortQuery", reflect.TypeFor[clientgo.SortQuery](), []string{"sort"}},
		{
			"TaskListQuery",
			reflect.TypeFor[clientgo.TaskListQuery](),
			[]string{"limit", "offset", "sort", "project", "author", "assignee", "statuses", "priorities", "search"},
		},
		{"FeedQuery", reflect.TypeFor[clientgo.FeedQuery](), []string{"limit", "offset"}},
		{"ListFilter", reflect.TypeFor[clientgo.ListFilter](), []string{"status", "role", "limit", "offset"}},
		{"SearchQuery", reflect.TypeFor[clientgo.SearchQuery](), []string{"limit", "offset", "query"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := taggedFields(tc.typ, "query"); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("query fields = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRequestDTOValidationTags(t *testing.T) {
	t.Parallel()

	types := []reflect.Type{
		reflect.TypeFor[clientgo.RegisterRequest](),
		reflect.TypeFor[clientgo.ChangePasswordRequest](),
		reflect.TypeFor[clientgo.ProjectRequest](),
		reflect.TypeFor[clientgo.ProjectUpdateRequest](),
		reflect.TypeFor[clientgo.TaskCreateRequest](),
		reflect.TypeFor[clientgo.TaskUpdateRequest](),
		reflect.TypeFor[clientgo.CommentCreateRequest](),
		reflect.TypeFor[clientgo.CommentUpdateRequest](),
		reflect.TypeFor[clientgo.AttachmentUploadRequest](),
		reflect.TypeFor[clientgo.UpdateRequest](),
		reflect.TypeFor[clientgo.RenameRequest](),
	}

	for _, typ := range types {
		t.Run(typ.Name(), func(t *testing.T) {
			t.Parallel()

			for i := range typ.NumField() {
				if typ.Field(i).Tag.Get("validate") == "" {
					t.Errorf("field %s has no validate tag", typ.Field(i).Name)
				}
			}
		})
	}
}
