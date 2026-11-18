//go:build !windows

package upgrade

import "syscall"

// detached starts the child in a new session, away from the caller's
// terminal and process group.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
