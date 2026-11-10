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
}

// TaskListResponse represents the API response for a list of tasks.
//
//	@Description	Paginated list of tasks with total count.
type TaskListResponse struct {
	Items []TaskResponse `json:"items"`
	Total int            `json:"total"`
}
