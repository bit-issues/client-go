package clientgo

import "net/http"

// Option configures a Client during New.
type Option func(*clientConfig)

// clientConfig collects the option values applied by New.
type clientConfig struct {
	httpClient  *http.Client
	accessToken string
	email       string
	password    string
	loginSet    bool
}

// WithHTTPClient sets the HTTP client used for requests; New falls back to a
// fresh stdlib [http.Client] when the option is absent or carries nil.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(cfg *clientConfig) {
		cfg.httpClient = httpClient
	}
}

// WithAccessToken configures a static bearer token (the CI/BITISSUES_TOKEN
// path): it is attached to every request and the client NEVER logs in or
// refreshes - a 401 is reported as ErrUnauthorized. It WINS over WithLogin.
func WithAccessToken(token string) Option {
	return func(cfg *clientConfig) {
		cfg.accessToken = token
	}
}

// WithLogin configures per-boot credentials: the first request that needs a
// bearer logs in internally (POST /api/v1/auth/login) and keeps both tokens
// in memory for the lifetime of the process. Nothing is persisted anywhere.
// New rejects an empty or whitespace email or password with
// ErrInvalidCredentials.
func WithLogin(email, password string) Option {
	return func(cfg *clientConfig) {
		cfg.loginSet = true
		cfg.email = email
		cfg.password = password
	}
}
