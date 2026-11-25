package query_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func loggedIn(t *testing.T) *cmdtest.Env {
	t.Helper()
	env := cmdtest.New(t)
	yt := cmdtest.NewFakeYouTrack(t, "")
	yt.SeedSampleIssues()
	yt.SeedSampleHistory()
	env.Login(yt)
	return env
}

func TestQueryList(t *testing.T) {
	t.Run("given --json, it prints every saved search with its query and owner", func(t *testing.T) {
		env := loggedIn(t)
		if code := env.Run("query", "list", "--json", "id,name,query,owner"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		var got []map[string]any
		if err := json.Unmarshal(env.Stdout.Bytes(), &got); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, env.Stdout)
		}
		want := []map[string]any{
			{"id": "120-1", "name": "My open", "query": "for: me #Unresolved", "owner": "jdoe"},
			{"id": "120-2", "name": "Nightshift bugs", "query": "project: NSR Type: Bug", "owner": "jroe"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("given no flags, it prints a table of names and queries", func(t *testing.T) {
		env := loggedIn(t)
		if code := env.Run("query", "list"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		for _, want := range []string{"My open", "for: me #Unresolved", "Nightshift bugs"} {
			if !strings.Contains(env.Stdout.String(), want) {
				t.Errorf("stdout lacks %q:\n%s", want, env.Stdout)
			}
		}
	})

	t.Run("given no saved searches, it says so on stderr", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Login(yt)
		if code := env.Run("query", "list"); code != 0 || env.Stdout.Len() != 0 || !strings.Contains(env.Stderr.String(), "no saved searches") {
			t.Errorf("exit %d, stdout %q, stderr %q", code, env.Stdout, env.Stderr)
		}
	})
}
