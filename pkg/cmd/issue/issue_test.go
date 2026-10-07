package issue_test

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func loggedIn(t *testing.T, prefix string) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
	t.Helper()
	env := cmdtest.New(t)
	yt := cmdtest.NewFakeYouTrack(t, prefix)
	yt.SeedSampleIssues()
	env.Login(yt)
	return env, yt
}

// requests returns the requests after login to paths with the given suffix.
func requests(yt *cmdtest.FakeYouTrack, method, pathSuffix string) []cmdtest.Request {
	var out []cmdtest.Request
	for _, r := range yt.Requests() {
		if r.Method == method && strings.HasSuffix(r.Path, pathSuffix) {
			out = append(out, r)
		}
	}
	return out
}

func query(t *testing.T, r cmdtest.Request) url.Values {
	t.Helper()
	q, err := url.ParseQuery(r.RawQuery)
	if err != nil {
		t.Fatalf("bad query %q: %v", r.RawQuery, err)
	}
	return q
}

func mustRun(t *testing.T, env *cmdtest.Env, args ...string) {
	t.Helper()
	if code := env.Run(args...); code != 0 {
		t.Fatalf("ytrack %s: exit %d: %s", strings.Join(args, " "), code, env.Stderr)
	}
}

func decodeJSON[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, b)
	}
	return v
}

