package issue_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

// ignoreCommands makes the fake accept every YouTrack command (200) without
// applying it, as a workflow that reverts the change would.
func ignoreCommands(yt *cmdtest.FakeYouTrack) {
	next := yt.Server.Config.Handler
	yt.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/commands") {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"commands":[]}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestIssueCreateChecksFieldsApplied(t *testing.T) {
	t.Run("given YouTrack accepts a field on create but leaves it, it prints the new issue and exits 1 naming the field", func(t *testing.T) {
		env, yt := writable(t)
		yt.IgnoreFieldWrites = []string{"Type"}
		if code := env.Run("issue", "create", "-p", "NSR", "-s", "Spike", "--field", "Type=Task", "--field", "Priority=Minor"); code != 1 {
			t.Fatalf("exit %d, stderr = %q", code, env.Stderr)
		}
		if !strings.HasPrefix(env.Stdout.String(), "NSR-102\t") {
			t.Errorf("stdout = %q, want the created issue", env.Stdout)
		}
		errText := env.Stderr.String()
		if !strings.Contains(errText, "created NSR-102: YouTrack accepted the change, but Type on NSR-102 did not take the new value") || strings.Contains(errText, "Priority") {
			t.Errorf("stderr = %q", errText)
		}
	})

	t.Run("given a period set by a command that does not apply, it exits 1 naming the field", func(t *testing.T) {
		env, yt := writable(t)
		ignoreCommands(yt)
		if code := env.Run("issue", "create", "-p", "NSR", "-s", "Spike", "--field", "Estimation=1w 2d"); code != 1 {
			t.Fatalf("exit %d, stderr = %q", code, env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "Estimation on NSR-102 did not take the new value") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given the fields are applied, it exits 0", func(t *testing.T) {
		env, _ := writable(t)
		mustRun(t, env, "issue", "create", "-p", "NSR", "-s", "Spike", "--field", "Type=Task", "--field", "Estimation=3h")
	})
}

func TestIssueEditChecksPeriodApplied(t *testing.T) {
	t.Run("given a period set by a command that does not apply, it exits 1 naming the field", func(t *testing.T) {
		env, yt := writable(t)
		ignoreCommands(yt)
		if code := env.Run("issue", "edit", "NSR-40", "--field", "Estimation=2d", "--field", "Type=Task"); code != 1 {
			t.Fatalf("exit %d, stderr = %q", code, env.Stderr)
		}
		errText := env.Stderr.String()
		if !strings.Contains(errText, "but Estimation on NSR-40 did not take the new value") || strings.Contains(errText, "Type") {
			t.Errorf("stderr = %q", errText)
		}
	})

	t.Run("given the period is applied, it exits 0", func(t *testing.T) {
		env, _ := writable(t)
		mustRun(t, env, "issue", "edit", "NSR-40", "--field", "Estimation=1w 2d 4h")
	})
}

func TestIssueEditComparesPeriodUnderTheWorkSchedule(t *testing.T) {
	// reverted sets Estimation to 5d under YouTrack's default schedule, then
	// gives the instance 6-hour days and 4-day weeks and ignores commands, so
	// a later 1w (1440 minutes) reads back as the old 5d (2400 minutes).
	reverted := func(t *testing.T) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
		t.Helper()
		env, yt := writable(t)
		mustRun(t, env, "issue", "edit", "NSR-40", "--field", "Estimation=5d")
		yt.MinutesADay, yt.DaysAWeek = 360, 4
		ignoreCommands(yt)
		env.Reset()
		return env, yt
	}

	t.Run("given the schedule is readable and the period did not take, it exits 1 naming the field", func(t *testing.T) {
		env, yt := reverted(t)
		if code := env.Run("issue", "edit", "NSR-40", "--field", "Estimation=1w"); code != 1 {
			t.Fatalf("exit %d, stderr = %q", code, env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "but Estimation on NSR-40 did not take the new value") {
			t.Errorf("stderr = %q", env.Stderr)
		}
		reqs := requests(yt, "GET", "/api/admin/timeTrackingSettings/workTimeSettings")
		if len(reqs) == 0 || query(t, reqs[len(reqs)-1]).Get("fields") != "minutesADay,daysAWeek" {
			t.Errorf("work schedule requests = %+v", reqs)
		}
	})

	t.Run("given the schedule is forbidden, it falls back to the rule for an unknown schedule", func(t *testing.T) {
		env, yt := reverted(t)
		yt.ForbidWorkSchedule = true
		mustRun(t, env, "issue", "edit", "NSR-40", "--field", "Estimation=1w")
		if len(requests(yt, "GET", "/api/admin/timeTrackingSettings/workTimeSettings")) == 0 {
			t.Error("the work schedule was not asked for")
		}
	})

	t.Run("given weeks of the instance's length, it exits 0", func(t *testing.T) {
		env, yt := writable(t)
		yt.MinutesADay, yt.DaysAWeek = 360, 4
		mustRun(t, env, "issue", "edit", "NSR-40", "--field", "Estimation=1w 1d")
	})

	t.Run("given no period field, it does not read the schedule", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "edit", "NSR-40", "--field", "Type=Task")
		if n := len(requests(yt, "GET", "/api/admin/timeTrackingSettings/workTimeSettings")); n != 0 {
			t.Errorf("work schedule requests = %d, want 0", n)
		}
	})
}
