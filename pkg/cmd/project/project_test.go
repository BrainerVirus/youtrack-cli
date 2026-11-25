package project_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func loggedIn(t *testing.T) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
	t.Helper()
	env := cmdtest.New(t)
	yt := cmdtest.NewFakeYouTrack(t, "")
	yt.SeedSampleIssues()
	yt.SeedSampleProjects()
	env.Login(yt)
	return env, yt
}

func fieldsJSON(t *testing.T, env *cmdtest.Env, args ...string) map[string]map[string]any {
	t.Helper()
	if code := env.Run(args...); code != 0 {
		t.Fatalf("exit %d: %s", code, env.Stderr)
	}
	var list []map[string]any
	if err := json.Unmarshal(env.Stdout.Bytes(), &list); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, env.Stdout)
	}
	out := map[string]map[string]any{}
	for _, f := range list {
		out[f["name"].(string)] = f
	}
	return out
}

func TestProjectFields(t *testing.T) {
	t.Run("given --json, it prints each field's type, values, requiredness and default", func(t *testing.T) {
		env, _ := loggedIn(t)
		got := fieldsJSON(t, env, "project", "fields", "nsr", "--json", "id,name,type,bundle,required,default")
		if len(got) != 12 {
			t.Errorf("got %d fields, want 12", len(got))
		}
		cases := []struct {
			name string
			want map[string]any
		}{
			{"Priority", map[string]any{
				"id": "93-10", "name": "Priority", "type": "enum[1]", "required": true,
				"bundle": []any{"Major", "Critical", "Normal", "Minor"}, "default": []any{"Normal"},
			}},
			{"Assignee", map[string]any{
				"id": "93-13", "name": "Assignee", "type": "user[1]", "required": false,
				"bundle": []any{}, "default": []any{"jroe"},
			}},
			{"Team", map[string]any{
				"id": "93-20", "name": "Team", "type": "group[1]", "required": false,
				"bundle": []any{"Developers", "QA"}, "default": []any{},
			}},
			{"Fix versions", map[string]any{
				"id": "93-15", "name": "Fix versions", "type": "version[*]", "required": false,
				"bundle": []any{"2026.3", "2026.4"}, "default": []any{},
			}},
		}
		for _, c := range cases {
			if !reflect.DeepEqual(got[c.name], c.want) {
				t.Errorf("%s = %v, want %v", c.name, got[c.name], c.want)
			}
		}
	})

	t.Run("given no flags, it prints a table with types, defaults and values", func(t *testing.T) {
		env, _ := loggedIn(t)
		if code := env.Run("project", "fields", "NSR"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		var priority string
		for l := range strings.SplitSeq(env.Stdout.String(), "\n") {
			if strings.HasPrefix(l, "Priority") {
				priority = l
			}
		}
		for _, want := range []string{"enum[1]", "yes", "Normal", "Major, Critical, Normal, Minor"} {
			if !strings.Contains(priority, want) {
				t.Errorf("Priority row %q lacks %q", priority, want)
			}
		}
		if strings.Contains(priority, "Show-stopper") {
			t.Errorf("Priority row %q lists an archived value", priority)
		}
	})

	t.Run("given an account that cannot read the project settings, it reads the fields from an issue without defaults", func(t *testing.T) {
		env, yt := loggedIn(t)
		yt.HideFieldSettings = true
		got := fieldsJSON(t, env, "project", "fields", "NSR", "--json", "name,type,default")
		if p := got["Priority"]; p == nil || p["type"] != "enum[1]" || p["default"] != nil {
			t.Errorf("Priority = %v", p)
		}
		if !strings.Contains(env.Stderr.String(), "cannot read the settings of project NSR") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given an unknown project, it lists the projects and exits 1", func(t *testing.T) {
		env, _ := loggedIn(t)
		code := env.Run("project", "fields", "NOPE")
		if code != 1 || !strings.Contains(env.Stderr.String(), `unknown project "NOPE"`) || !strings.Contains(env.Stderr.String(), "\n  OPS") {
			t.Errorf("exit %d, stderr = %q", code, env.Stderr)
		}
	})
}
