package clientgo

import "net/http"

// Option configures a Client during New.
type Option func(*clientConfig)

// clientConfig collects the option values applied by New.
type clientConfig struct {
	httpClient *http.Client
}

// WithHTTPClient sets the HTTP client used for requests; New falls back to a
// fresh stdlib [http.Client] when the option is absent or carries nil. It is
// the ONLY option: credentials are VALUES (Auth, Tokens), not configuration.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(cfg *clientConfig) {
		cfg.httpClient = httpClient
	}
}
