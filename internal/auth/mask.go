package auth

import "strings"

// Mask hides a token for display, keeping only YouTrack's "perm:"/"perm-"
// prefix so users can tell a permanent token from something else.
func Mask(token string) string {
	for _, p := range []string{"perm:", "perm-"} {
		if strings.HasPrefix(token, p) {
			return p + "****"
		}
	}
	return "****"
}