func TestIssueList(t *testing.T) {
	t.Run("given a native query, it sends it URL-encoded and unchanged", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		const q = "project: NSR for: me #Unresolved State: {In Progress} summary: a&b=c+d"
		mustRun(t, env, "issue", "list", "-q", q)

		reqs := requests(yt, "GET", "/api/issues")
		if len(reqs) == 0 {
			t.Fatal("no request")
		}
		raw := reqs[0].RawQuery
		if !strings.Contains(raw, "query=project%3A%20NSR%20for%3A%20me%20%23Unresolved%20State%3A%20%7BIn%20Progress%7D%20summary%3A%20a%26b%3Dc%2Bd&") {
			t.Errorf("query not encoded as expected: %s", raw)
		}
		if got := query(t, reqs[0]).Get("query"); got != q {
			t.Errorf("server decoded query %q, want %q", got, q)
		}
	})

	t.Run("given convenience flags, it appends them to the query as YouTrack terms", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "list", "-q", "#Unresolved", "--project", "NSR", "--assignee", "me", "--state", "In Progress", "--sort", "updated desc")
		got := query(t, yt.LastRequest(t)).Get("query")
		if want := "#Unresolved project: NSR for: me State: {In Progress} sort by: updated desc"; got != want {
			t.Errorf("query = %q, want %q", got, want)
		}
	})

	t.Run("given a multi-word sort attribute without an order, it braces it", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "list", "--sort", "issue id")
		if got := query(t, yt.LastRequest(t)).Get("query"); got != "sort by: {issue id}" {
			t.Errorf("query = %q", got)
		}
	})

	t.Run("without --json, it requests only the table's fields and prints one row per issue", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "list")
		q := query(t, yt.LastRequest(t))
		if got, want := q.Get("fields"), "idReadable,summary,updated,customFields(name,value(name,login,fullName))"; got != want {
			t.Errorf("fields = %q, want %q", got, want)
		}
		if got := q["customFields"]; !reflect.DeepEqual(got, []string{"State", "Assignee"}) {
			t.Errorf("customFields = %v, want only State and Assignee", got)
		}
		want := "NSR-40\tLogin redirect drops the return URL\tIn Progress\tjdoe\t2026-10-06T15:20:00Z\n" +
			"NSR-41\tDark mode toggle\tFixed\t\t2026-10-07T05:13:20Z\n"
		if env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("on a terminal, it prints a header and aligned columns", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		env.IO.SetStdoutTTY(true)
		mustRun(t, env, "issue", "list")
		lines := strings.Split(strings.TrimRight(env.Stdout.String(), "\n"), "\n")
		if len(lines) != 3 || !strings.HasPrefix(lines[0], "ID") || strings.Index(lines[0], "STATE") != strings.Index(lines[1], "In Progress") {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given --json fields, it requests only what they need and emits only them", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "list", "--json", "idReadable,priority,url")
		q := query(t, yt.LastRequest(t))
		if got, want := q.Get("fields"), "idReadable,customFields(name,value(name,login,fullName))"; got != want {
			t.Errorf("fields = %q, want %q", got, want)
		}
		if got := q["customFields"]; !reflect.DeepEqual(got, []string{"Priority"}) {
			t.Errorf("customFields = %v", got)
		}
		want := `[{"idReadable":"NSR-40","priority":"Major","url":"` + yt.URL() + `/issue/NSR-40"},` +
			`{"idReadable":"NSR-41","priority":"Normal","url":"` + yt.URL() + `/issue/NSR-41"}]` + "\n"
		if env.Stdout.String() != want {
			t.Errorf("stdout = %s\nwant     %s", env.Stdout, want)
		}
	})

	t.Run("given --json with plain attributes, it maps them to domain values", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "list", "--json", "id,project,reporter,assignee,state,created,resolved,tags,description")
		q := query(t, yt.LastRequest(t))
		if got, want := q.Get("fields"), "id,description,project(shortName,name),reporter(login,fullName),created,resolved,tags(name),customFields(name,value(name,login,fullName))"; got != want {
			t.Errorf("fields = %q, want %q", got, want)
		}
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		want := []map[string]any{
			{
				"id": "2-4040", "project": map[string]any{"shortName": "NSR", "name": "Nightshift"},
				"reporter": map[string]any{"login": "jroe", "fullName": "Jane Roe"},
				"assignee": map[string]any{"login": "jdoe", "fullName": "John Doe"},
				"state":    "In Progress", "created": "2026-09-21T14:13:20Z", "resolved": nil,
				"tags":        []any{"backend", "sso"},
				"description": "After SSO login the user lands on the dashboard.\r\n\r\nExpected: back to the page they came from.",
			},
			{
				"id": "2-4041", "project": map[string]any{"shortName": "NSR", "name": "Nightshift"},
				"reporter": map[string]any{"login": "jdoe", "fullName": "John Doe"},
				"assignee": nil, "state": "Fixed", "created": "2026-10-05T11:33:20Z", "resolved": "2026-10-07T05:13:20Z",
				"tags": []any{}, "description": "",
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got  %v\nwant %v", got, want)
		}
	})

	t.Run("given --json customFields, it requests all custom fields and decodes each kind", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "list", "--json", "customFields,state", "--limit", "1")
		q := query(t, yt.LastRequest(t))
		if got, want := q.Get("fields"), "customFields(name,value(name,login,fullName,minutes,presentation,text))"; got != want {
			t.Errorf("fields = %q, want %q", got, want)
		}
		if _, ok := q["customFields"]; ok {
			t.Errorf("customFields= restricts the fields: %v", q["customFields"])
		}
		got := decodeJSON[[]struct {
			State        string           `json:"state"`
			CustomFields []map[string]any `json:"customFields"`
		}](t, env.Stdout.Bytes())
		if len(got) != 1 || got[0].State != "In Progress" {
			t.Fatalf("got %+v", got)
		}
		byName := map[string]map[string]any{}
		for _, cf := range got[0].CustomFields {
			byName[cf["name"].(string)] = cf
		}
		want := map[string]map[string]any{
			"Priority":     {"name": "Priority", "kind": "enum", "value": "Major"},
			"State":        {"name": "State", "kind": "state", "value": "In Progress"},
			"Assignee":     {"name": "Assignee", "kind": "user", "value": map[string]any{"login": "jdoe", "fullName": "John Doe"}},
			"Subsystem":    {"name": "Subsystem", "kind": "owned", "value": []any{"Auth", "Web"}},
			"Fix versions": {"name": "Fix versions", "kind": "version", "value": []any{"2026.3"}},
			"Estimation":   {"name": "Estimation", "kind": "period", "value": map[string]any{"minutes": float64(750), "presentation": "1d 4h 30m"}},
			"Spent time":   {"name": "Spent time", "kind": "period", "value": nil},
			"Due Date":     {"name": "Due Date", "kind": "date", "value": "2026-10-07"},
			"Story points": {"name": "Story points", "kind": "simple", "value": float64(5)},
			"Root cause":   {"name": "Root cause", "kind": "text", "value": "Session cookie set before redirect"},
			"Sentiment":    {"name": "Sentiment", "kind": "unknown", "value": map[string]any{"$type": "Mood"}},
		}
		for name, w := range want {
			if !reflect.DeepEqual(byName[name], w) {
				t.Errorf("%s = %v, want %v", name, byName[name], w)
			}
		}
	})

	t.Run("given --json without fields, it fails listing the available fields", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		before := len(yt.Requests())
		if code := env.Run("issue", "list", "--json"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		for _, f := range []string{"idReadable", "customFields", "assignee", "url"} {
			if !strings.Contains(env.Stderr.String(), "  "+f+"\n") {
				t.Errorf("stderr does not list %s: %s", f, env.Stderr)
			}
		}
		if len(yt.Requests()) != before {
			t.Error("it sent a request")
		}
	})

	t.Run("given more issues than --limit, it asks for exactly the limit", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedIssues(50)
		mustRun(t, env, "issue", "list")
		reqs := requests(yt, "GET", "/api/issues")
		if len(reqs) != 1 || query(t, reqs[0]).Get("$top") != "30" || query(t, reqs[0]).Get("$skip") != "0" {
			t.Errorf("requests = %v", reqs)
		}
		if n := strings.Count(env.Stdout.String(), "\n"); n != 30 {
			t.Errorf("printed %d rows, want the default limit 30", n)
		}

		env.Reset()
		mustRun(t, env, "issue", "list", "-L", "5", "--json", "idReadable", "--jq", "length")
		if env.Stdout.String() != "5\n" || query(t, yt.LastRequest(t)).Get("$top") != "5" {
			t.Errorf("stdout = %q, query = %s", env.Stdout, yt.LastRequest(t).RawQuery)
		}
	})

	t.Run("given --limit 0, it pages through every issue", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedIssues(250)
		mustRun(t, env, "issue", "list", "--limit", "0", "--json", "idReadable", "--jq", "[length, .[249].idReadable]")
		if env.Stdout.String() != "[250,\"APP-250\"]\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		var pages []string
		for _, r := range requests(yt, "GET", "/api/issues") {
			q := query(t, r)
			pages = append(pages, q.Get("$skip")+"/"+q.Get("$top"))
		}
		if want := []string{"0/100", "100/100", "200/100"}; !reflect.DeepEqual(pages, want) {
			t.Errorf("pages ($skip/$top) = %v, want %v", pages, want)
		}
	})

	t.Run("given a server that caps $top, it keeps paging up to the limit", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedIssues(250)
		yt.MaxTop = 42
		mustRun(t, env, "issue", "list", "--limit", "100", "--json", "idReadable", "--jq", "[length, .[99].idReadable]")
		if env.Stdout.String() != "[100,\"APP-100\"]\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		var pages []string
		for _, r := range requests(yt, "GET", "/api/issues") {
			q := query(t, r)
			pages = append(pages, q.Get("$skip")+"/"+q.Get("$top"))
		}
		if want := []string{"0/100", "42/58", "84/16"}; !reflect.DeepEqual(pages, want) {
			t.Errorf("pages ($skip/$top) = %v, want %v", pages, want)
		}
	})

	t.Run("given a negative --limit, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "list", "--limit", "-1"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given no matches, it says so on stderr and exits 0", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.Issues = nil
		mustRun(t, env, "issue", "list")
		if env.Stdout.Len() != 0 || !strings.Contains(env.Stderr.String(), "no issues match your search") {
			t.Errorf("stdout = %q, stderr = %q", env.Stdout, env.Stderr)
		}
	})

	t.Run("given a rejected token, it exits 4", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		t.Setenv("YTRACK_TOKEN", "perm-wrong")
		if code := env.Run("issue", "list"); code != 4 {
			t.Errorf("exit %d, want 4: %s", code, env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "HTTP 401") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})
}

