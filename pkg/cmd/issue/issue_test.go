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

const (
	editorEnv       = "YTRACK_TEST_EDITOR_TEXT"
	editorMarkerEnv = "YTRACK_TEST_EDITOR_MARKER"
)

// TestMain lets the test binary act as a portable stub editor: run with
// YTRACK_TEST_EDITOR_TEXT set, it writes that text to the file named by its
// last argument (or exits 3 for "fail") instead of running tests.
func TestMain(m *testing.M) {
	if text, ok := os.LookupEnv(editorEnv); ok && len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-test.") {
		if marker := os.Getenv(editorMarkerEnv); marker != "" {
			_ = os.WriteFile(marker, nil, 0o600)
		}
		if text == "fail" {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Args[len(os.Args)-1], []byte(text), 0o600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

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
		if got := q["customFields"]; !reflect.DeepEqual(got, []string{"State", "Priority", "Assignee"}) {
			t.Errorf("customFields = %v, want only State, Priority and Assignee", got)
		}
		want := "NSR-40\tLogin redirect drops the return URL\tIn Progress\tMajor\tjdoe\t2026-10-06T15:20:00Z\n" +
			"NSR-41\tDark mode toggle\tFixed\tNormal\t\t2026-10-07T05:13:20Z\n"
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
		if got, want := q.Get("fields"), "customFields(name,projectCustomField(field(fieldType(id))),value(name,login,fullName,minutes,presentation,text))"; got != want {
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
			"Deployed":     {"name": "Deployed", "kind": "datetime", "value": "2026-10-07T12:00:00Z"},
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
		if got, want := query(t, reqs[0]).Get("fields"), "idReadable,summary,description,project(shortName,name),reporter(login,fullName),created,updated,resolved,tags(name),commentsCount,customFields(name,projectCustomField(field(fieldType(id))),value(name,login,fullName,minutes,presentation,text))"; got != want {
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
Deployed:   2026-10-07T12:00:00Z
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
		if q.Get("$skip") != "1" || q.Get("$top") != "2" || q.Get("fields") != "id,text,author(login,fullName),created,updated,deleted" {
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

	// stubEditor makes this test binary the editor (see TestMain): it writes
	// text to the file it is given, or exits with status 3 for "fail".
	stubEditor := func(t *testing.T, text string) {
		t.Helper()
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"YTRACK_EDITOR", "GIT_EDITOR", "VISUAL"} {
			t.Setenv(k, "")
		}
		t.Setenv("EDITOR", exe)
		t.Setenv(editorEnv, text)
	}

	t.Run("given --editor, it posts what the editor saved", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		stubEditor(t, "Written in the editor\n\n")
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
		stubEditor(t, "")
		if code := env.Run("issue", "comment", "NSR-40", "--editor"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if len(posted(t, yt)) != 0 || !strings.Contains(env.Stderr.String(), "the comment is empty") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a failing editor, it posts nothing and exits 1", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		stubEditor(t, "fail")
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
		stubEditor(t, "text")
		t.Setenv(editorMarkerEnv, marker)
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

func TestIssueURLHost(t *testing.T) {
	url := func(yt *cmdtest.FakeYouTrack) string { return yt.URL() + "/issue/NSR-40" }

	t.Run("given YTRACK_HOST naming another host than the URL, it is a usage error", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		t.Setenv("YTRACK_HOST", "acme.youtrack.cloud")
		before := len(yt.Requests())
		if code := env.Run("issue", "view", url(yt)); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "but YTRACK_HOST is acme.youtrack.cloud") || len(yt.Requests()) != before {
			t.Errorf("stderr = %q, requests %d -> %d", env.Stderr, before, len(yt.Requests()))
		}
	})

	t.Run("given YTRACK_TOKEN and a URL on a host that is not logged in, it refuses to send the token", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		other := cmdtest.NewFakeYouTrack(t, "")
		other.SeedSampleIssues()
		t.Setenv("YTRACK_TOKEN", other.Token)
		for _, args := range [][]string{{"issue", "view", url(other)}, {"issue", "comment", url(other), "--body", "x"}} {
			env.Reset()
			if code := env.Run(args...); code != 1 {
				t.Errorf("%v: exit %d, want 1", args, code)
			}
			if !strings.Contains(env.Stderr.String(), "refusing to send YTRACK_TOKEN to "+other.Key()) {
				t.Errorf("stderr = %q", env.Stderr)
			}
		}
		if n := len(other.Requests()); n != 0 {
			t.Errorf("the URL's host received %d requests", n)
		}
	})

	t.Run("given YTRACK_TOKEN and --host naming the URL's host, it sends the token there", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		other := cmdtest.NewFakeYouTrack(t, "")
		other.SeedSampleIssues()
		t.Setenv("YTRACK_TOKEN", other.Token)
		mustRun(t, env, "issue", "view", url(other), "--host", other.URL(), "--json", "idReadable")
		if env.Stdout.String() != `{"idReadable":"NSR-40"}`+"\n" || len(other.Requests()) != 1 {
			t.Errorf("stdout = %q, requests = %d", env.Stdout, len(other.Requests()))
		}
	})

	t.Run("given YTRACK_TOKEN and a URL on the logged-in host, it uses it", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		t.Setenv("YTRACK_TOKEN", yt.Token)
		mustRun(t, env, "issue", "view", url(yt), "--json", "idReadable")
	})

	t.Run("given a URL on a host without credentials, it exits 4 without contacting it", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		other := cmdtest.NewFakeYouTrack(t, "")
		if code := env.Run("issue", "view", url(other)); code != 4 {
			t.Errorf("exit %d, want 4: %s", code, env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "not logged in to "+other.Key()) || len(other.Requests()) != 0 {
			t.Errorf("stderr = %q, requests = %d", env.Stderr, len(other.Requests()))
		}
	})

	t.Run("given --web and a URL on a host that is not logged in, it opens the URL's host", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		other := cmdtest.NewFakeYouTrack(t, "")
		t.Setenv("YTRACK_TOKEN", "anything")
		mustRun(t, env, "issue", "view", url(other), "--web")
		if want := []string{url(other)}; !reflect.DeepEqual(env.Browser.URLs, want) {
			t.Errorf("browsed %v", env.Browser.URLs)
		}
	})
}

