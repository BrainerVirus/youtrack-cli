package issue_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func writable(t *testing.T) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
	t.Helper()
	env, yt := loggedIn(t, "")
	yt.SeedSampleProjects()
	return env, yt
}

// useStubEditor makes this test binary the editor (see TestMain). It returns
// the file where the editor copies the text it was opened with.
func useStubEditor(t *testing.T, text string) string {
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
	seen := filepath.Join(t.TempDir(), "initial")
	t.Setenv(editorMarkerEnv, seen)
	return seen
}

func bodyOf(t *testing.T, r cmdtest.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Body), &m); err != nil {
		t.Fatalf("body %q is not JSON: %v", r.Body, err)
	}
	return m
}

func lastPost(t *testing.T, yt *cmdtest.FakeYouTrack, pathSuffix string) map[string]any {
	t.Helper()
	reqs := requests(yt, "POST", pathSuffix)
	if len(reqs) == 0 {
		t.Fatalf("no POST %s", pathSuffix)
	}
	return bodyOf(t, reqs[len(reqs)-1])
}

// customField returns the stored custom field called name of an issue in
// the fake, as YouTrack would hold it.
func storedField(t *testing.T, yt *cmdtest.FakeYouTrack, id, name string) any {
	t.Helper()
	for _, is := range yt.Issues {
		if is["idReadable"] != id {
			continue
		}
		cfs, _ := is["customFields"].([]any)
		for _, cf := range cfs {
			if m := cf.(map[string]any); m["name"] == name {
				return m["value"]
			}
		}
		return nil
	}
	t.Fatalf("no issue %s in the fake", id)
	return nil
}

