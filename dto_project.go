package clientgo

// Project represents the API response for a single project.
type Project struct {
	ID        string `json:"id"         example:"backend-service"`
	Name      string `json:"name"       example:"Backend Service"`
	RepoURL   string `json:"repo_url"   example:"https://bitbucket.org/company/backend"`
	CreatedAt string `json:"created_at" example:"2026-04-01T08:00:00Z"`
	UpdatedAt string `json:"updated_at" example:"2026-04-02T09:00:00Z"`
}

// ProjectRequest represents the request body for creating a new project.
type ProjectRequest struct {
	Name    string `json:"name"     validate:"required"     example:"Backend Service"`
	RepoURL string `json:"repo_url" validate:"required,url" example:"https://bitbucket.org/company/backend"`
}

// ProjectUpdateRequest represents the request body for updating a project.
type ProjectUpdateRequest struct {
	Name    *string `json:"name,omitempty"     validate:"omitempty"     example:"Backend Service"`
	RepoURL *string `json:"repo_url,omitempty" validate:"omitempty,url" example:"https://bitbucket.org/company/backend"`
}

// ProjectListResponse represents the API response for a list of projects.
type ProjectListResponse struct {
	Items []Project `json:"items"`
	Total int       `json:"total" example:"1"`
}
