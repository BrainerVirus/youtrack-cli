package issue_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func linkWrites(yt *cmdtest.FakeYouTrack) []cmdtest.Request {
	var out []cmdtest.Request
	for _, r := range yt.Requests() {
		if r.Method != http.MethodGet && strings.Contains(r.Path, "/links/") {
			out = append(out, r)
		}
	}
	return out
}

func TestIssueLink(t *testing.T) {
	t.Run("given 'depends on', it adds the target to the type's INWARD link and confirms with the phrase", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "link", "NSR-40", "NSR-41", "--type", "depends on")
		w := linkWrites(yt)
		if len(w) != 1 || w[0].Method != http.MethodPost || w[0].Path != "/api/issues/NSR-40/links/173-1t/issues" {
			t.Fatalf("writes = %+v", w)
		}
		var body map[string]string
		if err := json.Unmarshal([]byte(w[0].Body), &body); err != nil || body["idReadable"] != "NSR-41" {
			t.Errorf("body = %s", w[0].Body)
		}
		if !strings.Contains(env.Stderr.String(), "NSR-40 depends on NSR-41") {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if want := (cmdtest.Link{TypeID: "173-1", Source: "NSR-41", Target: "NSR-40"}); !slices.Equal(yt.Links, []cmdtest.Link{want}) {
			t.Errorf("links = %+v", yt.Links)
		}
	})

	t.Run("given the source phrase in another case, it uses the OUTWARD link", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "link", "NSR-40", "NSR-41", "--type", "Is Required For")
		if w := linkWrites(yt); len(w) != 1 || w[0].Path != "/api/issues/NSR-40/links/173-1s/issues" {
			t.Fatalf("writes = %+v", w)
		}
	})

	t.Run("given an undirected type's name, it uses the link ID without a direction suffix", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "link", "NSR-40", "NSR-41", "--type", "relates")
		if w := linkWrites(yt); len(w) != 1 || w[0].Path != "/api/issues/NSR-40/links/173-0/issues" {
			t.Fatalf("writes = %+v", w)
		}
	})

	for _, phrase := range []string{"Depend", "blocks"} {
		t.Run("given --type "+phrase+", it is a usage error listing the valid phrases and sends no write", func(t *testing.T) {
			env, yt := loggedIn(t, "")
			if code := env.Run("issue", "link", "NSR-40", "NSR-41", "--type", phrase); code != 1 {
				t.Fatalf("exit %d, want 1", code)
			}
			for _, want := range []string{`"depends on"`, `"is required for"`, `"relates to"`, `"Relates"`, `"subtask of"`} {
				if !strings.Contains(env.Stderr.String(), want) {
					t.Errorf("stderr lacks %s: %s", want, env.Stderr)
				}
			}
			if strings.Contains(env.Stderr.String(), `"Depend",`) {
				t.Errorf("stderr offers a directed type's name: %s", env.Stderr)
			}
			if w := linkWrites(yt); len(w) != 0 {
				t.Errorf("writes = %+v", w)
			}
		})
	}

	t.Run("given no --type, it is a usage error", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("issue", "link", "NSR-40", "NSR-41"); code != 1 || !strings.Contains(env.Stderr.String(), "--type") {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if w := linkWrites(yt); len(w) != 0 {
			t.Errorf("writes = %+v", w)
		}
	})

	t.Run("given an unknown target, it names the issues and stops", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "link", "NSR-40", "NSR-99", "--type", "depends on"); code != 1 || !strings.Contains(env.Stderr.String(), "NSR-99") {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given --json, it prints the added link with domain field names", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		mustRun(t, env, "issue", "link", "NSR-40", "NSR-41", "--type", "subtask of", "--json", "issue,type,phrase,direction,issues")
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		if got["issue"] != "NSR-40" || got["type"] != "Subtask" || got["phrase"] != "subtask of" || got["direction"] != "INWARD" ||
			!slices.Equal(anyStrings(got["issues"]), []string{"NSR-41"}) {
			t.Errorf("json = %v", got)
		}
	})
}

