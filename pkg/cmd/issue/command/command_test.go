package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func htmlPreview(t *testing.T) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
	t.Helper()
	env := cmdtest.New(t)
	yt := cmdtest.NewFakeYouTrack(t, "")
	yt.SeedSampleIssues()
	yt.SeedSampleProjects()
	yt.Tags = append(yt.Tags, map[string]any{"$type": "IssueTag", "id": "6-9", "name": "R&D"})
	yt.CommandHTML = true
	env.Login(yt)
	return env, yt
}

func TestCommandDryRunHTML(t *testing.T) {
	t.Run("given an HTML preview, it prints plain text without tags or entities", func(t *testing.T) {
		env, _ := htmlPreview(t)
		if code := env.Run("issue", "command", "NSR-40", "tag R&D for me", "--dry-run"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got, want := env.Stdout.String(), "  Add tag R&D\n  Set Assignee jdoe\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})

	t.Run("given escape sequences hidden in entities, no ESC or BEL reaches the output", func(t *testing.T) {
		env, yt := htmlPreview(t)
		yt.CommandDescriptions = []string{
			`Add tag <span class="bold">&#27;[31mred</span>`,
			`Set Assignee &#x1b;]0;x&#7;<b>jdoe</b>`,
		}
		if code := env.Run("issue", "command", "NSR-40", "tag R&D for me", "--dry-run"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		out := env.Stdout.String() + env.Stderr.String()
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Fatalf("output has ESC or BEL: %q", out)
		}
		if got, want := env.Stdout.String(), "  Add tag [31mred\n  Set Assignee ]0;xjdoe\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})

	t.Run("given literal angle brackets, they survive; only real tags are dropped", func(t *testing.T) {
		env, yt := htmlPreview(t)
		yt.CommandDescriptions = []string{`a < b > c`, `Set <i>x</i> &lt;b&gt; 1<2`}
		if code := env.Run("issue", "command", "NSR-40", "tag R&D for me", "--dry-run"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got, want := env.Stdout.String(), "  a < b > c\n  Set x <b> 1<2\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})

	t.Run("given --json, it keeps YouTrack's HTML description", func(t *testing.T) {
		env, _ := htmlPreview(t)
		if code := env.Run("issue", "command", "NSR-40", "tag R&D for me", "--dry-run", "--json", "commands"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		var out struct {
			Commands []struct{ Description string } `json:"commands"`
		}
		if err := json.Unmarshal(env.Stdout.Bytes(), &out); err != nil {
			t.Fatalf("stdout %q: %v", env.Stdout, err)
		}
		if len(out.Commands) != 2 || out.Commands[0].Description != `Add tag <span class="bold">R&amp;D</span>` ||
			out.Commands[1].Description != `Set Assignee <span class="bold">jdoe</span>` {
			t.Errorf("commands = %+v", out.Commands)
		}
	})
}
