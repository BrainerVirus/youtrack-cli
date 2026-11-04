package root_test

import (
	"fmt"
	"io/fs"
	"regexp"
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
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var snippets []string
	for _, m := range fence.FindAllStringSubmatch(text, -1) {
		snippets = append(snippets, strings.Split(m[1], "\n")...)
	}
	for _, m := range inlineCode.FindAllStringSubmatch(fence.ReplaceAllString(text, ""), -1) {
		snippets = append(snippets, m[1])
	}
	invocations := 0
	var problems []string
	for _, s := range snippets {
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
	}
	return invocations, problems
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