func TestIssueCreate(t *testing.T) {
	t.Run("given fields of several types, it writes each with its type and prints the new ID and URL", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "create", "--project", "nsr", "--summary", "  Crash on start ", "--description", "Steps",
			"--field", "Priority=critical", "--field", "Subsystem=Auth, web", "--field", "Due Date=2026-10-20",
			"--field", "Story points=5", "--field", "Deployed=2026-10-07T10:00:00Z", "--field", "Root cause=Null config",
			"--field", "Fix versions=", "--assignee", "me", "--tag", "Backend")

		body := lastPost(t, yt, "/api/issues")
		want := map[string]any{
			"project":     map[string]any{"id": "0-12"},
			"summary":     "Crash on start",
			"description": "Steps",
			"tags":        []any{map[string]any{"id": "6-1"}},
			"customFields": []any{
				map[string]any{"name": "Priority", "$type": "SingleEnumIssueCustomField", "value": map[string]any{"name": "Critical"}},
				map[string]any{"name": "Subsystem", "$type": "MultiOwnedIssueCustomField", "value": []any{map[string]any{"name": "Auth"}, map[string]any{"name": "Web"}}},
				map[string]any{"name": "Due Date", "$type": "DateIssueCustomField", "value": 1792497600000.0},
				map[string]any{"name": "Story points", "$type": "SimpleIssueCustomField", "value": 5.0},
				map[string]any{"name": "Deployed", "$type": "SimpleIssueCustomField", "value": 1791367200000.0},
				map[string]any{"name": "Root cause", "$type": "TextIssueCustomField", "value": map[string]any{"text": "Null config"}},
				map[string]any{"name": "Fix versions", "$type": "MultiVersionIssueCustomField", "value": []any{}},
				map[string]any{"name": "Assignee", "$type": "SingleUserIssueCustomField", "value": map[string]any{"login": "jdoe"}},
			},
		}
		if !reflect.DeepEqual(body, want) {
			gotJSON, _ := json.MarshalIndent(body, "", " ")
			t.Errorf("body =\n%s", gotJSON)
		}
		if got, want := env.Stdout.String(), "NSR-102\t"+yt.URL()+"/issue/NSR-102\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
		if v := storedField(t, yt, "NSR-102", "Priority"); !reflect.DeepEqual(v, map[string]any{"name": "Critical"}) {
			t.Errorf("stored Priority = %v", v)
		}
		firstPages := 0
		for _, r := range requests(yt, "GET", "/customFields") {
			if query(t, r).Get("$skip") == "0" {
				firstPages++
			}
		}
		if firstPages != 1 {
			t.Errorf("read the field definitions %d times, want once", firstPages)
		}
	})

	t.Run("given a period field, it creates the issue and then sets it with a command", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "create", "-p", "NSR", "-s", "Spike", "--field", "Estimation=1w 2d", "--field", "Type=Task")
		body := lastPost(t, yt, "/api/issues")
		if cfs := body["customFields"].([]any); len(cfs) != 1 || cfs[0].(map[string]any)["name"] != "Type" {
			t.Errorf("REST custom fields = %v, want only Type", cfs)
		}
		cmd := lastPost(t, yt, "/api/commands")
		if want := map[string]any{"query": "Estimation 1w2d", "issues": []any{map[string]any{"idReadable": "NSR-102"}}}; !reflect.DeepEqual(cmd, want) {
			t.Errorf("command body = %v", cmd)
		}
		if v := storedField(t, yt, "NSR-102", "Estimation"); v == nil {
			t.Error("Estimation was not set")
		}
	})

	t.Run("given an unknown field, it lists the project's fields and creates nothing", func(t *testing.T) {
		env, yt := writable(t)
		if code := env.Run("issue", "create", "-p", "NSR", "-s", "x", "--field", "Severity=High"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		errText := env.Stderr.String()
		if !strings.Contains(errText, `project NSR has no custom field "Severity"`) || !strings.Contains(errText, "\n  Priority\n") || !strings.Contains(errText, "\n  Root cause") {
			t.Errorf("stderr = %q", errText)
		}
		if len(requests(yt, "POST", "/api/issues")) != 0 {
			t.Error("it created an issue")
		}
	})

	t.Run("given an unknown or archived value, it lists the valid ones and creates nothing", func(t *testing.T) {
		env, yt := writable(t)
		for value, want := range map[string]string{
			"Urgent":       `invalid value "Urgent" for field "Priority"; valid values:` + "\n  Major\n  Critical\n  Normal\n  Minor\n",
			"show-stopper": `invalid value "show-stopper" for field "Priority": it is archived; valid values:`,
		} {
			env.Reset()
			if code := env.Run("issue", "create", "-p", "NSR", "-s", "x", "--field", "Priority="+value); code != 1 {
				t.Fatalf("%s: exit %d", value, code)
			}
			if !strings.Contains(env.Stderr.String(), want) || strings.Contains(env.Stderr.String(), "  Show-stopper") {
				t.Errorf("%s: stderr = %q", value, env.Stderr)
			}
		}
		env.Reset()
		if code := env.Run("issue", "create", "-p", "NSR", "-s", "x", "--assignee", "nobody"); code != 1 || !strings.Contains(env.Stderr.String(), "  me\n  jdoe\n  jroe\n") {
			t.Errorf("unknown user: exit %d, stderr = %q", code, env.Stderr)
		}
		if len(requests(yt, "POST", "/api/issues")) != 0 {
			t.Error("it created an issue")
		}
	})

	t.Run("given values of the wrong shape, it explains the expected format", func(t *testing.T) {
		env, _ := writable(t)
		for field, want := range map[string]string{
			"Story points=lots":    "expected a whole number",
			"Due Date=tomorrow":    "expected a date as YYYY-MM-DD",
			"Estimation=two weeks": "expected a duration such as 1w 2d 4h 30m",
			"Deployed=noon":        "expected a date and time",
		} {
			env.Reset()
			if code := env.Run("issue", "create", "-p", "NSR", "-s", "x", "--field", field); code != 1 || !strings.Contains(env.Stderr.String(), want) {
				t.Errorf("%s: exit %d, stderr = %q", field, code, env.Stderr)
			}
		}
	})

	t.Run("given an unknown or archived project, it lists the usable projects", func(t *testing.T) {
		env, _ := writable(t)
		for _, p := range []string{"NOPE", "OLD"} {
			env.Reset()
			if code := env.Run("issue", "create", "-p", p, "-s", "x"); code != 1 {
				t.Fatalf("exit %d", code)
			}
			if want := `unknown project "` + p + `"; projects you can use:` + "\n  NSR\n  OPS\n"; !strings.Contains(env.Stderr.String(), want) {
				t.Errorf("stderr = %q", env.Stderr)
			}
		}
	})

	t.Run("given an unknown tag, it says how to create it and creates nothing", func(t *testing.T) {
		env, yt := writable(t)
		if code := env.Run("issue", "create", "-p", "NSR", "-s", "x", "--tag", "nightly"); code != 1 || !strings.Contains(env.Stderr.String(), `unknown tag "nightly"`) {
			t.Errorf("exit %d, stderr = %q", code, env.Stderr)
		}
		if len(requests(yt, "POST", "/api/issues")) != 0 {
			t.Error("it created an issue")
		}
	})

	t.Run("given --description-file -, it reads the description from stdin", func(t *testing.T) {
		env, yt := writable(t)
		env.Stdin.WriteString("line one\nline two\n")
		mustRun(t, env, "issue", "create", "-p", "NSR", "-s", "From stdin", "--description-file", "-")
		if got := lastPost(t, yt, "/api/issues")["description"]; got != "line one\nline two\n" {
			t.Errorf("description = %q", got)
		}
	})

	t.Run("in a terminal, it asks for the project, summary and description", func(t *testing.T) {
		env, yt := writable(t)
		env.Interactive()
		useStubEditor(t, "Written in the editor\n\n")
		env.Stdin.WriteString("NSR\n\nPrompted summary\ny\n")
		mustRun(t, env, "issue", "create")
		body := lastPost(t, yt, "/api/issues")
		if body["summary"] != "Prompted summary" || body["description"] != "Written in the editor" || !reflect.DeepEqual(body["project"], map[string]any{"id": "0-12"}) {
			t.Errorf("body = %v", body)
		}
		for _, q := range []string{"? Project:", "? Summary:", "? Write a description in your editor?"} {
			if !strings.Contains(env.Stderr.String(), q) {
				t.Errorf("stderr lacks %q: %q", q, env.Stderr)
			}
		}
		if !strings.Contains(env.Stderr.String(), "Created NSR-102: Prompted summary") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("with --editor, it sends what the editor saved", func(t *testing.T) {
		env, yt := writable(t)
		env.Interactive()
		seen := useStubEditor(t, "From the editor")
		mustRun(t, env, "issue", "create", "-p", "NSR", "-s", "x", "--editor")
		if got := lastPost(t, yt, "/api/issues")["description"]; got != "From the editor" {
			t.Errorf("description = %q", got)
		}
		if b, err := os.ReadFile(seen); err != nil || len(b) != 0 {
			t.Errorf("the editor started with %q (%v)", b, err)
		}
	})

	t.Run("not in a terminal, --editor is a usage error and no editor opens", func(t *testing.T) {
		env, yt := writable(t)
		seen := useStubEditor(t, "text")
		before := len(yt.Requests())
		for _, args := range [][]string{{"issue", "create", "-p", "NSR", "-s", "x", "--editor"}, {"issue", "edit", "NSR-40", "--editor"}} {
			env.Reset()
			if code := env.Run(args...); !env.IsUsageError(code) || !strings.Contains(env.Stderr.String(), "`--editor` needs a terminal") {
				t.Errorf("%v: exit %d: %s", args[:2], code, env.Stderr)
			}
		}
		if _, err := os.Stat(seen); err == nil || len(yt.Requests()) != before {
			t.Error("the editor opened or requests were sent")
		}
	})

	t.Run("given a project key, it prefers an exact short name, then a short name, then a unique name", func(t *testing.T) {
		env, yt := writable(t)
		yt.Projects = append(yt.Projects,
			map[string]any{"$type": "Project", "id": "0-20", "shortName": "XYZ", "name": "NSR", "archived": false},
			map[string]any{"$type": "Project", "id": "0-21", "shortName": "nsr2", "name": "Shared", "archived": false},
			map[string]any{"$type": "Project", "id": "0-22", "shortName": "SHR", "name": "shared", "archived": false})
		yt.ProjectFields["0-20"] = []map[string]any{}
		for key, want := range map[string]string{"NSR": "0-12", "nsr": "0-12", "xyz": "0-20", "Nightshift": "0-12"} {
			env.Reset()
			mustRun(t, env, "issue", "create", "-p", key, "-s", "x")
			if got := lastPost(t, yt, "/api/issues")["project"]; !reflect.DeepEqual(got, map[string]any{"id": want}) {
				t.Errorf("-p %s: project = %v, want %s", key, got, want)
			}
		}
		env.Reset()
		if code := env.Run("issue", "create", "-p", "SHARED", "-s", "x"); code != 1 || !strings.Contains(env.Stderr.String(), `project "SHARED" is ambiguous`) {
			t.Errorf("ambiguous name: exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given a field that cannot be empty, an empty value is refused before sending", func(t *testing.T) {
		env, yt := writable(t)
		if code := env.Run("issue", "create", "-p", "NSR", "-s", "x", "--field", "Priority="); code != 1 || !strings.Contains(env.Stderr.String(), `field "Priority" cannot be empty`) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if len(requests(yt, "POST", "/api/issues")) != 0 {
			t.Error("it created an issue")
		}
	})

	t.Run("given a group field, it writes a group from the field's bundle", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "create", "-p", "NSR", "-s", "x", "--field", "Team=qa")
		cfs := lastPost(t, yt, "/api/issues")["customFields"]
		if want := []any{map[string]any{"name": "Team", "$type": "SingleGroupIssueCustomField", "value": map[string]any{"name": "QA"}}}; !reflect.DeepEqual(cfs, want) {
			t.Errorf("customFields = %v", cfs)
		}
		if v := storedField(t, yt, "NSR-102", "Team"); !reflect.DeepEqual(v, map[string]any{"name": "QA"}) {
			t.Errorf("stored Team = %v", v)
		}
		env.Reset()
		if code := env.Run("issue", "create", "-p", "NSR", "-s", "x", "--field", "Team=Ops"); code != 1 || !strings.Contains(env.Stderr.String(), "  Developers\n  QA\n") {
			t.Errorf("unknown group: exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("it reads a user field's users only when a login is being checked", func(t *testing.T) {
		env, yt := writable(t)
		userReads := func() int { return len(requests(yt, "GET", "/customFields/93-13")) }
		mustRun(t, env, "issue", "create", "-p", "NSR", "-s", "x", "--field", "Priority=Major", "--assignee", "me")
		if n := userReads(); n != 0 {
			t.Errorf("read the users %d times without a login to check", n)
		}
		for _, r := range requests(yt, "GET", "/customFields") {
			if strings.Contains(query(t, r).Get("fields"), "aggregatedUsers") {
				t.Errorf("the definitions request asks for users: %s", r.RawQuery)
			}
		}
		mustRun(t, env, "issue", "create", "-p", "NSR", "-s", "x", "--assignee", "JROE")
		if n := userReads(); n != 1 {
			t.Errorf("read the users %d times, want once", n)
		}
		if got := lastPost(t, yt, "/api/issues")["customFields"].([]any)[0].(map[string]any)["value"]; !reflect.DeepEqual(got, map[string]any{"login": "jroe"}) {
			t.Errorf("assignee = %v", got)
		}
	})

	t.Run("not in a terminal, a missing summary is a usage error and nothing is sent", func(t *testing.T) {
		env, yt := writable(t)
		before := len(yt.Requests())
		if code := env.Run("issue", "create", "-p", "NSR"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if len(yt.Requests()) != before {
			t.Error("it sent requests")
		}
	})

	t.Run("given --json, it prints the created issue's fields", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "create", "-p", "NSR", "-s", "JSON", "--field", "State=In progress", "--assignee", "jroe",
			"--field", "Estimation=3h", "--json", "idReadable,url,project,state,assignee,customFields")
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		if got["idReadable"] != "NSR-102" || got["url"] != yt.URL()+"/issue/NSR-102" || got["state"] != "In Progress" ||
			!reflect.DeepEqual(got["assignee"], map[string]any{"login": "jroe", "fullName": ""}) ||
			!reflect.DeepEqual(got["project"], map[string]any{"shortName": "NSR", "name": "Nightshift"}) {
			t.Errorf("stdout = %s", env.Stdout)
		}
		// The period was set by command after the POST, so the output is re-read.
		if !strings.Contains(env.Stdout.String(), `{"kind":"period","name":"Estimation","value":{"minutes":180,"presentation":"3h"}}`) {
			t.Errorf("Estimation missing from %s", env.Stdout)
		}
	})
}

func TestIssueEdit(t *testing.T) {
	updates := func(yt *cmdtest.FakeYouTrack) []cmdtest.Request {
		return requests(yt, "POST", "/api/issues/NSR-40")
	}

	t.Run("given only --summary, it sends only the summary", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "edit", "NSR-40", "--summary", "New title")
		reqs := updates(yt)
		if len(reqs) != 1 || reqs[0].Body != `{"summary":"New title"}` {
			t.Fatalf("updates = %+v", reqs)
		}
		if len(requests(yt, "GET", "/customFields")) != 0 {
			t.Error("it read the field definitions without --field")
		}
		if got, want := env.Stdout.String(), "NSR-40\t"+yt.URL()+"/issue/NSR-40\n"; got != want {
			t.Errorf("stdout = %q", got)
		}
	})

	t.Run("given --field and --assignee, it sends only those custom fields", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "edit", "NSR-40", "--field", "Fix versions=2026.3,2026.4", "--assignee", "")
		body := bodyOf(t, updates(yt)[0])
		want := map[string]any{"customFields": []any{
			map[string]any{"name": "Fix versions", "$type": "MultiVersionIssueCustomField", "value": []any{map[string]any{"name": "2026.3"}, map[string]any{"name": "2026.4"}}},
			map[string]any{"name": "Assignee", "$type": "SingleUserIssueCustomField", "value": nil},
		}}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("body = %v", body)
		}
		if storedField(t, yt, "NSR-40", "Assignee") != nil {
			t.Error("the issue is still assigned")
		}
	})

	t.Run("given --description \"\", it clears the description", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "edit", "NSR-40", "--description", "")
		if reqs := updates(yt); len(reqs) != 1 || reqs[0].Body != `{"description":""}` {
			t.Errorf("updates = %+v", reqs)
		}
	})

	t.Run("given --editor, it starts from the current description", func(t *testing.T) {
		env, yt := writable(t)
		env.Interactive()
		seen := useStubEditor(t, "Rewritten\n")
		mustRun(t, env, "issue", "edit", "NSR-40", "--editor")
		if b, _ := os.ReadFile(seen); !strings.HasPrefix(string(b), "After SSO login the user lands on the dashboard.") {
			t.Errorf("the editor started with %q", b)
		}
		if reqs := updates(yt); len(reqs) != 1 || reqs[0].Body != `{"description":"Rewritten"}` {
			t.Errorf("updates = %+v", reqs)
		}
	})

	t.Run("given tags to add and remove, it changes only the tags", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "edit", "NSR-40", "--add-tag", "regression", "--remove-tag", "SSO", "--json", "tags")
		if len(updates(yt)) != 0 {
			t.Error("it sent an issue update")
		}
		if add := requests(yt, "POST", "/api/issues/NSR-40/tags"); len(add) != 1 || add[0].Body != `{"id":"6-3"}` {
			t.Errorf("tag requests = %+v", add)
		}
		if del := requests(yt, "DELETE", "/api/issues/NSR-40/tags/6-2"); len(del) != 1 {
			t.Errorf("untag requests = %+v", del)
		}
		if got := env.Stdout.String(); got != `{"tags":["backend","regression"]}`+"\n" {
			t.Errorf("stdout = %s", got)
		}
	})

	t.Run("given nothing to change, it is a usage error", func(t *testing.T) {
		env, yt := writable(t)
		before := len(yt.Requests())
		if code := env.Run("issue", "edit", "NSR-40"); !env.IsUsageError(code) || !strings.Contains(env.Stderr.String(), "nothing to change") {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if len(yt.Requests()) != before {
			t.Error("it sent requests")
		}
	})

	t.Run("given an unknown issue, it says it was not found", func(t *testing.T) {
		env, yt := writable(t)
		if code := env.Run("issue", "edit", "NSR-999", "-s", "x"); code != 1 || !strings.Contains(env.Stderr.String(), "issue NSR-999 not found on "+yt.Key()) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})
}

