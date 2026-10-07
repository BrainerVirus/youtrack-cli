// Package adapter is the typed YouTrack API surface commands use. It maps
// wire JSON to ytrack's own types so commands never depend on raw payloads.
package adapter

import (
	"context"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// User is a YouTrack account.
type User struct {
	Login    string `json:"login"`
	FullName string `json:"fullName"`
}

// CurrentUser returns the account the client's token belongs to.
func CurrentUser(ctx context.Context, c *transport.Client) (User, error) {
	var u User
	err := c.GetJSON(ctx, "/api/users/me?fields=login,fullName", &u)
	return u, err
}
