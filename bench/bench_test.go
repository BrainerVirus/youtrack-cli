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
		"attach":   {calls("issue attach APP-123 crash.log"), calls("issue comment APP-123 --body log --attach crash.log"), ""},
		"api-read": {[]Call{{Argv: []string{"api", "/issues", "-X", "GET", "-f", "query=project: APP timeout", "--fields", "idReadable,summary"}}}, calls("issue list -q timeout"), "APP-401 APP-405"},
		"help-first": {
			calls("issue view APP-123 --comments --help", "issue view APP-123 --comments --comments-limit 12"),
			calls("issue view APP-123 --comments --limit 12", "issue view --help", "issue view APP-123 --comments --comments-limit 12"), "",
		},
	} {
		c := cases[id]
		if c == nil {
			t.Fatalf("no case %s", id)
		}
		t.Run(id, func(t *testing.T) {
			if f := Grade(c, tc.good, nil, tc.answer); len(f) != 0 {
				t.Errorf("taught calls fail: %v", f)
			}
			if f := Grade(c, tc.bad, nil, tc.answer); len(f) == 0 {
				t.Error("the trap passes")
			}
		})
	}
	if len(cases) != 10 {
		t.Errorf("%d cases, the table covers 10", len(cases))
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
		{"it refuses read parameters without -X GET", []string{"api", "/issues", "-f", "query=x"}, 1, "add -X GET"},
		{"it answers a GET from the case fixture", []string{"api", "/issues", "-X", "GET", "-f", "query=x", "--fields", "idReadable"}, 0, "APP-401"},
		{"it links by phrase", []string{"issue", "link", "APP-1", "APP-2", "--type", "Depends On"}, 0, "APP-1 depends on APP-2"},
		{"it refuses a directed type name", []string{"issue", "link", "APP-1", "APP-2", "--type", "Depend"}, 1, "is directed"},
		{"it refuses days in a work item", []string{"work-item", "add", "APP-1", "--duration", "1d"}, 1, "days and weeks"},
		{"it fails on a missing attachment", []string{"issue", "attach", "APP-1", "a.txt", "nope.log"}, 1, "nope.log"},
		{"it attaches existing files", []string{"issue", "attach", "APP-1", "a.txt"}, 0, "a.txt\t8-120"},
		{"it refuses an unknown --json field like ytrack", []string{"issue", "view", "APP-123", "--json", "bogus"}, 1, "unknown JSON field"},
		{"it keeps every --json field", []string{"issue", "view", "APP-123", "--json", "state,assignee"}, 0, `"fullName": "Jane Doe"`},
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
