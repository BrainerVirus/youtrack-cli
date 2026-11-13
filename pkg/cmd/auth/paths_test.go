package auth_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
)

// twoHosts logs in to personal (the default host, logged in first) and work.
func twoHosts(t *testing.T) (env *cmdtest.Env, personal, work *cmdtest.FakeYouTrack) {
	t.Helper()
	env = cmdtest.New(t)
	personal = cmdtest.NewFakeYouTrack(t, "")
	work = cmdtest.NewFakeYouTrack(t, "")
	env.Login(personal)
	env.Login(work)
	return env, personal, work
}

func inFolder(env *cmdtest.Env, dir string) {
	env.Factory.WorkDir = func() (string, error) { return dir, nil }
}

func TestAuthPaths(t *testing.T) {
	t.Run("given --add, the glob is stored in hosts.yml and listed", func(t *testing.T) {
		env, personal, work := twoHosts(t)
		if code := env.Run("auth", "paths", "--host", work.URL(), "--add", "/w/**", "--add", "~/client/**"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got := env.Config().HostPaths(work.Key()); strings.Join(got, " ") != "/w/** ~/client/**" {
			t.Errorf("stored paths = %v", got)
		}
		env.Reset()
		if code := env.Run("auth", "paths"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		want := personal.Key() + "\n  (no paths)\n" + work.Key() + "\n  /w/**\n  ~/client/**\n"
		if personal.Key() > work.Key() {
			want = work.Key() + "\n  /w/**\n  ~/client/**\n" + personal.Key() + "\n  (no paths)\n"
		}
		if env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("given --json host,paths, it prints each host's globs", func(t *testing.T) {
		env, _, work := twoHosts(t)
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		env.Reset()
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--json", "host,paths")
		var got []map[string]any
		if err := json.Unmarshal(env.Stdout.Bytes(), &got); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, env.Stdout)
		}
		if len(got) != 1 || got[0]["host"] != work.Key() || len(got[0]["paths"].([]any)) != 1 || got[0]["paths"].([]any)[0] != "/w/**" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("given --remove, the glob is dropped; an absent one only warns", func(t *testing.T) {
		env, _, work := twoHosts(t)
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--remove", "/w/**", "--remove", "/nope/**")
		if got := env.Config().HostPaths(work.Key()); len(got) != 0 {
			t.Errorf("paths = %v", got)
		}
		if !strings.Contains(env.Stderr.String(), "/nope/** was not a path of") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a host that is not logged in, it is a usage error and saves nothing", func(t *testing.T) {
		env, _, _ := twoHosts(t)
		if code := env.Run("auth", "paths", "--host", "nope.example", "--add", "/w/**"); !env.IsUsageError(code) {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given two logged-in hosts and no --host, an edit is a usage error", func(t *testing.T) {
		env, _, _ := twoHosts(t)
		if code := env.Run("auth", "paths", "--add", "/w/**"); !env.IsUsageError(code) || !strings.Contains(env.Stderr.String(), "--host is required") {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
	})

	for goos, fold := range map[string]bool{"darwin": true, "windows": true, "linux": false} {
		t.Run("given "+goos+" and ~/Work/** stored, --add ~/work/** is already present only where case does not matter", func(t *testing.T) {
			t.Cleanup(config.SetPathOS(goos))
			env, _, work := twoHosts(t)
			mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "~/Work/**")
			env.Reset()
			mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "~/work/**")
			reported := strings.Contains(env.Stderr.String(), "~/work/** is already a path of "+work.Key()+", as ~/Work/**")
			if reported != fold {
				t.Errorf("reported %v, want %v; stderr = %q", reported, fold, env.Stderr)
			}
			want := 2
			if fold {
				want = 1
			}
			if got := env.Config().HostPaths(work.Key()); len(got) != want {
				t.Errorf("paths = %v", got)
			}
		})
	}

	t.Run("given an exact duplicate, --add reports it as already present", func(t *testing.T) {
		env, _, work := twoHosts(t)
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		env.Reset()
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		if !strings.Contains(env.Stderr.String(), "/w/** is already a path of "+work.Key()+"\n") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given macOS and globs of two hosts differing only by case, resolving the host fails naming both", func(t *testing.T) {
		t.Cleanup(config.SetPathOS("darwin"))
		env, personal, work := twoHosts(t)
		mustRunAuth(t, env, "auth", "paths", "--host", personal.URL(), "--add", "/w/Code/**")
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/code/**")
		inFolder(env, "/w/code/repo")
		env.Reset()
		if code := env.Run("auth", "token"); code == 0 {
			t.Fatalf("exit 0; stdout %q", env.Stdout)
		}
		for _, s := range []string{"/w/Code/**", "/w/code/**", personal.Key(), work.Key()} {
			if !strings.Contains(env.Stderr.String(), s) {
				t.Errorf("stderr lacks %q: %s", s, env.Stderr)
			}
		}
	})

	t.Run("given a relative glob, it is a usage error", func(t *testing.T) {
		env, _, work := twoHosts(t)
		if code := env.Run("auth", "paths", "--host", work.URL(), "--add", "w/**"); !env.IsUsageError(code) {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got := env.Config().HostPaths(work.Key()); len(got) != 0 {
			t.Errorf("paths = %v", got)
		}
	})
}

func TestStatusHostResolution(t *testing.T) {
	status := func(t *testing.T, env *cmdtest.Env) map[string]map[string]any {
		t.Helper()
		env.Reset()
		mustRunAuth(t, env, "auth", "status", "--json", "host,active,resolvedBy,matchedPath,paths")
		var rows []map[string]any
		if err := json.Unmarshal(env.Stdout.Bytes(), &rows); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, env.Stdout)
		}
		out := map[string]map[string]any{}
		for _, r := range rows {
			out[r["host"].(string)] = r
		}
		return out
	}

	t.Run("given a folder matching a host's path, status marks that host active by path", func(t *testing.T) {
		env, personal, work := twoHosts(t)
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		inFolder(env, "/w/repo")
		got := status(t, env)
		if w := got[work.Key()]; w["active"] != true || w["resolvedBy"] != "path" || w["matchedPath"] != "/w/**" || len(w["paths"].([]any)) != 1 {
			t.Errorf("work = %v", w)
		}
		if p := got[personal.Key()]; p["active"] != false || p["resolvedBy"] != "" {
			t.Errorf("personal = %v", p)
		}
		env.Reset()
		mustRunAuth(t, env, "auth", "status")
		for _, want := range []string{"Active host: true (current folder matches /w/**)", "Paths: /w/**", "Active host: false"} {
			if !strings.Contains(env.Stdout.String(), want) {
				t.Errorf("stdout lacks %q:\n%s", want, env.Stdout)
			}
		}
	})

	t.Run("given a folder outside every path, status marks the default host active by default", func(t *testing.T) {
		env, personal, work := twoHosts(t)
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		inFolder(env, "/elsewhere")
		if p := status(t, env)[personal.Key()]; p["active"] != true || p["resolvedBy"] != "default" {
			t.Errorf("personal = %v", p)
		}
	})

	t.Run("given YTRACK_HOST, it wins over a matching path", func(t *testing.T) {
		env, personal, work := twoHosts(t)
		mustRunAuth(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		inFolder(env, "/w/repo")
		t.Setenv("YTRACK_HOST", personal.URL())
		if p := status(t, env)[personal.Key()]; p["active"] != true || p["resolvedBy"] != "env" {
			t.Errorf("personal = %v", p)
		}
	})
}

func mustRunAuth(t *testing.T, env *cmdtest.Env, args ...string) {
	t.Helper()
	if code := env.Run(args...); code != 0 {
		t.Fatalf("ytrack %s: exit %d: %s", strings.Join(args, " "), code, env.Stderr)
	}
}
