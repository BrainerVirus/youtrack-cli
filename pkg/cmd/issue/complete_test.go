package issue_test

import (
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func TestCompletion(t *testing.T) {
	t.Setenv("YTRACK_NO_UPDATE_NOTIFIER", "1")

	t.Run("given a logged-in host, an issue argument completes IDs with their summaries", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Reset()
		mustRun(t, env, "__complete", "issue", "view", "NSR-4")
		out := env.Stdout.String()
		if !strings.Contains(out, "NSR-40\tLogin redirect drops the return URL\n") || !strings.HasSuffix(out, ":4\n") {
			t.Errorf("stdout = %q, want NSR-40 with its summary and no file completion", out)
		}
		q := query(t, requests(yt, "GET", "/api/issues")[0])
		if got := q.Get("query"); got != "project: NSR #Unresolved sort by: updated desc" {
			t.Errorf("query = %q", got)
		}
	})

	t.Run("given issue command, every argument completes issues", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		env.Reset()
		mustRun(t, env, "__complete", "issue", "command", "NSR-40", "NSR")
		if !strings.Contains(env.Stdout.String(), "NSR-40\t") {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given an argument past the issue, it completes nothing and sends nothing", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		before := len(yt.Requests())
		env.Reset()
		mustRun(t, env, "__complete", "issue", "view", "NSR-40", "")
		if got := env.Stdout.String(); got != ":4\n" {
			t.Errorf("stdout = %q", got)
		}
		if len(yt.Requests()) != before {
			t.Error("sent a request")
		}
	})

	t.Run("given --project, it completes project short names", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedSampleProjects()
		env.Reset()
		mustRun(t, env, "__complete", "issue", "list", "--project", "n")
		if !strings.Contains(env.Stdout.String(), "NSR\tNightshift\n") {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given no login, it completes nothing and prints no error", func(t *testing.T) {
		env := cmdtest.New(t)
		mustRun(t, env, "__complete", "issue", "view", "")
		if got := env.Stdout.String(); got != ":4\n" {
			t.Errorf("stdout = %q", got)
		}
		if strings.Contains(env.Stderr.String(), "ytrack:") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})
}
