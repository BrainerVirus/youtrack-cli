// Package root assembles the ytrack command tree and runs it.
package root

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/api"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/auth"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue"
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
  YTRACK_BROWSER      command used to open URLs
  YTRACK_EDITOR       editor for writing text (then GIT_EDITOR, VISUAL, EDITOR)
  NO_COLOR            disable color output

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
  $ ytrack issue list -q 'project: APP for: me #Unresolved'
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

	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return clierr.FlagErrorWrap(err)
	})

	cmd.AddCommand(auth.NewCmdAuth(f))
	cmd.AddCommand(issue.NewCmdIssue(f))
	cmd.AddCommand(workitem.NewCmdWorkItem(f))
	cmd.AddCommand(api.NewCmdAPI(f))
	cmd.AddCommand(version.NewCmdVersion(f))
	return cmd
}

// Execute runs the command tree with args, prints any error to stderr and
// returns the exit code.
func Execute(f *cmdutil.Factory, args []string) clierr.ExitCode {
	cmd := NewCmdRoot(f)
	cmd.SetArgs(args)
	ran, err := cmd.ExecuteC()
	if err == nil {
		return clierr.ExitOK
	}
	// Cobra reports these as plain errors; they are usage errors.
	if msg := err.Error(); strings.HasPrefix(msg, "unknown command") || strings.HasPrefix(msg, "unknown shorthand flag") || strings.HasPrefix(msg, "unknown flag") {
		err = clierr.FlagErrorWrap(err)
	}
	printError(f, ran, err)
	return clierr.Code(err)
}

func printError(f *cmdutil.Factory, cmd *cobra.Command, err error) {
	w := f.IOStreams.ErrOut
	switch {
	case errors.Is(err, clierr.ErrSilent):
		return
	case errors.Is(err, clierr.ErrCancel):
		fmt.Fprintln(w, "cancelled")
		return
	}
	fmt.Fprintf(w, "ytrack: %s\n", err)
	if _, ok := errors.AsType[*clierr.FlagError](err); ok && cmd != nil {
		fmt.Fprintf(w, "Run '%s --help' for usage.\n", cmd.CommandPath())
	}
}
