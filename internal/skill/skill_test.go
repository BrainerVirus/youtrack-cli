package skill_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/skill"
	"github.com/BrainerVirus/youtrack-cli/skills"
)

// The Agent Skills spec: a lowercase name of letters, digits and hyphens that
// equals the directory name, and a description of at most 1024 characters.
var specName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestEmbeddedSkillLayout(t *testing.T) {
	t.Run("given skills/ytrack/SKILL.md, its frontmatter is valid and names its directory", func(t *testing.T) {
		b, err := fs.ReadFile(skills.FS, "ytrack/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		m, err := skill.ParseMeta(b)
		if err != nil {
			t.Fatalf("frontmatter: %v", err)
		}
		if m.Name != "ytrack" || !specName.MatchString(m.Name) {
			t.Errorf("name = %q, want ytrack (the directory name)", m.Name)
		}
		if d := strings.TrimSpace(m.Description); d == "" || len(d) > 1024 {
			t.Errorf("description has %d characters, want 1 to 1024", len(d))
		}
		if m.Metadata["version"] == "" {
			t.Error("metadata.version is missing")
		}
	})

	t.Run("given the skill's markdown links, every relative one resolves inside the skill", func(t *testing.T) {
		link := regexp.MustCompile(`\]\(([^)#]+)\)`)
		err := fs.WalkDir(skill.Files(), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(skill.Files(), p)
			if err != nil {
				return err
			}
			for _, m := range link.FindAllStringSubmatch(string(b), -1) {
				target := m[1]
				if strings.Contains(target, "://") {
					continue
				}
				if _, err := fs.Stat(skill.Files(), filepath.ToSlash(filepath.Join(filepath.Dir(p), target))); err != nil {
					t.Errorf("%s links to %s, which is not in the skill", p, target)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestRender(t *testing.T) {
	t.Run("given a version, it stamps metadata.version and keeps the rest of SKILL.md", func(t *testing.T) {
		got, err := skill.Render("1.2.3")
		if err != nil {
			t.Fatal(err)
		}
		m, err := skill.ParseMeta(got)
		if err != nil {
			t.Fatal(err)
		}
		if m.Metadata["version"] != "1.2.3" || m.Name != "ytrack" {
			t.Errorf("meta = %+v", m)
		}
		orig, _ := fs.ReadFile(skill.Files(), "SKILL.md")
		if body := strings.SplitN(string(orig), "\n---", 2)[1]; !strings.HasSuffix(string(got), body) {
			t.Error("the body after the frontmatter changed")
		}
	})
}

func readMeta(t *testing.T, dir string) skill.Meta {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := skill.ParseMeta(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestInstall(t *testing.T) {
	t.Run("given an empty root, it writes SKILL.md and the references", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "skills") // created by Install
		dir, err := skill.Install(root, "1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(root, "ytrack"); dir != want {
			t.Errorf("dir = %q, want %q", dir, want)
		}
		if v := readMeta(t, dir).Metadata["version"]; v != "1.0.0" {
			t.Errorf("version = %q", v)
		}
		for _, ref := range []string{"queries.md", "commands.md", "api.md"} {
			if _, err := os.Stat(filepath.Join(dir, "references", ref)); err != nil {
				t.Errorf("references/%s: %v", ref, err)
			}
		}
		assertNoStaging(t, root)
	})

	t.Run("given an earlier install, it replaces it and drops stale files", func(t *testing.T) {
		root := t.TempDir()
		dir, err := skill.Install(root, "1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		stale := filepath.Join(dir, "references", "old.md")
		if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := skill.Install(root, "1.1.0"); err != nil {
			t.Fatal(err)
		}
		if v := readMeta(t, dir).Metadata["version"]; v != "1.1.0" {
			t.Errorf("version = %q, want 1.1.0", v)
		}
		if _, err := os.Stat(stale); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("stale file survived: %v", err)
		}
		assertNoStaging(t, root)
	})

	foreign := map[string]func(t *testing.T, dir string){
		"a SKILL.md named otherwise": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: other\ndescription: x\n---\nmine\n")
		},
		"no SKILL.md": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "notes.txt"), "mine\n")
		},
		"a SKILL.md without frontmatter": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "SKILL.md"), "name: ytrack\n")
		},
	}
	for name, setup := range foreign {
		t.Run("given a ytrack directory with "+name+", it refuses and leaves it untouched", func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "ytrack")
			if err := os.Mkdir(dir, 0o750); err != nil {
				t.Fatal(err)
			}
			setup(t, dir)
			before := snapshot(t, dir)
			_, err := skill.Install(root, "1.0.0")
			if _, ok := errors.AsType[*skill.ForeignError](err); !ok {
				t.Fatalf("err = %v, want a ForeignError", err)
			}
			if after := snapshot(t, dir); after != before {
				t.Errorf("directory changed:\nbefore %s\nafter  %s", before, after)
			}
			assertNoStaging(t, root)
		})
	}

	t.Run("given a file named ytrack, it refuses", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "ytrack"), "a file\n")
		if _, err := skill.Install(root, "1.0.0"); !errors.As(err, new(*skill.ForeignError)) {
			t.Fatalf("err = %v, want a ForeignError", err)
		}
	})

	t.Run("given ytrack as a symlink to an installed skill, it updates the target and keeps the link", func(t *testing.T) {
		base := t.TempDir()
		canonical := filepath.Join(base, "agents")
		target, err := skill.Install(canonical, "1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(base, "claude")
		if err := os.Mkdir(root, 0o750); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "ytrack")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := skill.Install(root, "1.1.0"); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Lstat(link); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			t.Fatalf("the link was replaced: %v", err)
		}
		if v := readMeta(t, target).Metadata["version"]; v != "1.1.0" {
			t.Errorf("target version = %q, want 1.1.0", v)
		}
	})
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// snapshot lists dir's files and contents.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		b.WriteString(p + "\n")
		if !d.IsDir() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			b.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// assertNoStaging fails when Install left a temporary directory in root.
func assertNoStaging(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ytrack-skill-") {
			t.Errorf("left %s behind", e.Name())
		}
	}
}
