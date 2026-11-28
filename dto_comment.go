package clientgo

// CommentResponse represents the API response for a single comment.
//
//	@Description	Full comment details with author information.
type CommentResponse struct {
	ID        int64     `json:"id"`
	Author    UserBrief `json:"author"`
	Content   string    `json:"content"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
}

// CommentCreateRequest represents the request body for creating a new comment.
//
//	@Description	Comment creation request with content.
type CommentCreateRequest struct {
	Content string `json:"content" validate:"required,min=1,max=10000"`
}

// CommentUpdateRequest represents the request body for updating a comment.
//
//	@Description	Comment update request with content.
type CommentUpdateRequest struct {
	Content string `json:"content" validate:"required,min=1,max=10000"`
}
