package clientgo

// PaginationQuery represents pagination query parameters.
type PaginationQuery struct {
	Limit  int `query:"limit"  validate:"omitempty,min=1,max=100" default:"20"`
	Offset int `query:"offset" validate:"omitempty,min=0"         default:"0"`
}

// SortQuery represents sorting query parameters.
type SortQuery struct {
	Sort string `query:"sort" validate:"omitempty"`
}

// TaskListQuery represents pagination and sorting query parameters for tasks.
type TaskListQuery struct {
	PaginationQuery
	SortQuery

	Project    *string `query:"project"    validate:"omitempty,max=255"`
	Author     *int64  `query:"author"     validate:"omitempty,min=1"`
	Assignee   *int64  `query:"assignee"   validate:"omitempty,min=0"`
	Statuses   *string `query:"statuses"`
	Priorities *string `query:"priorities"`
	Search     *string `query:"search"     validate:"omitempty,max=255"`
}

// FeedQuery represents pagination query parameters for the task feed.
type FeedQuery struct {
	PaginationQuery
}

// ListFilter represents query parameters for filtering users.
//
//	@Description	Query parameters for filtering and paginating users list.
type ListFilter struct {
	Status *string `query:"status" validate:"omitempty,oneof=pending active blocked" enums:"pending,active,blocked"`
	Role   *string `query:"role"   validate:"omitempty,oneof=admin user"             enums:"admin,user"`
	Limit  int     `query:"limit"  validate:"min=1,max=100"                                                         default:"20"`
	Offset int     `query:"offset" validate:"min=0"                                                                 default:"0"`
}

// SearchQuery represents query parameters for searching users by name.
type SearchQuery struct {
	PaginationQuery

	Query string `query:"query" validate:"required,min=1,max=255"`
}
