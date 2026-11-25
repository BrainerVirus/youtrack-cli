package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadAll(t *testing.T) map[string]*Case {
	t.Helper()
	cases, err := loadCases("cases", nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	return byID
}

func calls(lines ...string) []Call {
	out := make([]Call, len(lines))
	for i, l := range lines {
		out[i] = Call{Argv: strings.Fields(l)}
	}
	return out
}

// Each case's rules accept the call sequence the skill teaches and reject
// the mistake the case is about; a regex typo fails here, not in a model run.
func TestGivenEachCaseWhenGradedItPassesTheTaughtCallsAndFailsTheTrap(t *testing.T) {
	cases := loadAll(t)
	for id, tc := range map[string]struct {
		good, bad []Call
		answer    string
	}{
		"view-json": {calls("issue view APP-123 --json state,assignee"), calls("issue view APP-123"), "In Progress, Jane Doe"},
		"search-query": {
			[]Call{{Argv: []string{"issue", "list", "-q", "project: APP tag: regression #Unresolved"}}},
			calls("issue list --project APP --state open"), "APP-201, APP-207",
		},
		"my-issues-es": {
			calls("issue list --project APP --assignee me"),
			[]Call{{Argv: []string{"issue", "list", "-q", "project: APP for: me"}}},
			"APP-310 and APP-315",
		},
		"comment": {
			[]Call{{Argv: []string{"issue", "comment", "APP-123", "--body", "Reproduced on 2026.2 with Safari 19."}}},
			[]Call{{Argv: []string{"issue", "comment", "APP-123", "--body", "Reproduced on 2026.2"}}, {Argv: []string{"issue", "comment", "APP-123", "--body", "Reproduced on 2026.2"}}},
			"",
		},
		"log-time":     {calls("work-item add APP-123 --duration=1h30m --text review"), calls("issue command APP-123 work_1h30m"), ""},
		"set-field-es": {calls("issue edit APP-123 --field Priority=Critical"), calls("issue edit APP-123 --field Priority=Critical", "issue command APP-123 Priority_Critical"), ""},
		"link-es": {
			[]Call{{Argv: []string{"issue", "link", "APP-7", "APP-3", "--type", "subtask of"}}},
			[]Call{{Argv: []string{"issue", "link", "APP-3", "APP-7", "--type", "subtask of"}}},
			"",
		},
		"meeting-time": {
			[]Call{{Argv: []string{"work-item", "add", "APP-123", "--duration", "1h 30m", "--meeting"}}},
			[]Call{{Argv: []string{"work-item", "add", "APP-123", "--duration", "1h30m", "--type", "Meeting"}}},
			"",
		},
		"create-stdin": {
			[]Call{{Argv: []string{"issue", "create", "-p", "APP", "-s", "Files in the export folder", "--description-file", "-"}}},
			[]Call{{Argv: []string{"issue", "create", "-p", "APP", "-s", "Files", "--description", "a.csv b.csv"}}},
			"",
		},
		"attach":   {calls("issue attach APP-123 crash.log"), calls("issue comment APP-123 --body log --attach crash.log"), ""},
		"api-read": {[]Call{{Argv: []string{"api", "/issues", "-X", "GET", "-f", "query=project: APP timeout", "--fields", "idReadable,summary"}}}, calls("issue list -q timeout"), "APP-401 APP-405"},
		"help-first": {
			calls("issue view APP-123 --comments --help", "issue view APP-123 --comments --comments-limit 12"),
			[]Call{{Argv: strings.Fields("issue view APP-123 --comments --limit 12"), Exit: 1}, {Argv: strings.Fields("issue view --help")}, {Argv: strings.Fields("issue view APP-123 --comments --comments-limit 12")}},
			"",
		},
		"comment-delete": {
			calls("issue comment list APP-123 --json id,text", "issue comment delete APP-123 4-2 --yes"),
			calls("issue comment list APP-123", "issue comment delete APP-123 4-3 --yes"),
			"",
		},
		"time-report": {
			calls("work-item report --since 2026-11-23 --json minutes,issues"),
			calls("work-item list APP-123 --author me --since 2026-11-23", "work-item list APP-7 --author me --since 2026-11-23"),
			"Total 9h 30m: APP-123 5h 30m, APP-7 4h.",
		},
		"count-open": {
			[]Call{{Argv: []string{"issue", "count", "project: APP #Unresolved"}}},
			calls("issue list --project APP --limit 0 --json idReadable"),
			"137",
		},
		"create-field": {
			[]Call{{Argv: strings.Fields("project fields APP")}, {Argv: []string{"issue", "create", "-p", "APP", "-s", "Export times out", "--field", "Severity=Show-stopper"}}},
			[]Call{{Argv: []string{"issue", "create", "-p", "APP", "-s", "Export times out", "--field", "Severity=Critical"}, Exit: 1}, {Argv: strings.Fields("project fields APP")}, {Argv: []string{"issue", "create", "-p", "APP", "-s", "Export times out", "--field", "Severity=Show-stopper"}}},
			"",
		},
		"tag-create": {
			[]Call{{Argv: strings.Fields("issue edit APP-123 --add-tag safari-19"), Exit: 1}, {Argv: strings.Fields("tag create safari-19")}, {Argv: strings.Fields("issue edit APP-123 --add-tag safari-19")}},
			[]Call{{Argv: strings.Fields("tag create safari-19")}, {Argv: []string{"issue", "command", "APP-123", "tag safari-19"}}},
			"",
		},
		"command-assist": {
			[]Call{{Argv: []string{"issue", "command", "APP-3", "APP-7", "State Ver", "--assist"}}, {Argv: []string{"issue", "command", "APP-3", "APP-7", "State Verify on staging"}}},
			[]Call{{Argv: []string{"issue", "command", "APP-3", "APP-7", "State Verified"}, Exit: 1}, {Argv: []string{"issue", "command", "APP-3", "APP-7", "State Ver", "--assist"}}, {Argv: []string{"issue", "command", "APP-3", "APP-7", "State Verify on staging"}}},
			"",
		},
	} {
		c := cases[id]
		if c == nil {
			t.Fatalf("no case %s", id)
		}
		// Shell commands the taught run typed, for rules on commands.
		goodCmds := map[string][]string{
			"create-stdin": {"ls -1 ./export | ytrack issue create -p APP -s 'Files in the export folder' --description-file -"},
		}[id]
		t.Run(id, func(t *testing.T) {
			if f := Grade(c, tc.good, goodCmds, tc.answer); len(f) != 0 {
				t.Errorf("taught calls fail: %v", f)
			}
			if f := Grade(c, tc.bad, nil, tc.answer); len(f) == 0 {
				t.Error("the trap passes")
			}
		})
	}
	t.Run("meeting-time takes the call's own date as today, whatever the month", func(t *testing.T) {
		c := cases["meeting-time"]
		day := "2031-02-03"
		ok := []Call{{Argv: strings.Fields("work-item add APP-123 --duration 90m --meeting --date " + day), Day: day}}
		if f := Grade(c, ok, nil, ""); len(f) != 0 {
			t.Errorf("--date today fails: %v", f)
		}
		other := []Call{{Argv: strings.Fields("work-item add APP-123 --duration 90m --meeting --date 2031-02-02"), Day: day}}
		if f := Grade(c, other, nil, ""); len(f) == 0 {
			t.Error("another date passes")
		}
	})
	t.Run("create-stdin needs the ls output piped in", func(t *testing.T) {
		c := cases["create-stdin"]
		call := []Call{{Argv: strings.Fields("issue create -p APP -s Files --description-file -")}}
		for _, cmd := range []string{
			"echo hi | ytrack issue create -p APP -s Files --description-file -",
			"ls; echo hi | ytrack issue create -p APP -s Files --description-file -",
			"ls && echo hi | ytrack issue create -p APP -s Files --description-file -",
		} {
			if f := Grade(c, call, []string{cmd}, ""); len(f) == 0 {
				t.Errorf("a description that is not the ls output passes: %s", cmd)
			}
		}
	})
	if len(cases) != 18 {
		t.Errorf("%d cases, the table covers 18", len(cases))
	}
}

func TestGivenHelpFirstOnlyTheFlagInTheTextViewPasses(t *testing.T) {
	c := loadAll(t)["help-first"]
	if f := Grade(c, calls("issue view APP-123 --comments --comments-limit 12"), nil, ""); len(f) != 0 {
		t.Errorf("knew the flag, no guess: %v", f)
	}
	if f := Grade(c, calls("issue comment list APP-123 --limit 12"), nil, ""); len(f) != 0 {
		t.Errorf("the comment list text view fails: %v", f)
	}
	for _, bad := range [][]Call{
		calls("issue view APP-123 --json summary,comments"),
		calls("issue view APP-123 --comments"),
	} {
		if f := Grade(c, bad, nil, ""); len(f) != 1 || f[0] != "require: 12 comments in a text view" {
			t.Errorf("%v: %v", bad, f)
		}
	}
}

func TestGivenARequiredCallThatFailedItDoesNotCount(t *testing.T) {
	c := &Case{Require: []Rule{{Name: "view", Match: []string{"^issue view"}}}}
	if f := Grade(c, []Call{{Argv: []string{"issue", "view", "APP-1", "--bogus"}, Exit: 1}}, nil, ""); len(f) != 1 {
		t.Errorf("failures %v, want the require rule", f)
	}
}

func TestGivenTheFakeYtrack(t *testing.T) {
	c := loadAll(t)["api-read"]
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		args     []string
		code     int
		contains string
	}{
		{"it prints the real help", []string{"issue", "view", "--help"}, 0, "--comments-limit"},
		{"it refuses an unknown flag like ytrack", []string{"issue", "view", "APP-1", "--hostname", "x"}, 1, "unknown flag: --hostname"},
		{"it refuses an unknown command", []string{"issue", "close", "APP-1"}, 1, "ytrack issue --help"},
		{"it answers a command that also has subcommands", []string{"issue", "comment", "APP-9", "--body", "hi"}, 0, "Comments-4-17"},
		{"it refuses read parameters without -X GET", []string{"api", "/issues", "-f", "query=x"}, 1, "add -X GET"},
		{"it answers a GET from the case fixture", []string{"api", "/issues", "-X", "GET", "-f", "query=x", "--fields", "idReadable"}, 0, "APP-401"},
		{"it links by phrase", []string{"issue", "link", "APP-1", "APP-2", "--type", "Depends On"}, 0, "APP-1 depends on APP-2"},
		{"it refuses a directed type name", []string{"issue", "link", "APP-1", "APP-2", "--type", "Depend"}, 1, "is directed"},
		{"it refuses days in a work item", []string{"work-item", "add", "APP-1", "--duration", "1d"}, 1, "days and weeks"},
		{"it fails on a missing attachment", []string{"issue", "attach", "APP-1", "a.txt", "nope.log"}, 1, "nope.log"},
		{"it attaches existing files", []string{"issue", "attach", "APP-1", "a.txt"}, 0, "a.txt\t8-120"},
		{"it refuses an unknown --json field like ytrack", []string{"issue", "view", "APP-123", "--json", "bogus"}, 1, "unknown JSON field"},
		{"it keeps every --json field", []string{"issue", "view", "APP-123", "--json", "state,assignee"}, 0, `"fullName": "Jane Doe"`},
		{"it applies --jq like ytrack", []string{"issue", "view", "APP-123", "--json", "comments", "--jq", ".comments | length"}, 0, "14"},
		{"it takes --jq alone as every field", []string{"issue", "view", "APP-123", "--jq", ".assignee.login"}, 0, "jdoe"},
		{"it fails a bad --jq", []string{"issue", "view", "APP-123", "--jq", ".["}, 1, "ytrack: "},
		{"it applies --jq to an api fixture", []string{"api", "/issues", "-X", "GET", "-f", "query=x", "--jq", ".[].idReadable"}, 0, "APP-405"},
		{"it filters --json fields", []string{"issue", "view", "https://acme.youtrack.cloud/issue/APP-123", "--json", "state"}, 0, `"state": "In Progress"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := fakeYtrack(c, tc.args, dir, "")
			if code != tc.code || !strings.Contains(out+errOut, tc.contains) {
				t.Errorf("exit %d, output %q; want exit %d containing %q", code, out+errOut, tc.code, tc.contains)
			}
		})
	}
}

func TestGivenAListedIssueTheFakeViewsIt(t *testing.T) {
	c := loadAll(t)["my-issues-es"]
	out, errOut, code := fakeYtrack(c, []string{"issue", "view", "APP-315", "--json", "summary,state"}, t.TempDir(), "")
	if code != 0 || !strings.Contains(out, "Dark mode") || !strings.Contains(out, "In Progress") {
		t.Errorf("exit %d, %s%s", code, out, errOut)
	}
}

func TestGivenAWriteTheFakeShowsItOnTheNextRead(t *testing.T) {
	c := loadAll(t)["set-field-es"]
	for _, args := range [][]string{
		{"issue", "edit", "APP-123", "--field", "Priority=Critical"},
		{"issue", "command", "APP-123", "State Fixed"},
	} {
		if _, errOut, code := fakeYtrack(c, args, t.TempDir(), ""); code != 0 {
			t.Fatalf("%v: exit %d %s", args, code, errOut)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crash.log"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := fakeYtrack(c, []string{"issue", "attach", "APP-123", "crash.log"}, dir, ""); code != 0 {
		t.Fatal(errOut)
	}
	for _, args := range [][]string{{"issue", "attachments", "APP-123"}, {"api", "/issues/APP-123/attachments", "--fields", "name"}} {
		if out, _, _ := fakeYtrack(c, args, dir, ""); !strings.Contains(out, "crash.log") {
			t.Errorf("%v after the attach = %s", args, out)
		}
	}
	out, _, _ := fakeYtrack(c, []string{"issue", "view", "APP-123", "--json", "customFields"}, t.TempDir(), "")
	if !strings.Contains(out, `"Critical"`) || !strings.Contains(out, `"Fixed"`) {
		t.Errorf("view after the writes = %s", out)
	}
	if out, _, _ := fakeYtrack(c, []string{"issue", "view", "APP-123"}, t.TempDir(), ""); !strings.Contains(out, "Priority: Critical") {
		t.Errorf("text view after the edit = %s", out)
	}
}

func TestGivenAResultTheScrubberRemovesPathsAndTokens(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	world := filepath.Join(home, "tmp", "ytrack-bench-1")
	sc := newScrubber(world)
	in := world + "/project/crash.log " + home + "/.config perm:abc.def.ghijklmnop Bearer abcdefghijkl sk-ant-abcdefghijkl"
	got := sc.scrub(in)
	if want := "<world>/project/crash.log ~/.config <token> <token> <token>"; got != want {
		t.Errorf("scrub = %q, want %q", got, want)
	}
	if sc.check(got) != nil {
		t.Error("check refuses scrubbed text")
	}
	if sc.check(in) == nil {
		t.Error("check lets a home path through")
	}
}

func TestGivenDenialsOnlyAPlainYtrackDenialIsHostPolicy(t *testing.T) {
	if !hostBlocked([]string{"ytrack issue comment APP-1 --body 'hi'"}) {
		t.Error("a plain ytrack denial is not host policy")
	}
	if hostBlocked([]string{"ytrack issue list | python3 -c x", "curl https://x"}) {
		t.Error("a denial of a pipe or curl is the bench's own allow list, not host policy")
	}
}

func TestGivenAStreamJSONTranscriptItReadsCommandsUsageAndTheAnswer(t *testing.T) {
	stream := `{"type":"system","subtype":"init","skills":["ytrack","debug"],"plugins":[{"name":"cc-plugin-x"}]}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"ytrack"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ytrack issue view APP-1"}}]}}
{"type":"result","subtype":"success","is_error":false,"num_turns":3,"total_cost_usd":0.01,"result":"Done","usage":{"input_tokens":5,"output_tokens":7,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000},"permission_denials":[{"tool_name":"Bash","tool_input":{"command":"curl x"}}]}
`
	tr, err := parseTranscript([]byte(stream))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.done || !tr.skillLoaded || !tr.skillListed || tr.workitPlugin || tr.turns != 3 || tr.answer != "Done" || tr.usage.TotalInput() != 1105 ||
		len(tr.commands) != 1 || len(tr.denials) != 1 || tr.denials[0] != "curl x" {
		t.Errorf("transcript = %+v", tr)
	}
}

func TestGivenARunThatNeverLoadedTheSkillItIsNotGraded(t *testing.T) {
	c := loadAll(t)["comment"]
	cr := newCaseResult(c, "m", "k", []RunResult{{Run: 1, Status: "pass"}, {Run: 2, Status: "error", Reason: notLoaded}})
	s := summarize("m", "h", 1, []CaseResult{cr})
	if cr.Graded != 1 || s.Cases[0].NotLoaded != 1 || s.Cases[0].Errors != 1 {
		t.Errorf("graded %d, row %+v", cr.Graded, s.Cases[0])
	}
}

func TestGivenRunsTheCasePassesOnTwoOfThree(t *testing.T) {
	for _, tc := range []struct {
		passes, graded int
		want           bool
	}{{2, 3, true}, {1, 3, false}, {1, 1, true}, {0, 0, false}, {1, 2, false}} {
		if got := casePasses(tc.passes, tc.graded); got != tc.want {
			t.Errorf("casePasses(%d, %d) = %v", tc.passes, tc.graded, got)
		}
	}
}

func TestGivenTheNewCommandsTheFakeAnswersLikeYtrack(t *testing.T) {
	cases := loadAll(t)
	run := func(c *Case, args ...string) (string, int) {
		t.Helper()
		out, errOut, code := fakeYtrack(c, args, t.TempDir(), "")
		return out + errOut, code
	}
	c := cases["comment-delete"]
	if out, code := run(c, "issue", "comment", "list", "APP-123", "--json", "id,text"); code != 0 || !strings.Contains(out, `"id": "4-2"`) {
		t.Errorf("comment list = %d %s", code, out)
	}
	if out, code := run(c, "issue", "comment", "delete", "APP-123", "4-2"); code != 1 || !strings.Contains(out, "--yes") {
		t.Errorf("comment delete without --yes = %d %s", code, out)
	}
	if out, code := run(c, "issue", "comment", "delete", "APP-123", "4-99", "--yes"); code != 1 || !strings.Contains(out, "not found") {
		t.Errorf("comment delete of a missing ID = %d %s", code, out)
	}
	if _, code := run(c, "issue", "comment", "delete", "APP-123", "4-2", "--yes"); code != 0 {
		t.Fatal("comment delete --yes failed")
	}
	if out, _ := run(c, "issue", "comment", "list", "APP-123"); strings.Contains(out, "Cannot reproduce on Chrome") {
		t.Errorf("the deleted comment is still listed: %s", out)
	}

	c = cases["tag-create"]
	if out, code := run(c, "issue", "edit", "APP-123", "--add-tag", "safari-19"); code != 1 || !strings.Contains(out, "ytrack tag create safari-19") {
		t.Errorf("edit with a missing tag = %d %s", code, out)
	}
	if _, code := run(c, "tag", "create", "safari-19"); code != 0 {
		t.Fatal("tag create failed")
	}
	if out, code := run(c, "tag", "create", "Safari-19"); code != 1 || !strings.Contains(out, "already exists") {
		t.Errorf("a second tag create = %d %s", code, out)
	}
	if out, code := run(c, "issue", "edit", "APP-123", "--add-tag", "safari-19"); code != 0 {
		t.Errorf("edit after tag create = %d %s", code, out)
	}
	if out, _ := run(c, "tag", "list"); !strings.Contains(out, "safari-19") {
		t.Errorf("tag list = %s", out)
	}

	c = cases["command-assist"]
	if out, code := run(c, "issue", "command", "APP-3", "APP-7", "State Ver", "--assist"); code != 0 || !strings.Contains(out, "> State Verify on staging") {
		t.Errorf("assist = %d %s", code, out)
	}
	if _, code := run(c, "issue", "command", "APP-3", "APP-7", "State Verified"); code != 1 {
		t.Error("a guessed state applies")
	}
	if _, code := run(c, "issue", "command", "APP-3", "APP-7", "State Verify on staging"); code != 0 {
		t.Error("the state from the completions fails")
	}
	looked := []Call{{Argv: strings.Fields("project fields APP")}, {Argv: []string{"issue", "command", "APP-3", "APP-7", "State Verify on staging"}}}
	if f := Grade(c, looked, nil, ""); len(f) != 0 {
		t.Errorf("command-assist with the value from project fields: %v", f)
	}
	if out, code := run(cases["set-field-es"], "project", "fields", "APP", "--json", "name"); code != 0 || !strings.Contains(out, `"Priority"`) {
		t.Errorf("project fields without a fixture = %d %s", code, out)
	}
	for _, answer := range []string{"9h 30m in total", "You've logged **9 hours 30 minutes** so far"} {
		if f := Grade(cases["time-report"], calls("work-item report --since 2026-11-23"), nil, answer); len(f) != 0 {
			t.Errorf("time-report answer %q: %v", answer, f)
		}
	}
	if out, code := run(cases["count-open"], "issue", "count", "project: APP #Unresolved", "--json", "count", "--jq", ".count"); code != 0 || strings.TrimSpace(out) != "137" {
		t.Errorf("count --jq = %d %s", code, out)
	}
	if out, code := run(cases["time-report"], "work-item", "report", "--since", "2026-11-23", "--jq", ".duration"); code != 0 || !strings.Contains(out, "9h 30m") {
		t.Errorf("report --jq = %d %s", code, out)
	}
}
