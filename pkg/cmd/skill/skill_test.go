package skill_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/build"
	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/skill"
)

// offline fails the test on any HTTP request.
type offline struct{ t *testing.T }

func (o offline) RoundTrip(r *http.Request) (*http.Response, error) {
	o.t.Errorf("unexpected request to %s", r.URL)
	return nil, errors.New("offline")
}

// env is a logged-out ytrack with no network, built as version 1.2.3.
func env(t *testing.T) *cmdtest.Env {
	t.Helper()
	old := build.Version
	build.Version = "1.2.3"
	t.Cleanup(func() { build.Version = old })
	e := cmdtest.New(t)
	e.Factory.RoundTripper = offline{t}
	return e
}

func TestSkillPrint(t *testing.T) {
	t.Run("given no login and no network, it prints SKILL.md stamped with the ytrack version", func(t *testing.T) {
		e := env(t)
		if code := e.Run("version"); code != 0 {
			t.Fatalf("version: exit %d", code)
		}
		version := strings.Fields(e.Stdout.String())[2] // "ytrack version 1.2.3"
		e.Reset()

		if code := e.Run("skill", "print"); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		m, err := skill.ParseMeta(e.Stdout.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if m.Name != "ytrack" || m.Metadata["version"] != version {
			t.Errorf("name %q version %q, want ytrack %q", m.Name, m.Metadata["version"], version)
		}
		if !strings.Contains(e.Stdout.String(), "ytrack auth status") {
			t.Error("printed no skill body")
		}
	})
}

func TestSkillInstall(t *testing.T) {
	t.Run("given --dir and --json, it installs and prints path and version", func(t *testing.T) {
		e := env(t)
		root := t.TempDir()
		if code := e.Run("skill", "install", "--dir", root, "--json"); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		var got struct{ Path, Version string }
		if err := json.Unmarshal(e.Stdout.Bytes(), &got); err != nil {
			t.Fatalf("stdout %q: %v", e.Stdout, err)
		}
		if want := filepath.Join(root, "ytrack"); got.Path != want || got.Version != "1.2.3" {
			t.Errorf("got %+v, want path %q version 1.2.3", got, want)
		}
		for _, f := range []string{"SKILL.md", filepath.Join("references", "api.md")} {
			if _, err := os.Stat(filepath.Join(got.Path, f)); err != nil {
				t.Error(err)
			}
		}
	})

	t.Run("given a rerun, it updates the skill and prints its directory", func(t *testing.T) {
		e := env(t)
		root := t.TempDir()
		if code := e.Run("skill", "install", "--dir", root); code != 0 {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
		e.Reset()
		build.Version = "1.3.0"
		if code := e.Run("skill", "install", "--dir", root); code != 0 {
			t.Fatalf("rerun: exit %d: %s", code, e.Stderr)
		}
		if got := strings.TrimSpace(e.Stdout.String()); got != filepath.Join(root, "ytrack") {
			t.Errorf("stdout = %q", got)
		}
		b, _ := os.ReadFile(filepath.Join(root, "ytrack", "SKILL.md"))
		if m, _ := skill.ParseMeta(b); m.Metadata["version"] != "1.3.0" {
			t.Errorf("version = %q, want 1.3.0", m.Metadata["version"])
		}
	})

	t.Run("given a foreign ytrack directory, it exits 1 and leaves it untouched", func(t *testing.T) {
		e := env(t)
		root := t.TempDir()
		mine := filepath.Join(root, "ytrack", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(mine), 0o750); err != nil {
			t.Fatal(err)
		}
		const text = "---\nname: someone-else\ndescription: x\n---\n"
		if err := os.WriteFile(mine, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := e.Run("skill", "install", "--dir", root, "--json"); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		if !strings.Contains(e.Stderr.String(), "not the ytrack skill") || e.Stdout.Len() != 0 {
			t.Errorf("stdout %q stderr %q", e.Stdout, e.Stderr)
		}
		if b, _ := os.ReadFile(mine); string(b) != text {
			t.Errorf("SKILL.md changed to %q", b)
		}
	})

	t.Run("given no --dir, it is a usage error", func(t *testing.T) {
		e := env(t)
		if code := e.Run("skill", "install"); !e.IsUsageError(code) {
			t.Fatalf("exit %d: %s", code, e.Stderr)
		}
	})
}
