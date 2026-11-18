//go:build !windows

package skill

// isTransientRename is false off Windows, where a rename never fails because
// another process holds the directory open.
func isTransientRename(error) bool { return false }