func TestIssueCommand(t *testing.T) {
	t.Run("it applies the command and sends no comment or silent flag unless asked", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "command", "NSR-40", "State Fixed for me")
		reqs := requests(yt, "POST", "/api/commands")
		if len(reqs) != 1 || reqs[0].Body != `{"issues":[{"idReadable":"NSR-40"}],"query":"State Fixed for me"}` {
			t.Fatalf("requests = %+v", reqs)
		}
		if got := storedField(t, yt, "NSR-40", "State"); !reflect.DeepEqual(got, map[string]any{"$type": "BundleElement", "name": "Fixed"}) {
			t.Errorf("State = %v", got)
		}
		if got, want := env.Stdout.String(), "NSR-40\t"+yt.URL()+"/issue/NSR-40\n"; got != want {
			t.Errorf("stdout = %q", got)
		}
	})

	t.Run("given --comment and --silent, it sends both with the command", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "command", "NSR-40", "Priority Critical", "--comment", "Escalated", "--silent")
		body := lastPost(t, yt, "/api/commands")
		if body["comment"] != "Escalated" || body["silent"] != true {
			t.Errorf("body = %v", body)
		}
		if cs := yt.Comments["NSR-40"]; cs[len(cs)-1]["text"] != "Escalated" {
			t.Error("the comment was not added")
		}
	})

	t.Run("given --dry-run, it previews the parsed command and changes nothing", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "command", "NSR-40", "state fixed for me", "--dry-run")
		if n := len(requests(yt, "POST", "/api/commands")); n != 0 {
			t.Errorf("it applied the command (%d requests)", n)
		}
		assist := lastPost(t, yt, "/api/commands/assist")
		if assist["query"] != "state fixed for me" || assist["caret"] != 18.0 {
			t.Errorf("assist body = %v", assist)
		}
		if got, want := env.Stdout.String(), "  State: Fixed\n  Assignee: jdoe\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
		if got := storedField(t, yt, "NSR-40", "State"); got.(map[string]any)["name"] != "In Progress" {
			t.Errorf("State changed to %v", got)
		}
	})

	t.Run("given --dry-run and a command YouTrack cannot parse, it marks the bad part and exits 1", func(t *testing.T) {
		env, yt := writable(t)
		if code := env.Run("issue", "command", "NSR-40", "Frobnicate State Fixed", "--dry-run"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if got := env.Stdout.String(); got != "! Unknown command: Frobnicate\n  State: Fixed\n" {
			t.Errorf("stdout = %q", got)
		}
		if !strings.Contains(env.Stderr.String(), "nothing was changed") || len(requests(yt, "POST", "/api/commands")) != 0 {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given --dry-run, server text is printed without control characters", func(t *testing.T) {
		env, _ := writable(t)
		_ = env.Run("issue", "command", "NSR-40", "State \x1b]0;pwned\x07", "--dry-run")
		if strings.ContainsAny(env.Stdout.String()+env.Stderr.String(), "\x1b\x07") {
			t.Errorf("control characters in output: %q / %q", env.Stdout, env.Stderr)
		}
	})

	t.Run("given a command YouTrack rejects, it exits 1 with YouTrack's message", func(t *testing.T) {
		env, _ := writable(t)
		if code := env.Run("issue", "command", "NSR-40", "State Bogus"); code != 1 || !strings.Contains(env.Stderr.String(), "Command [State Bogus] is invalid") {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given --json, it reports what YouTrack parsed", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "command", "2-4040", "tag regression", "--json", "issue,url,command,dryRun,commands")
		if body := lastPost(t, yt, "/api/commands"); !reflect.DeepEqual(body["issues"], []any{map[string]any{"id": "2-4040"}}) {
			t.Errorf("a database ID was sent as %v", body["issues"])
		}
		want := map[string]any{
			"issue": "2-4040", "url": yt.URL() + "/issue/2-4040", "command": "tag regression", "dryRun": false,
			"commands": []any{map[string]any{"description": "Add tag regression", "error": false}},
		}
		if got := decodeJSON[map[string]any](t, env.Stdout.Bytes()); !reflect.DeepEqual(got, want) {
			t.Errorf("stdout = %s", env.Stdout)
		}
	})

	t.Run("given --dry-run with --silent, it is a usage error", func(t *testing.T) {
		env, _ := writable(t)
		if code := env.Run("issue", "command", "NSR-40", "State Fixed", "--dry-run", "--silent"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})
}

func TestIssueWriteHosts(t *testing.T) {
	t.Run("given a URL on a host that is not logged in, edit and command refuse without contacting it", func(t *testing.T) {
		env, _ := writable(t)
		other := cmdtest.NewFakeYouTrack(t, "")
		other.SeedSampleIssues()
		link := other.URL() + "/issue/NSR-40"
		for _, args := range [][]string{
			{"issue", "edit", link, "-s", "x"},
			{"issue", "command", link, "State Fixed"},
			{"issue", "command", link, "State Fixed", "--dry-run"},
		} {
			env.Reset()
			if code := env.Run(args...); code != 1 || !strings.Contains(env.Stderr.String(), "refusing to change an issue on "+other.Key()) {
				t.Errorf("%v: exit %d: %s", args[:2], code, env.Stderr)
			}
		}
		if n := len(other.Requests()); n != 0 {
			t.Errorf("the URL's host received %d requests", n)
		}
	})

	t.Run("given a URL on the logged-in host, it edits that issue", func(t *testing.T) {
		env, yt := writable(t)
		mustRun(t, env, "issue", "edit", yt.URL()+"/issue/NSR-40/some-slug", "-s", "Via URL")
		if reqs := requests(yt, "POST", "/api/issues/NSR-40"); len(reqs) != 1 {
			t.Errorf("updates = %d", len(reqs))
		}
	})

	t.Run("given --host naming the URL's host, the normal token rules apply", func(t *testing.T) {
		env, _ := writable(t)
		other := cmdtest.NewFakeYouTrack(t, "")
		other.SeedSampleIssues()
		other.SeedSampleProjects()
		t.Setenv("YTRACK_TOKEN", other.Token)
		mustRun(t, env, "issue", "command", other.URL()+"/issue/NSR-40", "State Fixed", "--host", other.URL())
		if !slices.ContainsFunc(other.Requests(), func(r cmdtest.Request) bool { return r.Path == "/api/commands" }) {
			t.Error("the command did not reach the named host")
		}
	})
}