func TestIssueLinkList(t *testing.T) {
	seeded := func(t *testing.T) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
		t.Helper()
		env, yt := loggedIn(t, "")
		yt.Links = []cmdtest.Link{
			{TypeID: "173-1", Source: "NSR-41", Target: "NSR-40"}, // NSR-40 depends on NSR-41
			{TypeID: "173-0", Source: "NSR-41", Target: "NSR-40"}, // relates, seen from the target
		}
		return env, yt
	}

	t.Run("given --list --json, it prints each non-empty link's phrase, direction and target IDs", func(t *testing.T) {
		env, _ := seeded(t)
		mustRun(t, env, "issue", "link", "NSR-40", "--list", "--json", "phrase,direction,issues,type")
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		if len(got) != 2 {
			t.Fatalf("got %d links: %s", len(got), env.Stdout)
		}
		if got[0]["phrase"] != "relates to" || got[0]["direction"] != "BOTH" || got[0]["type"] != "Relates" ||
			!slices.Equal(anyStrings(got[0]["issues"]), []string{"NSR-41"}) {
			t.Errorf("first link = %v", got[0])
		}
		if got[1]["phrase"] != "depends on" || got[1]["direction"] != "INWARD" || !slices.Equal(anyStrings(got[1]["issues"]), []string{"NSR-41"}) {
			t.Errorf("second link = %v", got[1])
		}
	})

	t.Run("given --list, it prints phrase<TAB>issue per linked issue, as seen from that issue", func(t *testing.T) {
		env, _ := seeded(t)
		mustRun(t, env, "issue", "link", "NSR-41", "--list")
		if want := "relates to\tNSR-40\nis required for\tNSR-40\n"; env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("given an issue with no links, it says so on stderr", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		mustRun(t, env, "issue", "link", "NSR-40", "--list")
		if env.Stdout.Len() != 0 || env.Stderr.String() != "NSR-40 has no links\n" {
			t.Errorf("stdout %q, stderr %q", env.Stdout, env.Stderr)
		}
	})

	t.Run("given --list-types --json, it prints both phrases of each type", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		mustRun(t, env, "issue", "link", "--list-types", "--json", "name,sourceToTarget,targetToSource,directed")
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		if len(got) != 4 || got[1]["name"] != "Depend" || got[1]["sourceToTarget"] != "is required for" ||
			got[1]["targetToSource"] != "depends on" || got[1]["directed"] != true {
			t.Errorf("types = %v", got)
		}
	})

	t.Run("given a link-type JSON field with --list, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "link", "NSR-40", "--list", "--json", "sourceToTarget"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given --list with a target, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "link", "NSR-40", "NSR-41", "--list"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
	})
}

func TestIssueUnlink(t *testing.T) {
	t.Run("given 'depends on', it deletes the target, by database ID, from the INWARD link", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.Links = []cmdtest.Link{{TypeID: "173-1", Source: "NSR-41", Target: "NSR-40"}}
		mustRun(t, env, "issue", "unlink", "NSR-40", "NSR-41", "--type", "depends on")
		w := linkWrites(yt)
		if len(w) != 1 || w[0].Method != http.MethodDelete || w[0].Path != "/api/issues/NSR-40/links/173-1t/issues/2-4041" {
			t.Fatalf("writes = %+v", w)
		}
		if len(yt.Links) != 0 {
			t.Errorf("links left: %+v", yt.Links)
		}
		if !strings.Contains(env.Stderr.String(), "NSR-40 depends on NSR-41") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a target that is not linked that way, it removes nothing and says so", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.Links = []cmdtest.Link{{TypeID: "173-1", Source: "NSR-41", Target: "NSR-40"}}
		if code := env.Run("issue", "unlink", "NSR-40", "NSR-41", "--type", "is required for"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if !strings.Contains(env.Stderr.String(), "no such link: NSR-40 is required for NSR-41") {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if w := linkWrites(yt); len(w) != 0 || len(yt.Links) != 1 {
			t.Errorf("writes = %+v, links = %+v", w, yt.Links)
		}
	})

	t.Run("given an undirected link seen from its target, it removes it", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.Links = []cmdtest.Link{{TypeID: "173-0", Source: "NSR-41", Target: "NSR-40"}}
		mustRun(t, env, "issue", "unlink", "NSR-40", "NSR-41", "--type", "Relates")
		if len(yt.Links) != 0 {
			t.Errorf("links left: %+v", yt.Links)
		}
	})

	t.Run("given a directed type's name, it is a usage error and sends no write", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.Links = []cmdtest.Link{{TypeID: "173-1", Source: "NSR-41", Target: "NSR-40"}}
		if code := env.Run("issue", "unlink", "NSR-40", "NSR-41", "--type", "depend"); code != 1 || !strings.Contains(env.Stderr.String(), "directed") {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if w := linkWrites(yt); len(w) != 0 {
			t.Errorf("writes = %+v", w)
		}
	})
}

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, len(list))
	for i, e := range list {
		out[i], _ = e.(string)
	}
	return out
}
