package command_test

import (
	"encoding/json"
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
