package issue_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func withHistory(t *testing.T) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
	t.Helper()
	env, yt := loggedIn(t, "")
	yt.SeedSampleProjects()
	yt.SeedSampleHistory()
	return env, yt
}

func TestIssueWatchers(t *testing.T) {
	t.Run("given --json, it lists each watcher and whether they starred the issue", func(t *testing.T) {
		env, _ := withHistory(t)
		mustRun(t, env, "issue", "watchers", "NSR-40", "--json", "login,fullName,starred")
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		want := []map[string]any{
			{"login": "jdoe", "fullName": "John Doe", "starred": true},
			{"login": "jroe", "fullName": "Jane Roe", "starred": false},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("given an issue nobody watches, it says so on stderr and prints nothing", func(t *testing.T) {
		env, _ := withHistory(t)
		mustRun(t, env, "issue", "watchers", "NSR-41")
		if env.Stdout.Len() != 0 || !strings.Contains(env.Stderr.String(), "nobody watches NSR-41") {
			t.Errorf("stdout %q, stderr %q", env.Stdout, env.Stderr)
		}
	})

	t.Run("given an unknown issue, it exits 1 naming it", func(t *testing.T) {
		env, _ := withHistory(t)
		if code := env.Run("issue", "watchers", "NSR-99"); code != 1 || !strings.Contains(env.Stderr.String(), "NSR-99") {
			t.Errorf("exit %d, stderr %q", code, env.Stderr)
		}
	})
}

func TestIssueVote(t *testing.T) {
	t.Run("given vote, it applies the vote command and prints the new count", func(t *testing.T) {
		env, yt := withHistory(t)
		mustRun(t, env, "issue", "vote", "NSR-40", "--json", "issue,votes,voted")
		cmds := requests(yt, http.MethodPost, "/api/commands")
		if len(cmds) != 1 || !strings.Contains(cmds[0].Body, `"query":"vote"`) || !strings.Contains(cmds[0].Body, `"idReadable":"NSR-40"`) {
			t.Fatalf("command requests = %+v", cmds)
		}
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		want := map[string]any{"issue": "NSR-40", "votes": float64(3), "voted": true}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("given unvote after vote, it removes the vote", func(t *testing.T) {
		env, _ := withHistory(t)
		mustRun(t, env, "issue", "vote", "NSR-40")
		env.Reset()
		mustRun(t, env, "issue", "unvote", "NSR-40")
		if env.Stdout.String() != "NSR-40\t2\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given YouTrack accepts the command but the vote does not change, it exits 1 naming the risk", func(t *testing.T) {
		env, yt := withHistory(t)
		ignoreCommands(yt)
		code := env.Run("issue", "vote", "NSR-40")
		if code != 1 || env.Stdout.Len() != 0 {
			t.Fatalf("exit %d, stdout %q", code, env.Stdout)
		}
		for _, want := range []string{`accepted the "vote" command, but NSR-40 did not change`, "you reported", "issue command NSR-40 vote --assist"} {
			if !strings.Contains(env.Stderr.String(), want) {
				t.Errorf("stderr lacks %q: %s", want, env.Stderr)
			}
		}
	})

	t.Run("given an unknown issue, it exits 1 and changes nothing", func(t *testing.T) {
		env, _ := withHistory(t)
		if code := env.Run("issue", "vote", "NSR-99"); code != 1 || !strings.Contains(env.Stderr.String(), "NSR-99") {
			t.Errorf("exit %d, stderr %q", code, env.Stderr)
		}
	})
}

func TestIssueWatch(t *testing.T) {
	t.Run("given unwatch, it writes hasStar false, sends no command, and you leave the starred watchers", func(t *testing.T) {
		env, yt := withHistory(t)
		mustRun(t, env, "issue", "unwatch", "NSR-40", "--json", "issue,watching")
		if cmds := requests(yt, http.MethodPost, "/api/commands"); len(cmds) != 0 {
			t.Fatalf("command requests = %+v; the star must not depend on command words", cmds)
		}
		writes := requests(yt, http.MethodPost, "/api/issues/NSR-40")
		if len(writes) != 1 || writes[0].Body != `{"watchers":{"hasStar":false}}` {
			t.Fatalf("issue writes = %+v", writes)
		}
		if got := decodeJSON[map[string]any](t, env.Stdout.Bytes()); !reflect.DeepEqual(got, map[string]any{"issue": "NSR-40", "watching": false}) {
			t.Errorf("got %v", got)
		}
		env.Reset()
		mustRun(t, env, "issue", "watchers", "NSR-40", "--json", "login")
		if got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes()); !reflect.DeepEqual(got, []map[string]any{{"login": "jroe"}}) {
			t.Errorf("watchers after unwatch = %v", got)
		}
	})

	t.Run("given watch, it writes hasStar true, stars the issue and prints watching", func(t *testing.T) {
		env, yt := withHistory(t)
		mustRun(t, env, "issue", "watch", "NSR-41")
		if env.Stdout.String() != "NSR-41\twatching\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		if writes := requests(yt, http.MethodPost, "/api/issues/NSR-41"); len(writes) != 1 || writes[0].Body != `{"watchers":{"hasStar":true}}` {
			t.Errorf("issue writes = %+v", writes)
		}
		env.Reset()
		mustRun(t, env, "issue", "watchers", "NSR-41", "--json", "login,starred")
		if got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes()); !reflect.DeepEqual(got, []map[string]any{{"login": "jdoe", "starred": true}}) {
			t.Errorf("watchers after watch = %v", got)
		}
	})

	t.Run("given YouTrack accepts the write but the star does not change, it exits 1", func(t *testing.T) {
		env, yt := withHistory(t)
		yt.IgnoreStarWrites = true
		code := env.Run("issue", "watch", "NSR-41")
		if code != 1 || env.Stdout.Len() != 0 || !strings.Contains(env.Stderr.String(), "the star on NSR-41 did not change") {
			t.Errorf("exit %d, stdout %q, stderr %q", code, env.Stdout, env.Stderr)
		}
	})

	t.Run("given an unknown issue, it exits 1 naming it", func(t *testing.T) {
		env, _ := withHistory(t)
		if code := env.Run("issue", "unwatch", "NSR-99"); code != 1 || !strings.Contains(env.Stderr.String(), "NSR-99") {
			t.Errorf("exit %d, stderr %q", code, env.Stderr)
		}
	})
}

func TestIssueActivities(t *testing.T) {
	t.Run("given no --category, it asks for every category and prints each change", func(t *testing.T) {
		env, yt := withHistory(t)
		mustRun(t, env, "issue", "activities", "NSR-40", "--json", "id,timestamp,author,category,field,added,removed")
		reqs := requests(yt, http.MethodGet, "/api/issues/NSR-40/activities")
		if len(reqs) == 0 || !strings.Contains(query(t, reqs[0]).Get("categories"), "CustomFieldCategory") {
			t.Fatalf("activities requests = %+v", reqs)
		}
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		if len(got) != 5 {
			t.Fatalf("got %d activities: %v", len(got), got)
		}
		state := got[1]
		want := map[string]any{
			"id": "a-2", "timestamp": rfc3339(1790100000000),
			"author":   map[string]any{"login": "jdoe", "fullName": "John Doe"},
			"category": "fields", "field": "State",
			"added": []any{"In Progress"}, "removed": []any{"Open"},
		}
		if !reflect.DeepEqual(state, want) {
			t.Errorf("State change = %v, want %v", state, want)
		}
		if summary := got[4]; !reflect.DeepEqual(summary["added"], []any{"Login redirect drops the return URL"}) {
			t.Errorf("summary change = %v", summary)
		}
	})

	t.Run("given --category names, it sends only their IDs and prints a table", func(t *testing.T) {
		env, yt := withHistory(t)
		mustRun(t, env, "issue", "activities", "NSR-40", "--category", "comments", "-c", "tags")
		reqs := requests(yt, http.MethodGet, "/api/issues/NSR-40/activities")
		if got := query(t, reqs[0]).Get("categories"); got != "CommentsCategory,TagsCategory" {
			t.Errorf("categories = %q", got)
		}
		out := env.Stdout.String()
		if !strings.Contains(out, "Reproduced on staging.") || !strings.Contains(out, "sso") || strings.Contains(out, "In Progress") {
			t.Errorf("stdout:\n%s", out)
		}
	})

	t.Run("given an unknown --category, it is a usage error that lists the names", func(t *testing.T) {
		env, yt := withHistory(t)
		before := len(yt.Requests())
		code := env.Run("issue", "activities", "NSR-40", "--category", "bogus")
		if code != 1 || !strings.Contains(env.Stderr.String(), `unknown --category "bogus"`) || !strings.Contains(env.Stderr.String(), "comments") {
			t.Errorf("exit %d, stderr %q", code, env.Stderr)
		}
		if n := len(yt.Requests()) - before; n != 0 {
			t.Errorf("it sent %d requests", n)
		}
	})

	t.Run("given --limit, it caps the changes", func(t *testing.T) {
		env, _ := withHistory(t)
		mustRun(t, env, "issue", "activities", "NSR-40", "--limit", "2", "--json", "id")
		if got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes()); len(got) != 2 {
			t.Errorf("got %v", got)
		}
	})
}

func TestIssueCount(t *testing.T) {
	t.Run("given YouTrack answers -1 while counting, it asks again and prints the final count", func(t *testing.T) {
		env, yt := withHistory(t)
		yt.CountPending = 2
		mustRun(t, env, "issue", "count", "project: {NSR}")
		if env.Stdout.String() != "2\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		reqs := requests(yt, http.MethodPost, "/api/issuesGetter/count")
		if len(reqs) != 3 {
			t.Fatalf("%d count requests, want 3", len(reqs))
		}
		if reqs[0].Body != `{"query":"project: {NSR}"}` {
			t.Errorf("body = %s", reqs[0].Body)
		}
	})

	t.Run("given --json, it prints the query and the count", func(t *testing.T) {
		env, _ := withHistory(t)
		mustRun(t, env, "issue", "count", "--json", "query,count")
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		if !reflect.DeepEqual(got, map[string]any{"query": "", "count": float64(2)}) {
			t.Errorf("got %v", got)
		}
	})
}

func TestIssueCommandAssist(t *testing.T) {
	t.Run("given --assist, it previews the parts and prints completions as full commands", func(t *testing.T) {
		env, yt := withHistory(t)
		code := env.Run("issue", "command", "NSR-40", "Sta", "--assist")
		if code != 1 {
			t.Errorf("exit %d, want 1 for the unparsed part", code)
		}
		if n := len(requests(yt, http.MethodPost, "/api/commands")); n != 0 {
			t.Errorf("it applied the command %d times", n)
		}
		reqs := requests(yt, http.MethodPost, "/api/commands/assist")
		if len(reqs) != 1 || !strings.Contains(query(t, reqs[0]).Get("fields"), "suggestions(") {
			t.Fatalf("assist requests = %+v", reqs)
		}
		out := env.Stdout.String()
		for _, want := range []string{"! Unknown command: Sta\n", "> State \tcomplete State\n"} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("given --assist --json, it exports the suggestions", func(t *testing.T) {
		env, _ := withHistory(t)
		env.Run("issue", "command", "NSR-40", "vo", "--assist", "--json", "suggestions")
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		want := []any{map[string]any{"option": "vote", "description": "complete vote", "command": "vote "}}
		if !reflect.DeepEqual(got["suggestions"], want) {
			t.Errorf("suggestions = %v", got["suggestions"])
		}
	})

	t.Run("given --assist with --comment, it is a usage error", func(t *testing.T) {
		env, _ := withHistory(t)
		if code := env.Run("issue", "command", "NSR-40", "vote", "--assist", "--comment", "x"); code != 1 || !strings.Contains(env.Stderr.String(), "--assist") {
			t.Errorf("exit %d, stderr %q", code, env.Stderr)
		}
	})
}

func TestIssueListQueryAssist(t *testing.T) {
	t.Run("given --query-assist, it prints completions for the query instead of issues", func(t *testing.T) {
		env, yt := withHistory(t)
		mustRun(t, env, "issue", "list", "-q", "project: NSR #Unr", "--query-assist")
		if n := len(requests(yt, http.MethodGet, "/api/issues")); n != 0 {
			t.Errorf("it listed issues %d times", n)
		}
		reqs := requests(yt, http.MethodPost, "/api/search/assist")
		if len(reqs) != 1 || reqs[0].Body != `{"caret":17,"query":"project: NSR #Unr"}` {
			t.Fatalf("assist requests = %+v", reqs)
		}
		if got := env.Stdout.String(); got != "project: NSR #Unresolved \tcomplete #Unresolved\n" {
			t.Errorf("stdout = %q", got)
		}
	})

	t.Run("given --query-assist without --query, it is a usage error", func(t *testing.T) {
		env, _ := withHistory(t)
		if code := env.Run("issue", "list", "--query-assist"); code != 1 || !strings.Contains(env.Stderr.String(), "--query-assist") {
			t.Errorf("exit %d, stderr %q", code, env.Stderr)
		}
	})

	t.Run("given --query-assist --json, it prints the completions as JSON", func(t *testing.T) {
		env, _ := withHistory(t)
		mustRun(t, env, "issue", "list", "-q", "project: NSR #Unr", "--query-assist", "--json", "query,option,description,completionStart,completionEnd")
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		want := []map[string]any{{
			"query": "project: NSR #Unresolved ", "option": "#Unresolved", "description": "complete #Unresolved",
			"completionStart": float64(13), "completionEnd": float64(17),
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("given --query-assist --jq alone, it filters every completion field", func(t *testing.T) {
		env, _ := withHistory(t)
		mustRun(t, env, "issue", "list", "-q", "#Unr", "--query-assist", "--jq", ".[].option")
		if env.Stdout.String() != "#Unresolved\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given --query-assist with an issue field in --json, it names the completion fields", func(t *testing.T) {
		env, _ := withHistory(t)
		code := env.Run("issue", "list", "-q", "x", "--query-assist", "--json", "idReadable")
		if code != 1 || !strings.Contains(env.Stderr.String(), `unknown JSON field for --query-assist: "idReadable"`) || !strings.Contains(env.Stderr.String(), "completionStart") {
			t.Errorf("exit %d, stderr %q", code, env.Stderr)
		}
	})
}
