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
