package cmdutil_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

// tempDir is t.TempDir with symbolic links resolved, so the paths git writes
// into a worktree's files are the ones the test's globs name.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// linkedWorktree makes a repository at <tmp>/repo and a linked worktree of
// it at <tmp>/elsewhere/wt, outside the repository's folder.
func linkedWorktree(t *testing.T) (repo, wt string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base := tempDir(t)
	repo, wt = filepath.Join(base, "repo"), filepath.Join(base, "elsewhere", "wt")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", repo)
	git("-C", repo, "commit", "-q", "--allow-empty", "-m", "init")
	git("-C", repo, "worktree", "add", "-q", wt)
	return repo, wt
}

func inFolder(env *cmdtest.Env, dir string) {
	env.Factory.WorkDir = func() (string, error) { return dir, nil }
}

func mustRun(t *testing.T, env *cmdtest.Env, args ...string) {
	t.Helper()
	if code := env.Run(args...); code != 0 {
		t.Fatalf("ytrack %v: exit %d\n%s", args, code, env.Stderr)
	}
}

// active runs `auth status --json` and returns the active host's row.
func active(t *testing.T, env *cmdtest.Env) map[string]any {
	t.Helper()
	env.Reset()
	mustRun(t, env, "auth", "status", "--json", "host,active,resolvedBy,matchedPath")
	var rows []map[string]any
	if err := json.Unmarshal(env.Stdout.Bytes(), &rows); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, env.Stdout)
	}
	for _, r := range rows {
		if r["active"] == true {
			return r
		}
	}
	t.Fatalf("no active host: %s", env.Stdout)
	return nil
}

func TestHostByWorktreeMainCheckout(t *testing.T) {
	t.Run("given a linked worktree outside the folder whose main checkout matches work's path, work is active by path (main checkout)", func(t *testing.T) {
		repo, wt := linkedWorktree(t)
		env, _, work := cmdtest.TwoHosts(t)
		glob := repo + "/**"
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", glob)
		sub := filepath.Join(wt, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{wt, sub} {
			inFolder(env, dir)
			if r := active(t, env); r["host"] != work.Key() || r["resolvedBy"] != "path (main checkout)" || r["matchedPath"] != glob {
				t.Errorf("in %s: active = %v", dir, r)
			}
		}
		env.Reset()
		mustRun(t, env, "auth", "status")
		if want := "Active host: true (this worktree's main checkout matches " + glob + ")"; !strings.Contains(env.Stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, env.Stdout)
		}
	})

	t.Run("given the main checkout itself, the host is active by path as before", func(t *testing.T) {
		repo, _ := linkedWorktree(t)
		env, _, work := cmdtest.TwoHosts(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", repo+"/**")
		inFolder(env, repo)
		if r := active(t, env); r["host"] != work.Key() || r["resolvedBy"] != "path" {
			t.Errorf("active = %v", r)
		}
	})

	t.Run("given a worktree whose own folder matches a path, that path wins over the main checkout's", func(t *testing.T) {
		repo, wt := linkedWorktree(t)
		env, personal, work := cmdtest.TwoHosts(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", repo+"/**")
		mustRun(t, env, "auth", "paths", "--host", personal.URL(), "--add", filepath.Dir(wt)+"/**")
		inFolder(env, wt)
		if r := active(t, env); r["host"] != personal.Key() || r["resolvedBy"] != "path" {
			t.Errorf("active = %v", r)
		}
	})

	t.Run("given a folder outside any git repository that matches no path, the default host is active", func(t *testing.T) {
		env, personal, work := cmdtest.TwoHosts(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		inFolder(env, tempDir(t))
		if r := active(t, env); r["host"] != personal.Key() || r["resolvedBy"] != "default" {
			t.Errorf("active = %v", r)
		}
	})

	t.Run("given a worktree whose .git file and commondir use relative paths, no git binary is needed", func(t *testing.T) {
		base := tempDir(t)
		repo, wt := filepath.Join(base, "repo"), filepath.Join(base, "elsewhere", "wt")
		gitdir := filepath.Join(repo, ".git", "worktrees", "wt")
		for _, d := range []string{gitdir, wt} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, filepath.Join(wt, ".git"), "gitdir: ../../repo/.git/worktrees/wt\n")
		writeFile(t, filepath.Join(gitdir, "commondir"), "../..\n")
		env, _, work := cmdtest.TwoHosts(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", repo+"/**")
		inFolder(env, wt)
		if r := active(t, env); r["host"] != work.Key() || r["resolvedBy"] != "path (main checkout)" {
			t.Errorf("active = %v", r)
		}
	})

	t.Run("given a submodule-style .git file without commondir, the default host is active", func(t *testing.T) {
		base := tempDir(t)
		repo, sub := filepath.Join(base, "repo"), filepath.Join(base, "elsewhere", "sub")
		gitdir := filepath.Join(repo, ".git", "modules", "sub")
		for _, d := range []string{gitdir, sub} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, filepath.Join(sub, ".git"), "gitdir: "+gitdir+"\n")
		env, personal, work := cmdtest.TwoHosts(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", repo+"/**")
		inFolder(env, sub)
		if r := active(t, env); r["host"] != personal.Key() || r["resolvedBy"] != "default" {
			t.Errorf("active = %v", r)
		}
	})
}

func TestIssueKeyRescueInWorktree(t *testing.T) {
	// rescueSetup puts NSR-40 on work only and ties personal (which has only
	// OWN) to the main checkout of a worktree the command runs in.
	rescueSetup := func(t *testing.T) (env *cmdtest.Env, personal, work *cmdtest.FakeYouTrack, glob string) {
		t.Helper()
		repo, wt := linkedWorktree(t)
		env, personal, work = cmdtest.TwoHosts(t)
		mustRun(t, env, "auth", "switch", "--host", work.URL())
		personal.AddProject("OWN")
		work.SeedSampleIssues()
		work.SeedSampleProjects()
		glob = repo + "/**"
		mustRun(t, env, "auth", "paths", "--host", personal.URL(), "--add", glob)
		inFolder(env, wt)
		env.Reset()
		return env, personal, work, glob
	}

	t.Run("given the main checkout's rule picks a host without the key's project, view uses the one host that has it", func(t *testing.T) {
		env, personal, work, _ := rescueSetup(t)
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable")
		if want := `{"idReadable":"NSR-40"}` + "\n"; env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
		if want := "using " + work.Key() + " for NSR-* (the folder rule picked " + personal.Key() + ")\n"; env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})

	t.Run("given the key's project is on no host, the error says the main checkout's rule chose the host", func(t *testing.T) {
		env, personal, _, glob := rescueSetup(t)
		if code := env.Run("issue", "view", "ZZZ-1"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		want := "ytrack: issue ZZZ-1 not found on " + personal.Key() + " (or you cannot see it); " + personal.Key() +
			" chosen by the folder rule " + glob + " (this worktree's main checkout)\n"
		if env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
