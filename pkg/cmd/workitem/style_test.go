package workitem_test

import (
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func TestWorkItemListStyling(t *testing.T) {
	t.Setenv("YTRACK_NO_UPDATE_NOTIFIER", "1")
	for _, color := range []bool{true, false} {
		name := map[bool]string{true: "styled", false: "plain"}[color]
		t.Run("given a "+name+" terminal, work-item list is pinned", func(t *testing.T) {
			env, _ := loggedIn(t)
			env.IO.SetStdoutTTY(true)
			env.IO.SetStderrTTY(true)
			env.IO.SetColorEnabled(color)
			env.IO.SetErrColorEnabled(color)
			env.Reset()
			if code := env.Run("work-item", "list", "NSR-40"); code != 0 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			cmdtest.Golden(t, "list-"+name, env.Stdout.String()+"--- stderr\n"+env.Stderr.String())
		})
	}

	t.Run("given NO_COLOR semantics (color off) on a terminal, there is no escape", func(t *testing.T) {
		env, _ := loggedIn(t)
		env.IO.SetStdoutTTY(true)
		env.Reset()
		if code := env.Run("work-item", "list", "NSR-40"); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if strings.Contains(env.Stdout.String(), "\x1b") {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})
}
