package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/root"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// The fake ytrack is this program run under the name ytrack, first on the
// model's PATH. It parses argv with ytrack's real command tree, so unknown
// commands and flags fail and --help prints the real help, but it never runs
// a command: replies come from the case's fixtures and built-in defaults, and
// any HTTP request fails. Every call is logged to the harness for grading.

const fakeHost = "acme.youtrack.cloud"

// harnessLayout: <harness>/bin/ytrack is the fake, <harness>/case.json its
// fixtures, <harness>/calls.jsonl its log.
func fakeMain(args []string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ytrack (bench fake):", err)
		return 1
	}
	harness := filepath.Dir(filepath.Dir(exe))
	b, err := os.ReadFile(filepath.Join(harness, "case.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ytrack (bench fake): no case:", err)
		return 1
	}
	var c Case
	if err := json.Unmarshal(b, &c); err != nil {
		fmt.Fprintln(os.Stderr, "ytrack (bench fake):", err)
		return 1
	}
	stdin, _ := readStdinIfPiped()
	cwd, _ := os.Getwd()
	out, errOut, code := fakeYtrack(&c, args, cwd, stdin)
	// A write changes the fixtures, so a later read shows it.
	if after, err := json.Marshal(&c); err == nil && !bytes.Equal(after, b) {
		_ = os.WriteFile(filepath.Join(harness, "case.json"), after, 0o600)
	}
	fmt.Fprint(os.Stdout, out)
	fmt.Fprint(os.Stderr, errOut)
	logCall(filepath.Join(harness, "calls.jsonl"), Call{Argv: args, Exit: code})
	return code
}

func readStdinIfPiped() (string, error) {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return "", err
	}
	if st.Mode()&os.ModeNamedPipe == 0 && st.Size() == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(os.Stdin)
	return buf.String(), err
}

