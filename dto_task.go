package clientgo

// TaskResponse represents the API response for a single task.
//
//	@Description	Full task details with nested author and assignee information.
type TaskResponse struct {
	ID          int64      `json:"id"`
	ProjectSlug string     `json:"project_slug"`
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Priority    string     `json:"priority"`
	Status      string     `json:"status"`
	Kind        string     `json:"kind,omitempty"`
	Author      UserBrief  `json:"author"`
	Assignee    *UserBrief `json:"assignee,omitempty"`
	DueDate     *string    `json:"due_date,omitempty"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
}

type TaskDetailsResponse struct {
	TaskResponse

	Project     *Project             `json:"project"`
	Attachments []AttachmentResponse `json:"attachments"`
	Comments    []CommentResponse    `json:"comments"`
}

// TaskListResponse represents the API response for a list of tasks.
//
//	@Description	Paginated list of tasks with total count.
type TaskListResponse struct {
	Items []TaskResponse `json:"items"`
	Total int            `json:"total"`
}

// TaskCreateRequest represents the request body for creating a new task.
//
//	@Description	Task creation request with title, description, priority, assignee, and due date.
type TaskCreateRequest struct {
	ProjectSlug string  `json:"project_slug"          validate:"required,max=255"`
	Title       string  `json:"title"                 validate:"required,max=255"`
	Description string  `json:"description,omitempty" validate:"max=10000"`
	Priority    string  `json:"priority,omitempty"    validate:"omitempty,oneof=Trivial Minor Major Critical Blocker"`
	Kind        string  `json:"kind,omitempty"        validate:"omitempty,oneof=Bug Enhancement Task Proposal"`
	AssigneeID  *int64  `json:"assignee_id,omitempty" validate:"omitempty,min=1"`
	DueDate     *string `json:"due_date,omitempty"    validate:"omitempty,datetime=2006-01-02"`
}

// TaskUpdateRequest represents the request body for updating a task.
//
//	@Description	Task update request with optional fields for partial updates.
type TaskUpdateRequest struct {
	Title       *string `json:"title,omitempty"       validate:"omitempty,max=255"`
	Description *string `json:"description,omitempty" validate:"omitempty,max=10000"`
	Priority    *string `json:"priority,omitempty"    validate:"omitempty,oneof=Trivial Minor Major Critical Blocker"                                                default:"Minor"`
	Status      *string `json:"status,omitempty"      validate:"omitempty,oneof=New Open 'In Progress' Resolved Closed Reopened Invalid Duplicate Wontfix 'On Hold'"`
	Kind        *string `json:"kind,omitempty"        validate:"omitempty,oneof=Bug Enhancement Task Proposal"                                                       default:"Task"`
	AssigneeID  *int64  `json:"assignee_id,omitempty" validate:"omitempty,min=0"`
	DueDate     *string `json:"due_date,omitempty"    validate:"omitzero,datetime=2006-01-02"`
	Comment     *string `json:"comment,omitempty"     validate:"omitempty,max=10000"`
}
