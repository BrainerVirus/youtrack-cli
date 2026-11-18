//go:build windows

package upgrade

import "syscall"

// detachedProcess is DETACHED_PROCESS: the child gets no console.
const detachedProcess = 0x00000008

// detached starts the child without a console, in its own process group, so
// the caller's console and Ctrl+C never reach it.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}
