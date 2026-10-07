package root_test

import (
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func TestUsageErrorsExit2(t *testing.T) {
	for name, args := range map[string][]string{
		"an unknown command":            {"bogus"},
		"an unknown subcommand of auth": {"auth", "bogus"},
		"an unknown flag":               {"api", "/issues", "--bogus"},
		"an argument to version":        {"version", "extra"},
	} {
		t.Run("given "+name+", it exits 1 and points to --help", func(t *testing.T) {
			env := cmdtest.New(t)
			if code := env.Run(args...); !env.IsUsageError(code) {
				t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
			}
			if !strings.Contains(env.Stderr.String(), "--help' for usage") {
				t.Errorf("stderr = %q", env.Stderr)
			}
			if env.Stdout.Len() != 0 {
				t.Errorf("stdout = %q", env.Stdout)
			}
		})
	}
}

func TestHelpNeedsNoNetworkOrLogin(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"auth"}, {"api", "--help"}, {"completion", "zsh"}, {"version"}} {
		t.Run(strings.Join(args, " ")+" exits 0", func(t *testing.T) {
			env := cmdtest.New(t)
			if code := env.Run(args...); code != 0 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			if env.Stdout.Len() == 0 {
				t.Error("printed nothing")
			}
		})
	}
}