func TestTerminalSafety(t *testing.T) {
	hostile := func(yt *cmdtest.FakeYouTrack) {
		is := yt.Issues[0]
		is["summary"] = "Evil\x1b]0;pwned\x07 \x1b[31mred\ttab"
		is["description"] = "\x1b]8;;https://evil.example\x07link\x1b]8;;\x07\r\nline2\u009b2J"
		cf := is["customFields"].([]any)[1].(map[string]any) // Type
		cf["value"].(map[string]any)["name"] = "Bug\x1b[2J\nInjected: yes"
		yt.Comments["NSR-40"][2]["text"] = "ok\x1b]52;c;Y2xpcA==\x07 done"
	}
	controls := func(s string) []rune {
		var bad []rune
		for _, r := range s {
			if r != '\n' && r != '\t' && (r < 0x20 || (r >= 0x7f && r <= 0x9f)) {
				bad = append(bad, r)
			}
		}
		return bad
	}

	t.Run("the list table strips control characters and turns tabs into spaces", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		hostile(yt)
		mustRun(t, env, "issue", "list")
		first := strings.SplitN(env.Stdout.String(), "\n", 2)[0]
		if bad := controls(env.Stdout.String()); len(bad) != 0 {
			t.Errorf("control characters %q in %q", bad, env.Stdout)
		}
		if cells := strings.Split(first, "\t"); len(cells) != 6 || cells[1] != "Evil]0;pwned [31mred tab" {
			t.Errorf("row = %q", first)
		}
	})

	t.Run("the issue view strips control characters but keeps line breaks", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		hostile(yt)
		mustRun(t, env, "issue", "view", "NSR-40", "--comments")
		out := env.Stdout.String()
		if bad := controls(out); len(bad) != 0 {
			t.Errorf("control characters %q in:\n%s", bad, out)
		}
		for _, want := range []string{
			"NSR-40: Evil]0;pwned [31mred tab\n",
			"\n]8;;https://evil.examplelink]8;;\nline22J\n",
			"Type:       Bug[2J Injected: yes\n",
			"  ok]52;c;Y2xpcA== done\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("JSON output keeps the text exactly, escaped by the encoder", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		hostile(yt)
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "summary")
		if got := decodeJSON[map[string]string](t, env.Stdout.Bytes())["summary"]; got != "Evil\x1b]0;pwned\x07 \x1b[31mred\ttab" {
			t.Errorf("summary = %q", got)
		}
		if strings.ContainsRune(env.Stdout.String(), 0x1b) {
			t.Error("raw ESC in JSON output")
		}
	})
}

