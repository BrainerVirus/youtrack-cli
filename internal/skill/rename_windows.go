package skill

import (
	"errors"
	"syscall"
)

// Windows error codes a rename returns while another process holds the
// directory or a file in it open.
const (
	errorAccessDenied     syscall.Errno = 5
	errorSharingViolation syscall.Errno = 32
)

func isTransientRename(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == errorAccessDenied || errno == errorSharingViolation)
}
