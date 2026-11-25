// Package root assembles the ytrack command tree and runs it.
package root

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/api"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/auth"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/project"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/skill"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/tag"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/upgrade"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/version"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

const longHelp = `An unofficial command-line interface for JetBrains YouTrack.

Environment:
  YTRACK_HOST         YouTrack service URL or host to use instead of the default host
  YTRACK_TOKEN        token to use instead of stored credentials (applies to the active host)
  YTRACK_DEBUG        set to 1 to log HTTP requests to stderr (credentials redacted)
  YTRACK_CONFIG_DIR   configuration directory (default ~/.config/ytrack)
  YTRACK_CACHE_DIR    cache directory (default ~/.cache/ytrack)
  YTRACK_BROWSER      command used to open URLs (then the browser config key, BROWSER)
  YTRACK_EDITOR       editor for writing text (then GIT_EDITOR, VISUAL, EDITOR)
  YTRACK_NO_UPDATE_NOTIFIER  set to 1 to turn off the daily release check, its
                      notice and the background upgrade
  YTRACK_NO_AUTO_UPGRADE     set to 1 to turn off only the background upgrade
  NO_COLOR            disable color output (as do --no-color, CLICOLOR=0 and TERM=dumb)
  CLICOLOR_FORCE      set to 1 to color output that is not a terminal (not under NO_COLOR)

Exit codes:
  0 success, 1 failure (including usage errors), 2 cancelled, 4 authentication required`

// NewCmdRoot returns the root command.
func NewCmdRoot(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "ytrack <command> <subcommand> [flags]",
		Short:         "Work with YouTrack from the command line",
		Long:          longHelp,
		Version:       f.AppVersion,
		SilenceErrors: true,
		SilenceUsage:  true,
		Example: `  $ ytrack auth login
  $ ytrack issue list -q 'project: APP Assignee: me #Unresolved'
  $ ytrack issue view APP-123 --comments
  $ ytrack work-item add APP-123 --duration 1h30m --text 'Code review'
  $ ytrack api /users/me --fields login,fullName
  $ ytrack api /issues --paginate --fields idReadable,summary --jq '.[].idReadable'`,
	}
	cmd.SetVersionTemplate(`{{printf "ytrack version %s\n" .Version}}`)
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
	cmd.SetIn(f.IOStreams.In)

	pf := cmd.PersistentFlags()
	pf.StringVar(&f.HostFlag, "host", "", "YouTrack `host` or service URL to use")
	pf.BoolVar(&f.Debug, "debug", false, "Log HTTP requests to stderr with credentials redacted")
	pf.Bool("no-color", false, "Disable color output")
	cmd.SetHelpFunc(helpFunc(f.IOStreams))

	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return clierr.FlagErrorWrap(err)
	})

	cmd.AddCommand(auth.NewCmdAuth(f))
	cmd.AddCommand(issue.NewCmdIssue(f))
	cmd.AddCommand(workitem.NewCmdWorkItem(f))
	cmd.AddCommand(tag.NewCmdTag(f))
	cmd.AddCommand(project.NewCmdProject(f))
	cmd.AddCommand(api.NewCmdAPI(f))
	cmd.AddCommand(skill.NewCmdSkill(f))
	cmd.AddCommand(version.NewCmdVersion(f))
	cmd.AddCommand(upgrade.NewCmdUpgrade(f, nil))
	return cmd
}

// Execute runs the command tree with args, prints any error to stderr and
// returns the exit code.
func Execute(f *cmdutil.Factory, args []string) clierr.ExitCode {
	if colorOff(args) {
		f.IOStreams.DisableColor()
	}
	cmd := NewCmdRoot(f)
	cmd.SetArgs(args)
	notice := &updateNotice{f: f}
	// Runs once the command and its flags are parsed, so help and usage
	// errors never check, and --json is known.
	cmd.PersistentPreRun = func(c *cobra.Command, _ []string) {
		refreshSkills(c)
		notice.start(c)
	}
	ran, err := cmd.ExecuteC()
	defer notice.print()
	if err == nil {
		return clierr.ExitOK
	}
	// Cobra reports these as plain errors; they are usage errors.
	if msg := err.Error(); strings.HasPrefix(msg, "unknown command") || strings.HasPrefix(msg, "unknown shorthand flag") || strings.HasPrefix(msg, "unknown flag") {
		err = clierr.FlagErrorWrap(err)
	}
	printError(f, ran, err, args)
	return clierr.Code(err)
}

