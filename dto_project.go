package clientgo

// Project represents the API response for a single project.
type Project struct {
	ID        string `json:"id"         example:"backend-service"`
	Name      string `json:"name"       example:"Backend Service"`
	RepoURL   string `json:"repo_url"   example:"https://bitbucket.org/company/backend"`
	CreatedAt string `json:"created_at" example:"2026-04-01T08:00:00Z"`
	UpdatedAt string `json:"updated_at" example:"2026-04-02T09:00:00Z"`
}