func TestIssueView(t *testing.T) {
	t.Run("given an ID, it prints the header, key fields and plain-text description", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "view", "NSR-40")

		reqs := requests(yt, "GET", "/api/issues/NSR-40")
		if len(reqs) != 1 {
			t.Fatalf("requests = %v", reqs)
		}
		if got, want := query(t, reqs[0]).Get("fields"), "idReadable,summary,description,project(shortName,name),reporter(login,fullName),created,updated,resolved,tags(name),commentsCount,customFields(name,value(name,login,fullName,minutes,presentation,text))"; got != want {
			t.Errorf("fields = %q\nwant     %q", got, want)
		}
		want := `NSR-40: Login redirect drops the return URL
Project:    NSR (Nightshift)
Reporter:   Jane Roe (jroe)
Created:    2026-09-21T14:13:20Z
Updated:    2026-10-06T15:20:00Z
Tags:       backend, sso
Priority:   Major
Type:       Bug
State:      In Progress
Assignee:   John Doe (jdoe)
Subsystem:  Auth, Web
Fix versions: 2026.3
Estimation: 1d 4h 30m
Due Date:   2026-10-07
Story points: 5
Root cause: Session cookie set before redirect
Sentiment:  {"$type":"Mood"}

After SSO login the user lands on the dashboard.

Expected: back to the page they came from.

3 comments; use --comments to view them.
View this issue on YouTrack: ` + yt.URL() + `/issue/NSR-40
`
		if env.Stdout.String() != want {
			t.Errorf("stdout:\n%s\nwant:\n%s", env.Stdout, want)
		}
	})

	t.Run("given --comments, it fetches and shows only the latest --comments-limit", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "view", "NSR-40", "--comments", "--comments-limit", "2")
		reqs := requests(yt, "GET", "/api/issues/NSR-40/comments")
		if len(reqs) != 1 {
			t.Fatalf("requests = %v", reqs)
		}
		q := query(t, reqs[0])
		if q.Get("$skip") != "1" || q.Get("$top") != "2" || q.Get("fields") != "id,text,author(login,fullName),created,updated" {
			t.Errorf("comments query = %s", reqs[0].RawQuery)
		}
		out := env.Stdout.String()
		want := `Comments (latest 2 of 3):

John Doe (jdoe) • 2026-10-06T15:20:00Z
  The return URL is lost
  in the SAML relay state.

John Doe (jdoe) • 2026-10-07T08:00:00Z
  Fix is up for review.
`
		if !strings.Contains(out, want) || strings.Contains(out, "Reproduced on staging") {
			t.Errorf("stdout:\n%s", out)
		}
	})

	t.Run("given an issue without comments or description, it says so", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		mustRun(t, env, "issue", "view", "NSR-41", "--comments")
		out := env.Stdout.String()
		for _, want := range []string{"Resolved:   2026-10-07T05:13:20Z\n", "\nNo description provided.\n", "\nNo comments.\n"} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "Assignee") {
			t.Errorf("an empty Assignee is shown:\n%s", out)
		}
	})

	t.Run("given an issue URL with a slug under a path prefix, it views that issue", func(t *testing.T) {
		env, yt := loggedIn(t, "/youtrack")
		mustRun(t, env, "issue", "view", yt.URL()+"/issue/NSR-40/login-redirect-drops-the-return-url", "--json", "idReadable")
		if got := yt.LastRequest(t).Path; got != "/youtrack/api/issues/NSR-40" {
			t.Errorf("path = %s", got)
		}
		if env.Stdout.String() != `{"idReadable":"NSR-40"}`+"\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given an issue URL on another host than --host, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "view", "https://other.youtrack.cloud/issue/NSR-40", "--host", "acme.youtrack.cloud"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given something that is not an issue, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		for _, arg := range []string{"NSR", "40", "https://acme.youtrack.cloud/projects/NSR"} {
			env.Reset()
			if code := env.Run("issue", "view", arg); !env.IsUsageError(code) {
				t.Errorf("%s: exit %d: %s", arg, code, env.Stderr)
			}
		}
	})

	t.Run("given --json with comments, it returns every comment with its URL", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "summary,comments")
		if got := query(t, requests(yt, "GET", "/api/issues/NSR-40")[0]).Get("fields"); got != "idReadable,summary" {
			t.Errorf("fields = %q", got)
		}
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		comments, _ := got["comments"].([]any)
		if len(got) != 2 || got["summary"] != "Login redirect drops the return URL" || len(comments) != 3 {
			t.Fatalf("got %v", got)
		}
		want := map[string]any{
			"id": "4-2", "text": "The return URL is lost\nin the SAML relay state.",
			"author":  map[string]any{"login": "jdoe", "fullName": "John Doe"},
			"created": "2026-10-06T15:20:00Z", "updated": "2026-10-07T05:13:20Z",
			"url": yt.URL() + "/issue/NSR-40#focus=Comments-4-2.0-0",
		}
		if !reflect.DeepEqual(comments[1], want) {
			t.Errorf("comment = %v\nwant      %v", comments[1], want)
		}
	})

	t.Run("given --web, it opens the issue in the browser without calling the API", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		before := len(yt.Requests())
		mustRun(t, env, "issue", "view", "NSR-40", "--web")
		if want := []string{yt.URL() + "/issue/NSR-40"}; !reflect.DeepEqual(env.Browser.URLs, want) {
			t.Errorf("browsed %v, want %v", env.Browser.URLs, want)
		}
		if len(yt.Requests()) != before || env.Stdout.Len() != 0 {
			t.Errorf("requests %d -> %d, stdout %q", before, len(yt.Requests()), env.Stdout)
		}
	})

	t.Run("given an unknown issue, it exits 1 saying the issue was not found", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("issue", "view", "NOPE-1"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if want := "ytrack: issue NOPE-1 not found on " + yt.Key() + " (or you cannot see it)\n"; env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})

	t.Run("given a rejected token, it exits 4", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		t.Setenv("YTRACK_TOKEN", "perm-wrong")
		if code := env.Run("issue", "view", "NSR-40"); code != 4 {
			t.Errorf("exit %d, want 4: %s", code, env.Stderr)
		}
	})
}

