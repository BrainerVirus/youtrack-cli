package clierr

import (
	"errors"
	"fmt"
	"testing"
)

type statusErr int

func (s statusErr) Error() string   { return fmt.Sprintf("HTTP %d", int(s)) }
func (s statusErr) HTTPStatus() int { return int(s) }

func TestCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ExitCode
	}{
		{"given no error, it exits 0", nil, ExitOK},
		{"given a generic error, it exits 1", errors.New("boom"), ExitError},
		{"given a silent error, it exits 1", ErrSilent, ExitError},
		{"given a usage error, it exits 1 like gh", FlagErrorf("bad flag"), ExitError},
		{"given missing credentials, it exits 4", AuthErrorf("not logged in"), ExitAuthRequired},
		{"given an HTTP 401 from the API, it exits 4", fmt.Errorf("req: %w", statusErr(401)), ExitAuthRequired},
		{"given an HTTP 403 from the API, it exits 1", statusErr(403), ExitError},
		{"given a cancelled prompt, it exits 2", fmt.Errorf("prompt: %w", ErrCancel), ExitCancel},
		{"given a usage error wrapping missing credentials, auth wins", FlagErrorWrap(AuthErrorf("x")), ExitAuthRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Code(tt.err); got != tt.want {
				t.Errorf("Code(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