// colorOff reports whether args turn color off: --no-color, or --json, whose
// output is for programs. Checked before parsing so help and errors honour it.
func colorOff(args []string) bool {
	for _, a := range args {
		switch {
		case a == "--":
			return false
		case a == "--no-color", a == "--no-color=true", a == "--json", strings.HasPrefix(a, "--json="):
			return true
		}
	}
	return false
}

func printError(f *cmdutil.Factory, cmd *cobra.Command, err error, args []string) {
	w := f.IOStreams.ErrOut
	cs := f.IOStreams.ErrColorScheme()
	switch {
	case errors.Is(err, clierr.ErrSilent):
		return
	case errors.Is(err, clierr.ErrCancel):
		fmt.Fprintln(w, "cancelled")
		return
	}
	fmt.Fprintln(w, cs.Error("ytrack: "+output.SanitizeText(err.Error())))
	if _, ok := errors.AsType[*clierr.FlagError](err); ok && cmd != nil {
		if hint := flagHint(cmd, err, args); hint != "" {
			fmt.Fprintln(w, output.SanitizeText(hint))
		}
		fmt.Fprintf(w, "Run '%s' for usage.\n", cs.Bold(cmd.CommandPath()+" --help"))
	}
}

// flagHint names the flag a usage error was probably after (weak models guess
// flags; the skill bench saw `--comments 12` and `--limit 12` for
// `--comments-limit`), or "" when nothing is close.
func flagHint(cmd *cobra.Command, err error, args []string) string {
	if name, ok := strings.CutPrefix(err.Error(), "unknown flag: --"); ok {
		if near := nearFlags(cmd, name); len(near) > 0 {
			return "did you mean --" + strings.Join(near, " or --") + "?"
		}
		return ""
	}
	// A number given to a flag that takes no value: `--comments 12` (12
	// became a stray argument) or `--comments=12` (not a bool).
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break // the rest are arguments
		}
		if prev := cmd.Flags().Lookup(strings.TrimPrefix(args[max(i-1, 0)], "--")); i > 0 && prev != nil && prev.Value.Type() != "bool" && !strings.Contains(args[i-1], "=") {
			continue // this word is the previous flag's value
		}
		name, ok := strings.CutPrefix(args[i], "--")
		if !ok {
			continue
		}
		var num string
		if n, v, eq := strings.Cut(name, "="); eq {
			name, num = n, v
		} else if i+1 < len(args) {
			num = args[i+1]
		}
		if !isNumber(num) {
			continue
		}
		fl := cmd.Flags().Lookup(name)
		if fl == nil || fl.Value.Type() != "bool" {
			continue
		}
		hint := "--" + name + " takes no value"
		cmd.Flags().VisitAll(func(o *pflag.Flag) {
			if strings.HasPrefix(o.Name, name+"-") && o.Value.Type() == "int" {
				hint += "; for a number use --" + o.Name + " " + num
			}
		})
		return hint
	}
	return ""
}

// nearFlags returns up to three of cmd's flags that start with name (two
// letters or more), contain it or are contained in it (three or more), or
// are a few edits from it (one edit per three letters, at most two), so a
// one-letter guess matches nothing.
func nearFlags(cmd *cobra.Command, name string) []string {
	var near []string
	cmd.Flags().VisitAll(func(fl *pflag.Flag) {
		if fl.Hidden || len(near) == 3 {
			return
		}
		n := fl.Name
		edits := min(len(name), len(n)) / 3
		if (len(name) >= 2 && strings.HasPrefix(n, name)) ||
			(len(name) >= 3 && strings.Contains(n, name)) ||
			(len(n) >= 3 && strings.Contains(name, n)) ||
			(edits > 0 && editDistance(n, name) <= min(edits, 2)) {
			near = append(near, n)
		}
	})
	return near
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// editDistance is the optimal string alignment distance between a and b:
// Levenshtein, plus a swap of two adjacent letters as one edit (--hots).
func editDistance(a, b string) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}
