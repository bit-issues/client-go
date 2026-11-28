package clientgo

// UserBrief represents a minimal user profile for task author/assignee.
//
//	@Description	Minimal user information for task relationships.
type UserBrief struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// UserBriefList represents a paginated list of brief user profiles.
type UserBriefList struct {
	Items []UserBrief `json:"items"`
	Total int         `json:"total"`
}

// UpdateRequest represents admin request to update user status/role.
//
//	@Description	Admin can update user status (active/blocked/pending) and role (admin/user).
type UpdateRequest struct {
	Status *string `json:"status,omitempty" validate:"omitempty,oneof=pending active blocked" enums:"pending,active,blocked"`
	Role   *string `json:"role,omitempty"   validate:"omitempty,oneof=admin user"             enums:"admin,user"`
}

// ListResponse represents paginated list of users.
//
//	@Description	Paginated response containing users list and total count.
type ListResponse struct {
	Items []UserResponseDTO `json:"items"`
	Total int               `json:"total" example:"42"`
}
