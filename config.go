package clientgo

import "net/http"

// Config configures a Client.
type Config struct {
	// BaseURL is the API root URL, e.g. "https://issues.example.com". The
	// client appends the /api/v1/... paths to it; a trailing slash is
	// tolerated and path prefixes are preserved.
	BaseURL string

	// HTTPClient sends the requests; when nil the default HTTP client is used.
	HTTPClient *http.Client

	// accessToken is sent as a bearer credential on every request when set.
	accessToken string
}

// WithBaseURL returns a copy of the config with the given base URL.
func (c Config) WithBaseURL(url string) Config {
	c.BaseURL = url

	return c
}

// WithJWTAuth returns a copy of the config that sends the access token as an
// Authorization bearer credential on every request.
func (c Config) WithJWTAuth(token string) Config {
	c.accessToken = token

	return c
}
