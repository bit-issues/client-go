package clientgo

import "errors"

// ErrInvalidBaseURL classifies a New call with an empty or whitespace base
// URL; New rejects it instead of panicking later.
var ErrInvalidBaseURL = errors.New("base URL must not be empty")

// ErrInvalidCredentials classifies a Login call with an empty or whitespace
// email or password; Login rejects it before any request is sent.
var ErrInvalidCredentials = errors.New("login email and password must not be empty")

// ErrInvalidResponse classifies a successful response that lacks the tokens
// required by the API contract.
var ErrInvalidResponse = errors.New("invalid API response")

// ErrSessionExpired classifies a refresh whose refresh token the backend
// rejected (401: revoked or expired): the session is over and the caller
// must log in again. The message deliberately carries no token value, and a
// transient failure (5xx, transport) keeps its restkit classification instead.
var ErrSessionExpired = errors.New("session expired - log in again")
