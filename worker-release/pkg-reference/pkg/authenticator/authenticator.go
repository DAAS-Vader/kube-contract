package authenticator

import (
	"context"
	"k8s.io/apiserver/pkg/authentication/authenticator"
)

// Request represents an authentication request
type Request struct {
	RemoteAddr string
	UserAgent  string
}

// User represents an authenticated user
type User struct {
	Name     string
	Username string
	UID      string
	Groups   []string
}

// TokenAuthenticator interface for token authentication
type TokenAuthenticator interface {
	AuthenticateToken(ctx context.Context, token string) (*authenticator.Response, bool, error)
}