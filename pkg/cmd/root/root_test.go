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

// Weak models guess flags (skill bench, help-first): a usage error names the
// flag they were after, so one retry fixes it.
func TestUsageErrorsNameTheIntendedFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		hint string
	}{
		"an unknown flag close to a real one":       {[]string{"issue", "view", "APP-1", "--comments", "--limit", "12"}, "did you mean --comments-limit?"},
		"a typo":                                    {[]string{"issue", "list", "--limt", "5"}, "did you mean --limit?"},
		"a number after a flag that takes no value": {[]string{"issue", "view", "APP-1", "--comments", "12"}, "--comments takes no value; for a number use --comments-limit 12"},
	} {
		t.Run("given "+name+", it names the flag", func(t *testing.T) {
			env := cmdtest.New(t)
			if code := env.Run(tc.args...); !env.IsUsageError(code) {
				t.Fatalf("exit %d, want a usage error; stderr: %s", code, env.Stderr)
			}
			if !strings.Contains(env.Stderr.String(), tc.hint) {
				t.Errorf("stderr %q lacks %q", env.Stderr, tc.hint)
			}
		})
	}

	t.Run("given an unknown flag with nothing close, it adds no guess", func(t *testing.T) {
		env := cmdtest.New(t)
		env.Run("issue", "view", "APP-1", "--zzzzqq")
		if strings.Contains(env.Stderr.String(), "did you mean") {
			t.Errorf("stderr %q guesses", env.Stderr)
		}
	})
}