func TestIssueReviewFixes(t *testing.T) {
	t.Run("given -q that looks like jq together with --json, it suggests --jq", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		for _, q := range []string{".[].idReadable", "map(.id) | length", "#Unresolved []"} {
			env.Reset()
			if code := env.Run("issue", "list", "--json", "idReadable", "-q", q); !env.IsUsageError(code) || !strings.Contains(env.Stderr.String(), "did you mean --jq?") {
				t.Errorf("%q: exit %d: %s", q, code, env.Stderr)
			}
		}
		env.Reset()
		mustRun(t, env, "issue", "list", "--json", "idReadable", "-q", "project: NSR")
	})

	t.Run("given a flag value with a brace, it is a usage error", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		before := len(yt.Requests())
		if code := env.Run("issue", "list", "--state", "Open} or {Fixed"); !env.IsUsageError(code) || len(yt.Requests()) != before {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("deleted comments are left out", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.Comments["NSR-40"][1]["deleted"] = true
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "comments", "--jq", "[.comments[].id]")
		if env.Stdout.String() != `["4-1","4-3"]`+"\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given --json comments with --comments-limit, it returns only the latest", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "comments", "--comments-limit", "1", "--jq", "[.comments[].id]")
		if env.Stdout.String() != `["4-3"]`+"\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		q := query(t, requests(yt, "GET", "/comments")[0])
		if q.Get("$skip") != "2" || q.Get("$top") != "1" {
			t.Errorf("comments query = %v", q)
		}
	})

	t.Run("given a renamed state field, list and view both report no state", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		cf := yt.Issues[0]["customFields"].([]any)[2].(map[string]any)
		cf["name"] = "Stage"
		mustRun(t, env, "issue", "list", "--json", "state", "--limit", "1")
		list := env.Stdout.String()
		env.Reset()
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "state")
		if list != `[{"state":""}]`+"\n" || env.Stdout.String() != `{"state":""}`+"\n" {
			t.Errorf("list %q, view %q", list, env.Stdout)
		}
	})

	t.Run("given a failed post after editing, it keeps the text in a file", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		exe, _ := os.Executable()
		for _, k := range []string{"YTRACK_EDITOR", "GIT_EDITOR", "VISUAL"} {
			t.Setenv(k, "")
		}
		t.Setenv("EDITOR", exe)
		t.Setenv(editorEnv, "A long careful comment")
		if code := env.Run("issue", "comment", "NOPE-1", "--editor"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		_, rest, ok := strings.Cut(env.Stderr.String(), "your text is saved in ")
		path, _, _ := strings.Cut(rest, "\n")
		if !ok {
			t.Fatalf("stderr = %q", env.Stderr)
		}
		t.Cleanup(func() { _ = os.Remove(path) })
		if b, err := os.ReadFile(path); err != nil || string(b) != "A long careful comment" {
			t.Errorf("draft = %q, %v", b, err)
		}
	})
}