func TestIssueComment(t *testing.T) {
	posted := func(t *testing.T, yt *cmdtest.FakeYouTrack) []cmdtest.Request {
		t.Helper()
		return requests(yt, "POST", "/api/issues/NSR-40/comments")
	}

	t.Run("given --body, it posts the text and prints the comment URL", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", "NSR-40", "--body", "Reproduced on 2026.2")
		reqs := posted(t, yt)
		if len(reqs) != 1 || reqs[0].Body != `{"text":"Reproduced on 2026.2"}` || reqs[0].Header.Get("Content-Type") != "application/json" {
			t.Fatalf("requests = %+v", reqs)
		}
		if got := query(t, reqs[0]).Get("fields"); got != "id,issue(idReadable)" {
			t.Errorf("fields = %q", got)
		}
		if want := yt.URL() + "/issue/NSR-40#focus=Comments-4-103.0-0\n"; env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("given --body-file -, it reads the comment from stdin", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Stdin.WriteString("line one\nline two\n")
		mustRun(t, env, "issue", "comment", "NSR-40", "--body-file", "-")
		if reqs := posted(t, yt); len(reqs) != 1 || reqs[0].Body != `{"text":"line one\nline two\n"}` {
			t.Errorf("requests = %+v", reqs)
		}
	})

	t.Run("given --body-file with a path, it reads the comment from the file", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		path := filepath.Join(t.TempDir(), "note.md")
		if err := os.WriteFile(path, []byte("From a *file*"), 0o600); err != nil {
			t.Fatal(err)
		}
		mustRun(t, env, "issue", "comment", "NSR-40", "-F", path)
		if reqs := posted(t, yt); len(reqs) != 1 || reqs[0].Body != `{"text":"From a *file*"}` {
			t.Errorf("requests = %+v", reqs)
		}
	})

	stubEditor := func(t *testing.T, script string) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "editor.sh")
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"YTRACK_EDITOR", "GIT_EDITOR", "VISUAL"} {
			t.Setenv(k, "")
		}
		t.Setenv("EDITOR", path)
	}

	t.Run("given --editor, it posts what the editor saved", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		stubEditor(t, `printf 'Written in the editor\n\n' > "$1"`)
		mustRun(t, env, "issue", "comment", "NSR-40", "--editor", "--json", "id,url,issue")
		if reqs := posted(t, yt); len(reqs) != 1 || reqs[0].Body != `{"text":"Written in the editor"}` {
			t.Errorf("requests = %+v", reqs)
		}
		want := `{"id":"4-103","issue":"NSR-40","url":"` + yt.URL() + `/issue/NSR-40#focus=Comments-4-103.0-0"}` + "\n"
		if env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("given an editor that saves nothing, it posts nothing and exits 1", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		stubEditor(t, `: > "$1"`)
		if code := env.Run("issue", "comment", "NSR-40", "--editor"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if len(posted(t, yt)) != 0 || !strings.Contains(env.Stderr.String(), "the comment is empty") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a failing editor, it posts nothing and exits 1", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		stubEditor(t, `exit 3`)
		if code := env.Run("issue", "comment", "NSR-40", "--editor"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if len(posted(t, yt)) != 0 || !strings.Contains(env.Stderr.String(), "exited with status 3") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given an issue URL, it comments on that issue", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", yt.URL()+"/issue/NSR-40", "-b", "via URL", "--json", "id")
		if len(posted(t, yt)) != 1 || env.Stdout.String() != `{"id":"4-103"}`+"\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given no body when not interactive, it is a usage error", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("issue", "comment", "NSR-40"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if len(posted(t, yt)) != 0 {
			t.Error("it posted a comment")
		}
	})

	t.Run("given two body sources, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "comment", "NSR-40", "--body", "a", "--body-file", "-"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given an unknown issue, it exits 1 saying the issue was not found", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("issue", "comment", "NOPE-1", "--body", "x"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if !strings.Contains(env.Stderr.String(), "issue NOPE-1 not found on "+yt.Key()) {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a rejected token, it exits 4 before opening the editor", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		marker := filepath.Join(t.TempDir(), "opened")
		stubEditor(t, `touch "`+marker+`"`)
		t.Setenv("YTRACK_TOKEN", "")
		t.Setenv("YTRACK_HOST", "nobody.youtrack.cloud")
		if code := env.Run("issue", "comment", "NSR-40", "--editor"); code != 4 {
			t.Errorf("exit %d, want 4: %s", code, env.Stderr)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Error("the editor opened")
		}
	})

	t.Run("given a token the server rejects, it exits 4", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		t.Setenv("YTRACK_TOKEN", "perm-wrong")
		if code := env.Run("issue", "comment", "NSR-40", "--body", "x"); code != 4 {
			t.Errorf("exit %d, want 4: %s", code, env.Stderr)
		}
	})
}
