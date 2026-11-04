package root_test

import (
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

// styled turns on a color terminal for stdout and stderr.
func styled(env *cmdtest.Env) {
	env.IO.SetStdoutTTY(true)
	env.IO.SetStderrTTY(true)
	env.IO.SetColorEnabled(true)
	env.IO.SetErrColorEnabled(true)
}

func TestHelpStyling(t *testing.T) {
	t.Setenv("YTRACK_NO_UPDATE_NOTIFIER", "1")
	for _, args := range [][]string{{"--help"}, {"issue", "--help"}, {"work-item", "add", "--help"}} {
		name := strings.Join(args[:len(args)-1], "-")
		if name == "" {
			name = "root"
		}
		t.Run("given a color terminal, "+strings.Join(args, " ")+" styles headings, commands, flags and examples", func(t *testing.T) {
			env := cmdtest.New(t)
			styled(env)
			if code := env.Run(args...); code != 0 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			cmdtest.Golden(t, "help-"+name+"-styled", env.Stdout.String())
		})
		t.Run("given no color, "+strings.Join(args, " ")+" is cobra's plain layout", func(t *testing.T) {
			env := cmdtest.New(t)
			if code := env.Run(args...); code != 0 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			cmdtest.Golden(t, "help-"+name+"-plain", env.Stdout.String())
		})
	}

	off := map[string]func(t *testing.T, env *cmdtest.Env) []string{
		"--no-color": func(*testing.T, *cmdtest.Env) []string { return []string{"--no-color", "--help"} },
		"--json": func(*testing.T, *cmdtest.Env) []string {
			return []string{"issue", "list", "--json", "idReadable", "--help"}
		},
	}
	for name, setup := range off {
		t.Run("given "+name+" on a color terminal, help and errors have no ANSI escape", func(t *testing.T) {
			env := cmdtest.New(t)
			styled(env)
			env.Run(setup(t, env)...)
			if out := env.Stdout.String() + env.Stderr.String(); strings.Contains(out, "\x1b") {
				t.Errorf("output has an escape: %q", out)
			}
		})
	}
}

func TestErrorStyling(t *testing.T) {
	t.Setenv("YTRACK_NO_UPDATE_NOTIFIER", "1")
	t.Run("given a color terminal, the error is red and the unblock command bold", func(t *testing.T) {
		env := cmdtest.New(t)
		styled(env)
		env.Run("issue", "view", "NOT AN ID")
		cmdtest.Golden(t, "error-usage-styled", env.Stderr.String())
	})
	t.Run("given a pipe, the error is plain", func(t *testing.T) {
		env := cmdtest.New(t)
		env.Run("issue", "view", "NOT AN ID")
		cmdtest.Golden(t, "error-usage-plain", env.Stderr.String())
	})
	t.Run("given a color terminal and --no-color, the error is plain", func(t *testing.T) {
		env := cmdtest.New(t)
		styled(env)
		env.Run("issue", "view", "NOT AN ID", "--no-color")
		if strings.Contains(env.Stderr.String(), "\x1b") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})
}
