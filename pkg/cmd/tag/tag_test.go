package tag_test

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

func posts(yt *cmdtest.FakeYouTrack) []cmdtest.Request {
	var out []cmdtest.Request
	for _, r := range yt.Requests() {
		if r.Method == "POST" && r.Path == "/api/tags" {
			out = append(out, r)
		}
	}
	return out
}

func TestTagList(t *testing.T) {
	t.Run("given --json, it prints every tag with its owner", func(t *testing.T) {
		env, _ := loggedIn(t)
		if code := env.Run("tag", "list", "--json", "id,name,owner"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		var got []map[string]any
		if err := json.Unmarshal(env.Stdout.Bytes(), &got); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, env.Stdout)
		}
		want := []map[string]any{
			{"id": "6-1", "name": "backend", "owner": "jdoe"},
			{"id": "6-2", "name": "sso", "owner": "jdoe"},
			{"id": "6-3", "name": "regression", "owner": "jroe"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("given no flags, it prints a table of names", func(t *testing.T) {
		env, _ := loggedIn(t)
		if code := env.Run("tag", "list"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		for _, want := range []string{"backend", "sso", "regression", "jroe"} {
			if !strings.Contains(env.Stdout.String(), want) {
				t.Errorf("stdout lacks %q:\n%s", want, env.Stdout)
			}
		}
	})
}

func TestTagCreate(t *testing.T) {
	t.Run("given a new name, it creates the tag and prints it as JSON", func(t *testing.T) {
		env, yt := loggedIn(t)
		if code := env.Run("tag", "create", "needs review", "--json", "id,name,owner"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		reqs := posts(yt)
		if len(reqs) != 1 || reqs[0].Body != `{"name":"needs review"}` {
			t.Fatalf("POST /api/tags requests = %+v", reqs)
		}
		var got map[string]any
		if err := json.Unmarshal(env.Stdout.Bytes(), &got); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, env.Stdout)
		}
		if got["name"] != "needs review" || got["id"] == "" || got["owner"] != "jdoe" {
			t.Errorf("got %v", got)
		}
		// The new tag is then usable by issue create --tag.
		if code := env.Run("issue", "create", "-p", "NSR", "-s", "x", "--tag", "needs review"); code != 0 {
			t.Errorf("issue create --tag: exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given no --json, it prints the tag name", func(t *testing.T) {
		env, _ := loggedIn(t)
		if code := env.Run("tag", "create", "nightly"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != "nightly\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given a name a visible tag already has, it creates nothing and exits 1", func(t *testing.T) {
		env, yt := loggedIn(t)
		code := env.Run("tag", "create", "Backend")
		if code != 1 || !strings.Contains(env.Stderr.String(), `tag "backend" already exists (ID 6-1)`) {
			t.Errorf("exit %d, stderr = %q", code, env.Stderr)
		}
		if len(posts(yt)) != 0 {
			t.Error("it created a tag")
		}
	})

	t.Run("given a blank name, it is a usage error", func(t *testing.T) {
		env, yt := loggedIn(t)
		before := len(yt.Requests())
		if code := env.Run("tag", "create", "  "); code != 1 || !strings.Contains(env.Stderr.String(), "the tag name is empty") {
			t.Errorf("exit %d, stderr = %q", code, env.Stderr)
		}
		if n := len(yt.Requests()) - before; n != 0 {
			t.Errorf("it sent %d requests", n)
		}
	})
}
