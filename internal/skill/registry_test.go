package skill_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/skill"
)

// isolateHome points the home and state directories at a temp dir, so a
// test never reads or writes the real ones.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	return home
}

func install(t *testing.T, root, version string) string {
	t.Helper()
	dir, err := skill.Install(root, version)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func open(t *testing.T) *skill.Registry {
	t.Helper()
	reg, err := skill.OpenRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestOpenRegistryAdopts(t *testing.T) {
	t.Run("given no registry, it adopts the ytrack copies in home-level skills dirs and nothing foreign", func(t *testing.T) {
		home := isolateHome(t)
		claude := filepath.Join(home, ".claude", "skills")
		codex := filepath.Join(home, ".codex", "skills")
		install(t, claude, "0.4.0")
		install(t, codex, "0.3.2")
		foreign := filepath.Join(home, ".cursor", "skills", "ytrack")
		if err := os.MkdirAll(foreign, 0o750); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(foreign, "SKILL.md"), "---\nname: someone-else\n---\n")
		install(t, filepath.Join(home, "elsewhere"), "0.4.0") // not a known agent dir

		reg := open(t)
		if want := []string{claude, codex}; !slices.Equal(reg.Dirs, want) {
			t.Errorf("dirs = %q, want %q", reg.Dirs, want)
		}
		if !reg.Dirty() {
			t.Error("adopted copies are not marked for saving")
		}
	})

	t.Run("given a registry file, it does not adopt again", func(t *testing.T) {
		home := isolateHome(t)
		reg := open(t)
		reg.Record(filepath.Join(home, "repo-skills"))
		if err := reg.Save(); err != nil {
			t.Fatal(err)
		}
		install(t, filepath.Join(home, ".claude", "skills"), "0.4.0")
		if got := open(t).Dirs; !slices.Equal(got, []string{filepath.Join(home, "repo-skills")}) {
			t.Errorf("dirs = %q", got)
		}
	})
}

func TestRegistryRecord(t *testing.T) {
	t.Run("given two dirs symlinked to one copy, it records the copy once, by the dir that holds it", func(t *testing.T) {
		home := isolateHome(t)
		agents := filepath.Join(home, ".agents", "skills")
		target := install(t, agents, "0.4.0")
		var links []string
		for _, d := range []string{".claude/skills", ".cursor/skills"} {
			root := filepath.Join(home, filepath.FromSlash(d))
			if err := os.MkdirAll(root, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(root, "ytrack")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			links = append(links, root)
		}
		reg := open(t) // adopts all three
		if !slices.Equal(reg.Dirs, []string{agents}) {
			t.Errorf("adopted %q, want only %q", reg.Dirs, agents)
		}

		reg = &skill.Registry{}
		reg.Record(links[0])
		reg.Record(links[1])
		reg.Record(agents)
		if !slices.Equal(reg.Dirs, []string{agents}) {
			t.Errorf("recorded %q, want only %q", reg.Dirs, agents)
		}
	})

	t.Run("given a relative dir, it records it absolute", func(t *testing.T) {
		t.Chdir(t.TempDir())
		reg := &skill.Registry{}
		reg.Record(".claude/skills")
		wd, _ := os.Getwd()
		if want := filepath.Join(wd, ".claude", "skills"); !slices.Equal(reg.Dirs, []string{want}) {
			t.Errorf("dirs = %q, want %q", reg.Dirs, want)
		}
	})
}

func TestManaged(t *testing.T) {
	home := isolateHome(t)
	repo := filepath.Join(home, "src", "app")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	// A home-level agent dir stays managed even when it is versioned itself.
	if err := os.MkdirAll(filepath.Join(home, ".claude", ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]bool{
		filepath.Join(home, ".claude", "skills"):           true,
		filepath.Join(home, ".pi", "agent", "skills"):      true,
		filepath.Join(home, "my-skills"):                   true,
		filepath.Join(repo, ".claude", "skills"):           false,
		filepath.Join(repo, "nested", ".agents", "skills"): false,
		filepath.Join(t.TempDir(), "skills"):               false, // outside home
	} {
		if got := skill.Managed(dir); got != want {
			t.Errorf("Managed(%s) = %v, want %v", dir, got, want)
		}
	}
}

func TestRegistrySync(t *testing.T) {
	t.Run("given stale, deleted and repository copies, it rewrites the home ones, drops the deleted and leaves the repository", func(t *testing.T) {
		home := isolateHome(t)
		agents := filepath.Join(home, ".agents", "skills")
		claude := filepath.Join(home, ".claude", "skills")
		repo := filepath.Join(home, "src", "app")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		repoSkills := filepath.Join(repo, ".claude", "skills")
		install(t, agents, "0.4.0")
		deleted := install(t, claude, "0.4.0")
		repoCopy := install(t, repoSkills, "0.4.0")
		reg := &skill.Registry{Synced: "0.4.0", Dirs: []string{agents, claude, repoSkills}}
		if err := os.RemoveAll(deleted); err != nil {
			t.Fatal(err)
		}

		results, ok := reg.Sync("0.5.4")
		if !ok {
			t.Fatalf("not ok: %+v", results)
		}
		actions := map[string]string{}
		for _, r := range results {
			actions[r.Dir] = r.Action
		}
		want := map[string]string{agents: skill.SyncUpdated, claude: skill.SyncDropped, repoSkills: skill.SyncManual}
		for d, a := range want {
			if actions[d] != a {
				t.Errorf("%s: %q, want %q", d, actions[d], a)
			}
		}
		assertComplete(t, filepath.Join(agents, "ytrack"), "0.5.4")
		if _, err := os.Lstat(deleted); err == nil {
			t.Error("the deleted copy was re-created")
		}
		if v := readMeta(t, repoCopy).Metadata["version"]; v != "0.4.0" {
			t.Errorf("the repository copy was rewritten to %s", v)
		}
		if !slices.Equal(reg.Dirs, []string{agents, repoSkills}) || reg.Synced != "0.5.4" {
			t.Errorf("registry = %+v", reg)
		}
	})

	t.Run("given a read-only dir, it fails that copy and keeps the synced version", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permissions differ on Windows")
		}
		home := isolateHome(t)
		ro := filepath.Join(home, ".agents", "skills")
		install(t, ro, "0.4.0")
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o750) })
		if f, err := os.Create(filepath.Join(ro, "probe")); err == nil {
			_ = f.Close()
			t.Skip("running with permissions that ignore a read-only directory")
		}
		reg := &skill.Registry{Synced: "0.4.0", Dirs: []string{ro}}
		results, ok := reg.Sync("0.5.4")
		if ok || len(results) != 1 || results[0].Action != skill.SyncFailed {
			t.Fatalf("ok %v, results %+v", ok, results)
		}
		if reg.Synced != "0.4.0" || !slices.Equal(reg.Dirs, []string{ro}) {
			t.Errorf("registry = %+v", reg)
		}
	})
}

func TestRegistryLock(t *testing.T) {
	t.Run("given the lock held, a second TryLock fails until it is released", func(t *testing.T) {
		isolateHome(t)
		unlock, ok := skill.TryLock()
		if !ok {
			t.Fatal("first TryLock failed")
		}
		if _, ok := skill.TryLock(); ok {
			t.Fatal("second TryLock succeeded while the lock is held")
		}
		unlock()
		unlock2, ok := skill.TryLock()
		if !ok {
			t.Fatal("TryLock after release failed")
		}
		unlock2()
	})

	t.Run("given a lock left by a killed process, it takes it", func(t *testing.T) {
		isolateHome(t)
		unlock, ok := skill.TryLock()
		if !ok {
			t.Fatal("TryLock failed")
		}
		_ = unlock // never released, as by a killed process
		path, err := skill.RegistryPath()
		if err != nil {
			t.Fatal(err)
		}
		lock := filepath.Join(filepath.Dir(path), "skills.lock")
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(lock, old, old); err != nil {
			t.Fatal(err)
		}
		unlock, ok = skill.TryLock()
		if !ok {
			t.Fatal("the stale lock was not taken")
		}
		unlock()
	})
}
