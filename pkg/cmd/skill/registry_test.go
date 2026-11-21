package skill_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/skill"
)

// copies is a home with copies recorded in ~/.agents/skills (stale),
// ~/.claude/skills (current), ~/.cursor/skills (then deleted) and a
// repository's .claude/skills (stale), built as version 1.2.3.
type copies struct {
	e                              *cmdtest.Env
	agents, claude, cursor, repoSk string
}

func setupCopies(t *testing.T) copies {
	t.Helper()
	e := env(t)
	home, _ := os.UserHomeDir()
	c := copies{
		e:      e,
		agents: filepath.Join(home, ".agents", "skills"),
		claude: filepath.Join(home, ".claude", "skills"),
		cursor: filepath.Join(home, ".cursor", "skills"),
		repoSk: filepath.Join(home, "src", "app", ".claude", "skills"),
	}
	if err := os.MkdirAll(filepath.Join(home, "src", "app", ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	for dir, v := range map[string]string{c.agents: "1.0.0", c.claude: "1.2.3", c.cursor: "1.0.0", c.repoSk: "1.0.0"} {
		if _, err := skill.Install(dir, v); err != nil {
			t.Fatal(err)
		}
	}
	reg, err := skill.OpenRegistry()
	if err != nil {
		t.Fatal(err)
	}
	reg.Record(c.cursor)
	reg.Record(c.repoSk)
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(c.cursor, "ytrack")); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSkillList(t *testing.T) {
	t.Run("given recorded copies, --json reports each dir's version, state and whether ytrack updates it", func(t *testing.T) {
		c := setupCopies(t)
		if code := c.e.Run("skill", "list", "--json"); code != 0 {
			t.Fatalf("exit %d: %s", code, c.e.Stderr)
		}
		var rows []struct {
			Dir, Path, Version, State string
			Managed                   bool
		}
		if err := json.Unmarshal(c.e.Stdout.Bytes(), &rows); err != nil {
			t.Fatalf("stdout %q: %v", c.e.Stdout, err)
		}
		got := map[string]string{}
		for _, r := range rows {
			got[r.Dir] = strings.Join([]string{r.Version, r.State, map[bool]string{true: "auto", false: "manual"}[r.Managed]}, " ")
		}
		want := map[string]string{
			c.agents: "1.0.0 stale auto",
			c.claude: "1.2.3 current auto",
			c.cursor: " missing auto",
			c.repoSk: "1.0.0 stale manual",
		}
		for d, w := range want {
			if got[d] != w {
				t.Errorf("%s: %q, want %q", d, got[d], w)
			}
		}
		if len(rows) != len(want) {
			t.Errorf("%d rows, want %d", len(rows), len(want))
		}
	})

	t.Run("given a stale repository copy, it tells how to update it", func(t *testing.T) {
		c := setupCopies(t)
		if code := c.e.Run("skill", "list"); code != 0 {
			t.Fatalf("exit %d: %s", code, c.e.Stderr)
		}
		if want := "ytrack skill install --dir " + c.repoSk; !strings.Contains(c.e.Stderr.String(), want) {
			t.Errorf("stderr %q lacks %q", c.e.Stderr, want)
		}
		if !strings.Contains(c.e.Stdout.String(), c.repoSk+"\t1.0.0\tstale\tmanual") {
			t.Errorf("stdout %q", c.e.Stdout)
		}
	})

	t.Run("given a copy from a newer ytrack, it reports newer, not stale, and does not suggest a sync that would downgrade it", func(t *testing.T) {
		c := setupCopies(t)
		if _, err := skill.Install(c.agents, "9.0.0"); err != nil {
			t.Fatal(err)
		}
		if code := c.e.Run("skill", "list"); code != 0 {
			t.Fatalf("exit %d: %s", code, c.e.Stderr)
		}
		if !strings.Contains(c.e.Stdout.String(), c.agents+"\t9.0.0\tnewer\tauto") {
			t.Errorf("stdout %q", c.e.Stdout)
		}
		if strings.Contains(c.e.Stderr.String(), "update the stale copies") {
			t.Errorf("suggests a downgrading sync: %q", c.e.Stderr)
		}
		if !strings.Contains(c.e.Stderr.String(), "newer ytrack") {
			t.Errorf("stderr %q does not explain the newer copy", c.e.Stderr)
		}
	})
}

func TestSkillSync(t *testing.T) {
	t.Run("given stale, missing and repository copies, it rewrites the home ones, drops the missing and leaves the repository", func(t *testing.T) {
		c := setupCopies(t)
		if code := c.e.Run("skill", "sync"); code != 0 {
			t.Fatalf("exit %d: %s", code, c.e.Stderr)
		}
		for _, line := range []string{
			"updated\t" + filepath.Join(c.agents, "ytrack") + "\t1.2.3",
			"updated\t" + filepath.Join(c.claude, "ytrack") + "\t1.2.3",
			"dropped\t" + filepath.Join(c.cursor, "ytrack") + "\t(missing)",
			"manual\t" + filepath.Join(c.repoSk, "ytrack") + "\t1.0.0",
		} {
			if !strings.Contains(c.e.Stdout.String(), line+"\n") {
				t.Errorf("stdout %q lacks %q", c.e.Stdout, line)
			}
		}
		if v := version(t, filepath.Join(c.agents, "ytrack")); v != "1.2.3" {
			t.Errorf("agents copy at %s", v)
		}
		if v := version(t, filepath.Join(c.repoSk, "ytrack")); v != "1.0.0" {
			t.Errorf("the repository copy was rewritten to %s", v)
		}
		if _, err := os.Lstat(filepath.Join(c.cursor, "ytrack")); err == nil {
			t.Error("the missing copy was re-created")
		}
		if got := recorded(t); !slices.Equal(got, []string{c.agents, c.claude, c.repoSk}) {
			t.Errorf("recorded %q", got)
		}
	})

	t.Run("given nothing recorded, it exits 0", func(t *testing.T) {
		e := env(t)
		if code := e.Run("skill", "sync"); code != 0 || e.Stdout.Len() != 0 {
			t.Fatalf("exit %d, stdout %q", code, e.Stdout)
		}
	})
}

func TestSkillUninstall(t *testing.T) {
	t.Run("given an installed dir, it removes the copy and forgets it", func(t *testing.T) {
		c := setupCopies(t)
		if code := c.e.Run("skill", "uninstall", "--dir", c.claude); code != 0 {
			t.Fatalf("exit %d: %s", code, c.e.Stderr)
		}
		if _, err := os.Lstat(filepath.Join(c.claude, "ytrack")); err == nil {
			t.Error("the copy survived")
		}
		if got := recorded(t); slices.Contains(got, c.claude) {
			t.Errorf("still recorded: %q", got)
		}
	})

	t.Run("given a foreign ytrack dir, it exits 1 and leaves it untouched", func(t *testing.T) {
		e := env(t)
		root := t.TempDir()
		mine := filepath.Join(root, "ytrack", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(mine), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mine, []byte("---\nname: other\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := e.Run("skill", "uninstall", "--dir", root); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		if _, err := os.Stat(mine); err != nil {
			t.Error(err)
		}
	})

	t.Run("given no --dir, it is a usage error", func(t *testing.T) {
		e := env(t)
		if code := e.Run("skill", "uninstall"); !e.IsUsageError(code) {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
	})
}
