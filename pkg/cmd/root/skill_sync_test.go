package root_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/build"
	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/skill"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/root"
)

// syncEnv is a ytrack built as version, with the background skill refresh on
// and home and state directories in a temp dir. It returns the home.
func syncEnv(t *testing.T, version string) (*cmdtest.Env, string) {
	t.Helper()
	old := build.Version
	build.Version = version
	t.Cleanup(func() { build.Version = old })
	t.Cleanup(root.SetSkillSync(true))
	e := cmdtest.New(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	return e, home
}

func installAt(t *testing.T, dir, version string) string {
	t.Helper()
	p, err := skill.Install(dir, version)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func copyVersion(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := skill.ParseMeta(b)
	if err != nil {
		t.Fatal(err)
	}
	return m.Metadata["version"]
}

// saveRegistry writes a registry stamped synced with dirs.
func saveRegistry(t *testing.T, synced string, dirs ...string) {
	t.Helper()
	reg, err := skill.OpenRegistry()
	if err != nil {
		t.Fatal(err)
	}
	reg.Synced, reg.Dirs = synced, dirs
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
}

func registryBytes(t *testing.T) []byte {
	t.Helper()
	path, err := skill.RegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	return b
}

func openRegistry(t *testing.T) *skill.Registry {
	t.Helper()
	reg, err := skill.OpenRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestSkillAdoption(t *testing.T) {
	t.Run("given no registry, any command records the ytrack copies in agent dirs and nothing foreign", func(t *testing.T) {
		e, home := syncEnv(t, "dev")
		claude := filepath.Join(home, ".claude", "skills")
		codex := filepath.Join(home, ".codex", "skills")
		installAt(t, claude, "0.4.0")
		installAt(t, codex, "0.3.2")
		installAt(t, filepath.Join(home, "notes"), "0.4.0") // not an agent dir
		foreign := filepath.Join(home, ".cursor", "skills", "ytrack", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(foreign), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(foreign, []byte("---\nname: mine\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		if code := e.Run("version"); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		if len(registryBytes(t)) == 0 {
			t.Fatal("no registry file written")
		}
		if got := openRegistry(t).Dirs; !slices.Equal(got, []string{claude, codex}) {
			t.Errorf("recorded %q, want %q", got, []string{claude, codex})
		}
		// A development build adopts but rewrites nothing.
		if v := copyVersion(t, filepath.Join(claude, "ytrack")); v != "0.4.0" {
			t.Errorf("claude copy rewritten to %s", v)
		}
	})
}

func TestSkillRefresh(t *testing.T) {
	t.Run("given copies synced to an older release, a newer release rewrites them and advances the stamp", func(t *testing.T) {
		e, home := syncEnv(t, "0.5.4")
		agents := filepath.Join(home, ".agents", "skills")
		claude := filepath.Join(home, ".claude", "skills")
		installAt(t, agents, "0.4.0")
		installAt(t, claude, "0.4.0")
		saveRegistry(t, "0.4.0", agents, claude)

		if code := e.Run("version"); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		for _, d := range []string{agents, claude} {
			if v := copyVersion(t, filepath.Join(d, "ytrack")); v != "0.5.4" {
				t.Errorf("%s at %s, want 0.5.4", d, v)
			}
		}
		if reg := openRegistry(t); reg.Synced != "0.5.4" {
			t.Errorf("synced = %q, want 0.5.4", reg.Synced)
		}
		if e.Stderr.Len() != 0 {
			t.Errorf("stderr = %q", e.Stderr)
		}
	})

	for name, c := range map[string]struct{ build, synced string }{
		"an equal stamp":             {"0.5.4", "0.5.4"},
		"a newer stamp":              {"0.5.4", "0.6.0"},
		"a development build":        {"dev", "0.4.0"},
		"a git describe build":       {"0.5.4-3-gabc1234", "0.4.0"},
		"a dirty git describe build": {"0.5.4-dirty", "0.4.0"},
	} {
		t.Run("given "+name+", it writes nothing", func(t *testing.T) {
			e, home := syncEnv(t, c.build)
			agents := filepath.Join(home, ".agents", "skills")
			installAt(t, agents, "0.4.0")
			saveRegistry(t, c.synced, agents)
			before := registryBytes(t)

			if code := e.Run("version"); code != 0 {
				t.Fatalf("exit %d: %s", code, e.Stderr)
			}
			if v := copyVersion(t, filepath.Join(agents, "ytrack")); v != "0.4.0" {
				t.Errorf("copy rewritten to %s", v)
			}
			if after := registryBytes(t); string(after) != string(before) {
				t.Errorf("registry changed:\n%s\n->\n%s", before, after)
			}
		})
	}

	t.Run("given a read-only recorded dir, the command's exit code and output are unchanged and the stamp stays", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permissions differ on Windows")
		}
		e, home := syncEnv(t, "0.5.4")
		baseline := cmdtest.New(t)
		restore := root.SetSkillSync(false)
		if code := baseline.Run("version"); code != 0 {
			t.Fatalf("baseline exit %d", code)
		}
		restore()

		ro := filepath.Join(home, ".agents", "skills")
		installAt(t, ro, "0.4.0")
		saveRegistry(t, "0.4.0", ro)
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o750) })
		if f, err := os.Create(filepath.Join(ro, "probe")); err == nil {
			_ = f.Close()
			t.Skip("running with permissions that ignore a read-only directory")
		}

		if code := e.Run("version"); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		if e.Stdout.String() != baseline.Stdout.String() || e.Stderr.String() != baseline.Stderr.String() {
			t.Errorf("output changed: stdout %q stderr %q, want %q %q", e.Stdout, e.Stderr, baseline.Stdout, baseline.Stderr)
		}
		if reg := openRegistry(t); reg.Synced != "0.4.0" || !slices.Equal(reg.Dirs, []string{ro}) {
			t.Errorf("registry = %+v", reg)
		}
	})

	t.Run("given a read-only state dir, the command's exit code and output are unchanged", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permissions differ on Windows")
		}
		e, home := syncEnv(t, "0.5.4")
		installAt(t, filepath.Join(home, ".claude", "skills"), "0.4.0")
		state := filepath.Join(home, ".local", "state")
		if err := os.MkdirAll(state, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(state, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(state, 0o750) })
		if code := e.Run("version"); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		if e.Stderr.Len() != 0 || !strings.HasPrefix(e.Stdout.String(), "ytrack version 0.5.4") {
			t.Errorf("stdout %q stderr %q", e.Stdout, e.Stderr)
		}
	})

	t.Run("given a recorded copy the user deleted, the refresh forgets it and does not re-create it", func(t *testing.T) {
		e, home := syncEnv(t, "0.5.4")
		agents := filepath.Join(home, ".agents", "skills")
		claude := filepath.Join(home, ".claude", "skills")
		installAt(t, agents, "0.4.0")
		deleted := installAt(t, claude, "0.4.0")
		saveRegistry(t, "0.4.0", agents, claude)
		if err := os.RemoveAll(deleted); err != nil {
			t.Fatal(err)
		}

		if code := e.Run("version"); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		if _, err := os.Lstat(deleted); err == nil {
			t.Error("the deleted copy was re-created")
		}
		if reg := openRegistry(t); !slices.Equal(reg.Dirs, []string{agents}) || reg.Synced != "0.5.4" {
			t.Errorf("registry = %+v", reg)
		}
		if e.Stderr.Len() != 0 {
			t.Errorf("stderr = %q", e.Stderr)
		}
	})

	t.Run("given a recorded copy in a repository, neither the refresh nor skill sync rewrites it", func(t *testing.T) {
		e, home := syncEnv(t, "0.5.4")
		agents := filepath.Join(home, ".agents", "skills")
		repo := filepath.Join(home, "src", "app")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		repoSkills := filepath.Join(repo, ".agents", "skills")
		installAt(t, agents, "0.4.0")
		repoCopy := installAt(t, repoSkills, "0.4.0")
		saveRegistry(t, "0.4.0", agents, repoSkills)

		if code := e.Run("version"); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		if code := e.Run("skill", "sync"); code != 0 {
			t.Fatalf("skill sync: exit %d: %s", code, e.Stderr)
		}
		if v := copyVersion(t, repoCopy); v != "0.4.0" {
			t.Errorf("the repository copy was rewritten to %s", v)
		}
		if v := copyVersion(t, filepath.Join(agents, "ytrack")); v != "0.5.4" {
			t.Errorf("the home copy is at %s, want 0.5.4", v)
		}
		if reg := openRegistry(t); !slices.Equal(reg.Dirs, []string{agents, repoSkills}) {
			t.Errorf("recorded %q", reg.Dirs)
		}
	})

	t.Run("given 20 concurrent runs needing a refresh, all exit 0 and leave complete copies and no temp dirs", func(t *testing.T) {
		_, home := syncEnv(t, "0.5.4")
		agents := filepath.Join(home, ".agents", "skills")
		claude := filepath.Join(home, ".claude", "skills")
		installAt(t, agents, "0.4.0")
		installAt(t, claude, "0.4.0")
		saveRegistry(t, "0.4.0", agents, claude)
		envs := make([]*cmdtest.Env, 20)
		for i := range envs {
			envs[i] = cmdtest.New(t)
		}

		var wg sync.WaitGroup
		codes := make([]int, len(envs))
		for i, e := range envs {
			wg.Go(func() { codes[i] = int(e.Run("version")) })
		}
		wg.Wait()

		for i, code := range codes {
			if code != 0 {
				t.Errorf("run %d: exit %d: %s", i, code, envs[i].Stderr)
			}
		}
		for _, d := range []string{agents, claude} {
			assertCompleteCopy(t, filepath.Join(d, "ytrack"), "0.5.4")
			entries, err := os.ReadDir(d)
			if err != nil {
				t.Fatal(err)
			}
			for _, en := range entries {
				if strings.HasPrefix(en.Name(), ".ytrack-skill-") {
					t.Errorf("left %s in %s", en.Name(), d)
				}
			}
		}
		if reg := openRegistry(t); reg.Synced != "0.5.4" {
			t.Errorf("synced = %q", reg.Synced)
		}
	})
}

// assertCompleteCopy fails unless dir holds every file of the embedded
// skill, stamped with version.
func assertCompleteCopy(t *testing.T, dir, version string) {
	t.Helper()
	if v := copyVersion(t, dir); v != version {
		t.Errorf("%s at %s, want %s", dir, v, version)
	}
	err := fs.WalkDir(skill.Files(), ".", func(p string, _ fs.DirEntry, err error) error {
		if err == nil {
			if _, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); statErr != nil {
				t.Errorf("missing %s: %v", p, statErr)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
