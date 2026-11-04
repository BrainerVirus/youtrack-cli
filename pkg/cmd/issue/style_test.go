package issue_test

import (
	"strings"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

// styledEnv is a logged-in color terminal whose clock is fixed, so relative
// times in golden files never drift.
func styledEnv(t *testing.T, color bool) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
	t.Helper()
	t.Setenv("YTRACK_NO_UPDATE_NOTIFIER", "1")
	env, yt := loggedIn(t, "")
	env.Factory.Now = func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) }
	env.IO.SetStdoutTTY(true)
	env.IO.SetStderrTTY(true)
	env.IO.SetColorEnabled(color)
	env.IO.SetErrColorEnabled(color)
	env.Reset()
	return env, yt
}

func TestIssueListStyling(t *testing.T) {
	for _, color := range []bool{true, false} {
		name := map[bool]string{true: "styled", false: "plain"}[color]
		t.Run("given a "+name+" terminal, issue list is pinned", func(t *testing.T) {
			env, _ := styledEnv(t, color)
			mustRun(t, env, "issue", "list", "--all")
			cmdtest.Golden(t, "list-"+name, env.Stdout.String())
		})
	}
}

func TestIssueViewStyling(t *testing.T) {
	for _, color := range []bool{true, false} {
		name := map[bool]string{true: "styled", false: "plain"}[color]
		t.Run("given a "+name+" terminal, issue view is pinned", func(t *testing.T) {
			env, yt := styledEnv(t, color)
			mustRun(t, env, "issue", "view", "NSR-40", "--comments")
			cmdtest.Golden(t, "view-"+name, strings.ReplaceAll(env.Stdout.String(), yt.URL(), "{{URL}}"))
		})
	}

	t.Run("given --json on a color terminal, the output has no ANSI escape", func(t *testing.T) {
		env, _ := styledEnv(t, true)
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable,summary,state")
		if out := env.Stdout.String() + env.Stderr.String(); strings.Contains(out, "\x1b") {
			t.Errorf("output = %q", out)
		}
	})
}