func logCall(path string, c Call) {
	b, _ := json.Marshal(c)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

type noNetwork struct{}

func (noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("bench fake: no network (%s %s)", r.Method, r.URL.Host)
}

// parsed is a call the real command tree accepted.
type parsed struct {
	path  string // "issue view"
	args  []string
	flags map[string][]string
}

// flag is a flag's value; a list flag such as --json fields comes back
// comma-joined, as it was written.
func (p parsed) flag(name string) string { return strings.Join(p.flags[name], ",") }

func (p parsed) has(name string) bool { _, ok := p.flags[name]; return ok }

// fakeYtrack answers one call: stdout, stderr and the exit code.
func fakeYtrack(c *Case, args []string, cwd, stdin string) (string, string, int) {
	ios, _, out, errOut := iostreams.Test()
	f := &cmdutil.Factory{
		IOStreams:    ios,
		AppVersion:   "dev",
		Config:       func() (*config.Config, error) { return nil, errors.New("bench fake: no config") },
		RoundTripper: noNetwork{},
	}
	cmd := root.NewCmdRoot(f)
	var hit *parsed
	stubRuns(cmd, &hit)
	cmd.SetArgs(args)
	ran, err := cmd.ExecuteC()
	if err != nil {
		msg := "ytrack: " + err.Error() + "\n"
		if ran != nil {
			msg += fmt.Sprintf("Run '%s --help' for usage.\n", ran.CommandPath())
		}
		return out.String(), errOut.String() + msg, 1
	}
	if hit == nil { // help, --version or a command group
		return out.String(), errOut.String(), 0
	}
	line := Call{Argv: args}.Line()
	for _, r := range c.Fixtures.Rules {
		if regexp.MustCompile(r.Match).MatchString(line) {
			return r.Stdout, r.Stderr, r.Exit
		}
	}
	rOut, rErr, code := reply(c, *hit, cwd, stdin)
	if code == 0 {
		remember(c, *hit, stdin)
	}
	return rOut, rErr, code
}

// remember applies a successful write to the fixtures: fields, assignee and
// summary (issue edit, issue command), comments, links and work items.
func remember(c *Case, p parsed, stdin string) {
	if len(p.args) == 0 || p.has("dry-run") || p.has("list") || p.has("list-types") {
		return
	}
	issue := c.Fixtures.Issues[issueID(p.args[0])]
	if issue == nil {
		return
	}
	switch p.path {
	case "issue edit":
		for _, fv := range p.flags["field"] {
			name, value, _ := strings.Cut(fv, "=")
			setField(issue, name, value)
		}
		if p.has("assignee") {
			setField(issue, "Assignee", p.flag("assignee"))
		}
		if p.has("summary") {
			issue["summary"] = p.flag("summary")
		}
	case "issue command":
		text := p.args[len(p.args)-1]
		for _, name := range append(fieldNames(issue), "Assignee") {
			if v, ok := strings.CutPrefix(strings.ToLower(text), strings.ToLower(name)+" "); ok {
				setField(issue, name, text[len(text)-len(v):])
				break
			}
		}
	case "issue comment":
		body := p.flag("body")
		if p.flag("body-file") == "-" {
			body = stdin
		}
		comments, _ := issue["comments"].([]any)
		issue["comments"] = append(comments, map[string]any{"id": "4-99", "author": map[string]any{"login": "jdoe", "fullName": "Jane Doe"}, "text": body})
	case "issue link":
		links, _ := issue["links"].([]any)
		issue["links"] = append(links, map[string]any{"phrase": strings.ToLower(p.flag("type")), "issues": p.args[1:]})
	case "work-item add":
		items, _ := issue["workItems"].([]any)
		issue["workItems"] = append(items, map[string]any{"id": "115-3", "date": "today", "duration": map[string]any{"presentation": p.flag("duration")}, "text": p.flag("text")})
	}
}

func fieldNames(issue map[string]any) []string {
	var names []string
	fields, _ := issue["customFields"].([]any)
	for _, f := range fields {
		if m, ok := f.(map[string]any); ok {
			names = append(names, str(m["name"]))
		}
	}
	return names
}

func setField(issue map[string]any, name, value string) {
	var v any = value
	if strings.EqualFold(name, "Assignee") {
		switch value {
		case "":
			v = nil
		case "me", "jdoe":
			v = map[string]any{"login": "jdoe", "fullName": "Jane Doe"}
		default:
			v = map[string]any{"login": value, "fullName": value}
		}
	}
	key := strings.ToLower(name)
	if _, ok := issue[key]; ok {
		issue[key] = v
	}
	fields, _ := issue["customFields"].([]any)
	for _, f := range fields {
		if m, ok := f.(map[string]any); ok && strings.EqualFold(str(m["name"]), name) {
			m["value"] = v
			return
		}
	}
	issue["customFields"] = append(fields, map[string]any{"name": name, "value": v})
}

// stubRuns replaces every leaf command's run with one that records the
// parse. Groups keep theirs (help, or an unknown-command error), and PreRunE
// stays, so an unknown --json field fails as in ytrack.
func stubRuns(cmd *cobra.Command, hit **parsed) {
	if !cmd.HasSubCommands() && (cmd.Run != nil || cmd.RunE != nil) {
		cmd.Run = nil
		cmd.RunE = func(c *cobra.Command, args []string) error {
			p := parsed{path: strings.TrimPrefix(c.CommandPath(), "ytrack "), args: args, flags: map[string][]string{}}
			c.Flags().Visit(func(fl *pflag.Flag) {
				if sv, ok := fl.Value.(pflag.SliceValue); ok {
					p.flags[fl.Name] = sv.GetSlice()
				} else {
					p.flags[fl.Name] = []string{fl.Value.String()}
				}
			})
			*hit = &p
			return nil
		}
	}
	for _, sub := range cmd.Commands() {
		stubRuns(sub, hit)
	}
}

func fail(format string, a ...any) (string, string, int) {
	return "", "ytrack: " + fmt.Sprintf(format, a...) + "\n", 1
}

// reply is the built-in answer of a parsed call that no fixture rule matched.
func reply(c *Case, p parsed, cwd, stdin string) (string, string, int) {
	switch p.path {
	case "auth status":
		return fakeHost + "\n  ✓ Logged in to " + fakeHost + " as jdoe (Jane Doe)\n  - Active host: yes (the default host)\n", "", 0
	case "version":
		return "ytrack version dev\n", "", 0
	case "issue view":
		return viewIssue(c, p)
	case "issue list":
		return listIssues(c, p)
	case "issue comment":
		return comment(p, cwd, stdin)
	case "issue attach":
		return attach(p, cwd)
	case "issue edit":
		return edit(p)
	case "issue create":
		if p.flag("project") == "" || p.flag("summary") == "" {
			return fail("--project and --summary are required when not running interactively")
		}
		return "https://" + fakeHost + "/issue/APP-130\n", "✓ Created APP-130\n", 0
	case "issue command":
		return command(p)
	case "issue link":
		return link(c, p)
	case "issue unlink":
		return "", "✓ Removed the link\n", 0
	case "issue attachments":
		return "", "", 0
	case "work-item add":
		return addWorkItem(p)
	case "work-item list":
		items, _ := c.Fixtures.Issues[issueID(p.args[0])]["workItems"].([]any)
		if p.has("json") {
			if items == nil {
				items = []any{}
			}
			return toJSON(items), "", 0
		}
		if len(items) == 0 {
			return "", "no work items\n", 0
		}
		return toJSON(items), "", 0
	case "work-item edit", "work-item delete":
		return "", "✓ Done\n", 0
	case "api":
		return api(p)
	}
	return "", "", 0
}

func issueID(arg string) string {
	arg = strings.TrimRight(arg, "/")
	return arg[strings.LastIndex(arg, "/")+1:]
}

func pick(obj map[string]any, fields string) map[string]any {
	out := map[string]any{}
	for _, f := range strings.Split(fields, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out[f] = obj[f]
		}
	}
	return out
}

func toJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b) + "\n"
}

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case map[string]any:
		for _, k := range []string{"name", "fullName", "login", "presentation"} {
			if s, ok := t[k].(string); ok {
				return s
			}
		}
	}
	return fmt.Sprint(v)
}

func viewIssue(c *Case, p parsed) (string, string, int) {
	id := issueID(p.args[0])
	issue, ok := c.Fixtures.Issues[id]
	for _, is := range c.Fixtures.List {
		if !ok && str(is["idReadable"]) == id {
			issue, ok = is, true
		}
	}
	if !ok {
		return fail("issue %s not found on %s", id, fakeHost)
	}
	if p.has("json") {
		return toJSON(pick(issue, p.flag("json"))), "", 0
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", id, str(issue["summary"]))
	fmt.Fprintf(&b, "State: %s  Assignee: %s  Priority: %s\n\n%s\n", str(issue["state"]), str(issue["assignee"]), str(issue["priority"]), str(issue["description"]))
	if p.has("comments") {
		comments, _ := issue["comments"].([]any)
		limit := 10
		if v := p.flag("comments-limit"); v != "" {
			_, _ = fmt.Sscan(v, &limit)
		}
		if len(comments) > limit {
			comments = comments[len(comments)-limit:]
		}
		for _, cm := range comments {
			m, _ := cm.(map[string]any)
			fmt.Fprintf(&b, "\n%s (%s):\n%s\n", str(m["author"]), str(m["created"]), str(m["text"]))
		}
	}
	return b.String(), "", 0
}

func listIssues(c *Case, p parsed) (string, string, int) {
	if len(c.Fixtures.List) == 0 {
		return "", "no issues match\n", 0
	}
	if p.has("json") {
		rows := make([]map[string]any, len(c.Fixtures.List))
		for i, is := range c.Fixtures.List {
			rows[i] = pick(is, p.flag("json"))
		}
		return toJSON(rows), "", 0
	}
	var b strings.Builder
	for _, is := range c.Fixtures.List {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", str(is["idReadable"]), str(is["state"]), str(is["assignee"]), str(is["summary"]))
	}
	return b.String(), "", 0
}

func missingFile(cwd string, names ...string) string {
	for _, n := range names {
		if n == "" || n == "-" {
			continue
		}
		if !filepath.IsAbs(n) {
			n = filepath.Join(cwd, n)
		}
		if _, err := os.Stat(n); err != nil {
			return filepath.Base(n)
		}
	}
	return ""
}

func comment(p parsed, cwd, stdin string) (string, string, int) {
	if !p.has("body") && !p.has("body-file") {
		return fail("add the comment text with --body or --body-file")
	}
	if p.flag("body-file") == "-" && strings.TrimSpace(stdin) == "" {
		return fail("the comment is empty")
	}
	if f := missingFile(cwd, append([]string{p.flag("body-file")}, p.flags["attach"]...)...); f != "" {
		return fail("open %s: no such file or directory; nothing was posted", f)
	}
	return fmt.Sprintf("https://%s/issue/%s#focus=Comments-4-17.0-0\n", fakeHost, issueID(p.args[0])), "", 0
}

func attach(p parsed, cwd string) (string, string, int) {
	if len(p.args) < 2 {
		return fail("name at least one file to attach")
	}
	if f := missingFile(cwd, p.args[1:]...); f != "" {
		return fail("open %s: no such file or directory; nothing was attached", f)
	}
	var b strings.Builder
	for i, f := range p.args[1:] {
		fmt.Fprintf(&b, "%s\t8-%d\n", filepath.Base(f), 120+i)
	}
	return b.String(), "", 0
}

func edit(p parsed) (string, string, int) {
	if len(p.flags) == 0 {
		return fail("nothing to change: pass --summary, --description, --field, --assignee or a tag flag")
	}
	for _, fv := range p.flags["field"] {
		if !strings.Contains(fv, "=") {
			return fail("--field %q: want Name=Value", fv)
		}
	}
	id := issueID(p.args[0])
	return fmt.Sprintf("%s\thttps://%s/issue/%s\n", id, fakeHost, id), "✓ Updated " + id + "\n", 0
}

func command(p parsed) (string, string, int) {
	cmdText := p.args[len(p.args)-1]
	if p.has("dry-run") {
		return fmt.Sprintf("%s: understood\n", cmdText), "", 0
	}
	return "", fmt.Sprintf("✓ Applied %q to %s\n", cmdText, strings.Join(p.args[:len(p.args)-1], ", ")), 0
}

type linkType struct{ name, outward, inward string }

var linkTypes = []linkType{
	{"Depend", "depends on", "is required for"},
	{"Subtask", "parent for", "subtask of"},
	{"Duplicate", "duplicates", "is duplicated by"},
	{"Relates", "relates to", "relates to"},
}

func link(c *Case, p parsed) (string, string, int) {
	if p.has("list-types") {
		var b strings.Builder
		b.WriteString("NAME\tOUTWARD\tINWARD\n")
		for _, t := range linkTypes {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", t.name, t.outward, t.inward)
		}
		return b.String(), "", 0
	}
	if p.has("list") {
		issue := c.Fixtures.Issues[issueID(p.args[0])]
		links, _ := issue["links"].([]any)
		if len(links) == 0 {
			return "", issueID(p.args[0]) + " has no links\n", 0
		}
		if p.has("json") {
			return toJSON(links), "", 0
		}
		var b strings.Builder
		for _, l := range links {
			m, _ := l.(map[string]any)
			fmt.Fprintf(&b, "%s\t%v\n", m["phrase"], m["issues"])
		}
		return b.String(), "", 0
	}
	if len(p.args) < 2 || !p.has("type") {
		return fail("give the issue, the issues to link and --type <phrase>")
	}
	want := strings.ToLower(strings.TrimSpace(p.flag("type")))
	for _, t := range linkTypes {
		if want == t.outward || want == t.inward || (t.outward == t.inward && want == strings.ToLower(t.name)) {
			return "", fmt.Sprintf("✓ %s %s %s\n", issueID(p.args[0]), want, strings.Join(p.args[1:], ", ")), 0
		}
		if want == strings.ToLower(t.name) {
			return fail("link type %s is directed: pass one of its phrases, %q or %q", t.name, t.outward, t.inward)
		}
	}
	return fail("no link type has the phrase %q; run 'ytrack issue link --list-types'", p.flag("type"))
}

var daysOrWeeks = regexp.MustCompile(`(?i)\d\s*(d|w|day|days|week|weeks)\b`)

func addWorkItem(p parsed) (string, string, int) {
	d := p.flag("duration")
	if d == "" {
		return fail("--duration is required")
	}
	if daysOrWeeks.MatchString(d) {
		return fail("--duration %q: days and weeks are refused; use hours and minutes", d)
	}
	id := issueID(p.args[0])
	return "115-3\n", fmt.Sprintf("✓ Logged %s on %s for today: https://%s/issue/%s\n", d, id, fakeHost, id), 0
}

var readParams = []string{"query", "fields", "customFields", "$top", "$skip"}

func api(p parsed) (string, string, int) {
	params := slices.Concat(p.flags["raw-field"], p.flags["field"])
	method := strings.ToUpper(p.flag("method"))
	if method == "" {
		method = http.MethodGet
		if len(params) > 0 || p.has("input") {
			onlyRead := !p.has("input")
			for _, kv := range params {
				k, _, _ := strings.Cut(kv, "=")
				onlyRead = onlyRead && slices.Contains(readParams, k)
			}
			if onlyRead {
				return fail("-f/-F without -X sends a POST, which creates or changes data (a POST to /issues creates an issue); " +
					"these parameters only read: add -X GET to read, or -X POST to send the POST")
			}
			method = http.MethodPost
		}
	}
	if p.has("paginate") && method != http.MethodGet {
		return fail("--paginate only works with GET requests")
	}
	if method == http.MethodGet {
		return "[]\n", "", 0
	}
	return "{\"$type\":\"Ok\"}\n", "", 0
}
