package clientgo

// AuthorizeResponse is the JSON body of the authorize endpoint.
type AuthorizeResponse struct {
	URL string `json:"url" example:"https://bitbucket.org/site/oauth2/authorize?client_id=...&response_type=code&scope=webhook&state=...&redirect_uri=..."`
}

// StatusResponse reports the current OAuth connection state.
type StatusResponse struct {
	Connected   bool     `json:"connected"`
	ConnectedAt *string  `json:"connected_at,omitempty" example:"2026-08-24T12:00:00Z"`
	ExpiresAt   *string  `json:"expires_at,omitempty"   example:"2026-08-24T14:00:00Z"`
	Scopes      []string `json:"scopes,omitempty"       example:"webhook"`
}
