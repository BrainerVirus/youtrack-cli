package root_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/skill"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/root"
)

// The agent skill teaches ytrack's commands and flags by name; a renamed or
// removed one must fail here, not in an agent's session.
func TestSkillNamesOnlyRealCommandsAndFlags(t *testing.T) {
	tree := commandTree(t)
	checked := 0
	err := fs.WalkDir(skill.Files(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		b, err := fs.ReadFile(skill.Files(), p)
		if err != nil {
			return err
		}
		inv, problems := skillDrift(tree, string(b))
		checked += inv
		for _, problem := range problems {
			t.Errorf("skill %s: %s", p, problem)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 20 {
		t.Errorf("found only %d ytrack invocations in the skill; the scanner is broken", checked)
	}
}

func TestSkillDriftCatchesBogusNames(t *testing.T) {
	tree := commandTree(t)
	for text, want := range map[string]string{
		"Run `ytrack issue lst APP-1`.":                    `unknown command "lst" in "ytrack issue"`,
		"```sh\nytrack isue view APP-1\n```":               `unknown command "isue" in "ytrack"`,
		"Run `ytrack issue view APP-1 --hostname x`.":      `unknown flag --hostname for "ytrack issue view"`,
		"Run `ytrack work-item add APP-1 -z 1h`.":          `unknown flag -z for "ytrack work-item add"`,
		"```sh\nytrack issue list --json id --bogus\n```":  `unknown flag --bogus for "ytrack issue list"`,
		"Pass `--no-such-flag` to any command.":            `--no-such-flag is not a flag of any ytrack command`,
		"Use `ytrack auth status --host=a.example --jsn`.": `unknown flag --jsn for "ytrack auth status"`,
	} {
		t.Run(want, func(t *testing.T) {
			_, problems := skillDrift(tree, text)
			if len(problems) != 1 || problems[0] != want {
				t.Errorf("problems = %q, want [%q]", problems, want)
			}
		})
	}
	t.Run("gh commands, quoted values and flag values are not checked as ytrack", func(t *testing.T) {
		text := "`gh issue create --title x` and `ytrack issue list -q 'a --b #c' --json id --jq '.[] | .id'`\n" +
			"```sh\nytrack auth login --with-token < token.txt   # --not-checked\n```"
		if n, problems := skillDrift(tree, text); n != 2 || len(problems) != 0 {
			t.Errorf("invocations %d, problems %q", n, problems)
		}
	})
}

func TestAPIExamplesReadWithGET(t *testing.T) {
	tree := commandTree(t)
	api, _, err := tree.Find([]string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	// "  $ ytrack api ..." example lines, as code snippets.
	var help []string
	for _, l := range strings.Split(api.Example, "\n") {
		if _, cmd, ok := strings.Cut(l, "$ "); ok {
			help = append(help, cmd)
		}
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]string{"api --help": help, "README.md": codeSnippets(string(readme))}
	_ = fs.WalkDir(skill.Files(), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(skill.Files(), p)
			sources["skill "+p] = codeSnippets(string(b))
		}
		return err
	})
	total := 0
	for name, snippets := range sources {
		calls := apiInvocations(snippets)
		total += len(calls)
		for _, c := range calls {
			if p := apiMisuse(c); p != "" {
				t.Errorf("%s: %s", name, p)
			}
		}
	}
	if total < 10 {
		t.Errorf("found only %d ytrack api examples; the scanner is broken", total)
	}

	for line, want := range map[string]string{
		"ytrack api /issues -f query='project: APP' --paginate":    "ytrack api /issues: --paginate without -X GET is a POST, which --paginate refuses",
		"ytrack api /issues -f query='project: APP'":               `ytrack api /issues: read parameter "query" without -X GET is sent as a POST`,
		"ytrack api /api/issues -F '$top=5'":                       `ytrack api /api/issues: read parameter "$top" without -X GET is sent as a POST`,
		"ytrack api issues -f 'project[id]=0-0'":                   "ytrack api issues: parameters without -X GET POST to /issues, which creates an issue",
		"ytrack api /admin/projects --input q.json --paginate":     "ytrack api /admin/projects: --paginate without -X GET is a POST, which --paginate refuses",
		"ytrack api /issues -X GET -f query='x' --paginate":        "",
		"ytrack api /issues --method=get -f query='x'":             "",
		"ytrack api /issues -F 'project[id]=0-0' -f summary='New'": "",
		"ytrack api /issues/APP-1/comments -f text='hi'":           "",
		"ytrack api /issues/APP-1 -X DELETE":                       "",
	} {
		t.Run("given "+line, func(t *testing.T) {
			calls := apiInvocations([]string{line})
			if len(calls) != 1 {
				t.Fatalf("calls = %q", calls)
			}
			if got := apiMisuse(calls[0]); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func commandTree(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := root.NewCmdRoot(cmdtest.New(t).Factory)
	cmd.InitDefaultHelpCmd()
	cmd.InitDefaultCompletionCmd()
	return cmd
}

var (
	fence      = regexp.MustCompile("(?s)```[a-z]*\n(.*?)```")
	inlineCode = regexp.MustCompile("`([^`\n]+)`")
	flagWord   = regexp.MustCompile(`^(--[a-z][a-z0-9-]*|-[A-Za-z])(=.*)?$`)
	cmdWord    = regexp.MustCompile(`^[a-z][a-z-]*$`)
)

// skillDrift checks every ytrack invocation and flag named in the code of a
// markdown text (fenced blocks and inline code) against the command tree. It
// returns the number of invocations and the problems found. Code that starts
// with gh is a gh command and is skipped.
func skillDrift(tree *cobra.Command, text string) (int, []string) {
	invocations := 0
	var problems []string
	for _, s := range codeSnippets(text) {
		words := shellWords(s)
		if len(words) == 0 || words[0] == "gh" {
			continue
		}
		start := -1
		for i, w := range words {
			if w == "ytrack" {
				start = i
				break
			}
		}
		if start < 0 {
			// A flag named on its own must exist on some command.
			for _, w := range words {
				if m := flagWord.FindStringSubmatch(w); m != nil && !anyCommandHas(tree, m[1]) {
					problems = append(problems, m[1]+" is not a flag of any ytrack command")
				}
			}
			continue
		}
		invocations++
		problems = append(problems, checkInvocation(tree, words[start+1:])...)
		if p := apiMisuse(words[start+1:]); p != "" {
			problems = append(problems, p)
		}
	}
	return invocations, problems
}

// codeSnippets returns the lines of a markdown text's fenced blocks and its
// inline code spans.
func codeSnippets(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var snippets []string
	for _, m := range fence.FindAllStringSubmatch(text, -1) {
		snippets = append(snippets, strings.Split(m[1], "\n")...)
	}
	for _, m := range inlineCode.FindAllStringSubmatch(fence.ReplaceAllString(text, ""), -1) {
		snippets = append(snippets, m[1])
	}
	return snippets
}

// apiInvocations returns the arguments after "ytrack" of every ytrack api
// call in snippets.
func apiInvocations(snippets []string) [][]string {
	var calls [][]string
	for _, s := range snippets {
		words := shellWords(s)
		for i, w := range words {
			if w == "ytrack" {
				if i+1 < len(words) && words[i+1] == "api" {
					calls = append(calls, words[i+1:])
				}
				break
			}
		}
	}
	return calls
}

// Query parameters that only make sense on a read.
var readParams = map[string]bool{"query": true, "fields": true, "$top": true, "$skip": true, "customFields": true}

// apiMisuse reports a ytrack api call (args start at "api") that means to
// read but would write: -f, -F and --input switch the method to POST unless
// -X names one, --paginate refuses anything but GET, and a POST to /issues
// creates an issue.
func apiMisuse(args []string) string {
	if len(args) == 0 || args[0] != "api" {
		return ""
	}
	var path, method string
	var keys []string
	params, paginate := false, false
	for i := 1; i < len(args); i++ {
		w := args[i]
		if !strings.HasPrefix(w, "-") {
			if path == "" {
				path = w
			}
			continue
		}
		name, val, inline := strings.Cut(w, "=")
		value := func() string {
			if inline {
				return val
			}
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch name {
		case "-f", "--raw-field", "-F", "--field":
			params = true
			key, _, _ := strings.Cut(value(), "=")
			keys = append(keys, key)
		case "--input":
			params = true
			value()
		case "-X", "--method":
			method = strings.ToUpper(value())
		case "--paginate":
			paginate = true
		case "--fields", "--jq", "-q", "--template", "-t", "-H", "--header":
			value()
		}
	}
	call := "ytrack api " + path
	if method == "GET" || (!params && method == "") {
		return ""
	}
	if paginate {
		return call + ": --paginate without -X GET is a POST, which --paginate refuses"
	}
	if method != "" {
		return "" // an explicit write
	}
	for _, k := range keys {
		if readParams[k] {
			return fmt.Sprintf("%s: read parameter %q without -X GET is sent as a POST", call, k)
		}
	}
	if p := "/" + strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(path, "/"), "api/"), "/"); p == "/issues" && !slices.Contains(keys, "summary") {
		return call + ": parameters without -X GET POST to /issues, which creates an issue"
	}
	return ""
}

// checkInvocation resolves the command path in args, then checks each flag
// against that command, skipping the flag's value.
func checkInvocation(tree *cobra.Command, args []string) []string {
	var problems []string
	cmd, resolving := tree, true
	for i := 0; i < len(args); i++ {
		w := args[i]
		if m := flagWord.FindStringSubmatch(w); m != nil {
			fl := lookupFlag(cmd, m[1])
			if fl == nil {
				problems = append(problems, fmt.Sprintf("unknown flag %s for %q", m[1], cmd.CommandPath()))
				continue
			}
			if m[2] == "" && fl.NoOptDefVal == "" {
				i++ // the flag's value
			}
			continue
		}
		if !resolving {
			continue
		}
		if sub := subcommand(cmd, w); sub != nil {
			cmd = sub
			continue
		}
		resolving = false
		if cmd.HasSubCommands() && cmdWord.MatchString(w) {
			problems = append(problems, fmt.Sprintf("unknown command %q in %q", w, cmd.CommandPath()))
		}
	}
	return problems
}

func subcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, c := range cmd.Commands() {
		if c.Name() == name || c.HasAlias(name) {
			return c
		}
	}
	return nil
}

// lookupFlag finds --name or -x on cmd, including flags it inherits.
func lookupFlag(cmd *cobra.Command, flag string) *pflag.Flag {
	cmd.InitDefaultHelpFlag()
	for _, set := range []*pflag.FlagSet{cmd.Flags(), cmd.InheritedFlags()} {
		if name, ok := strings.CutPrefix(flag, "--"); ok {
			if fl := set.Lookup(name); fl != nil {
				return fl
			}
		} else if fl := set.ShorthandLookup(flag[1:]); fl != nil {
			return fl
		}
	}
	return nil
}

func anyCommandHas(cmd *cobra.Command, flag string) bool {
	if lookupFlag(cmd, flag) != nil {
		return true
	}
	for _, c := range cmd.Commands() {
		if anyCommandHas(c, flag) {
			return true
		}
	}
	return false
}

// shellWords splits a command line into words as a POSIX shell would for
// quoting, and stops at a comment, pipe, list operator or redirection.
func shellWords(line string) []string {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote rune
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			flush()
		case r == '|' || r == ';' || r == '&' || (!inWord && (r == '#' || r == '>')):
			flush()
			return truncateAtRedirect(words)
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return truncateAtRedirect(words)
}

func truncateAtRedirect(words []string) []string {
	for i, w := range words {
		if w == "<" {
			return words[:i]
		}
	}
	return words
}

// The skill tells agents that an issue URL on another host than --host or
// YTRACK_HOST is refused, quoting the error; keep both true.
func TestSkillHostConflictClaim(t *testing.T) {
	b, err := fs.ReadFile(skill.Files(), "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"the issue URL is on X`) {
		t.Fatal("SKILL.md no longer quotes the host-conflict error")
	}
	for name, setup := range map[string]func(env *cmdtest.Env) []string{
		"--host":      func(*cmdtest.Env) []string { return []string{"--host", "b.example.com"} },
		"YTRACK_HOST": func(*cmdtest.Env) []string { t.Setenv("YTRACK_HOST", "b.example.com"); return nil },
	} {
		t.Run("given "+name+" on another host than the issue URL, it exits 1 with the quoted error", func(t *testing.T) {
			env := cmdtest.New(t)
			args := append([]string{"issue", "view", "https://a.example.com/issue/APP-1"}, setup(env)...)
			if code := env.Run(args...); code != 1 {
				t.Fatalf("exit %d, want 1: %s", code, env.Stderr)
			}
			if want := "the issue URL is on a.example.com but " + name + " is b.example.com"; !strings.Contains(env.Stderr.String(), want) {
				t.Errorf("stderr = %q, want %q", env.Stderr, want)
			}
		})
	}
}
