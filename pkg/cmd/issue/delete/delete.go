// Package delete implements `ytrack issue delete`.
package delete

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

type options struct {
	ref shared.Ref
	yes bool
}

// NewCmdDelete returns the issue delete command.
func NewCmdDelete(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "delete {<id> | <url>}",
		Short: "Delete an issue",
		Long: `Delete an issue with its comments, attachments and work items. YouTrack
cannot undo it.

In a terminal it shows the issue and asks before deleting it; otherwise
--yes is required.`,
		Example: `  $ ytrack issue delete APP-123
  $ ytrack issue delete APP-123 --yes`,
		Args:              cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			if !opts.yes && !f.IOStreams.CanPrompt() {
				return clierr.FlagErrorf("`--yes` is required to delete an issue when not running interactively")
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			return f.WithIssueHostForWrite(cmd.Context(), ref.ID, func() error { return run(cmd, f, opts) })
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Delete without asking for confirmation")
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	id := opts.ref.ID
	if !opts.yes {
		issue, err := adapter.GetIssue(ctx, client, id, []string{"idReadable", "summary"})
		if err != nil {
			return shared.NotFound(err, id, host)
		}
		id = issue.IDReadable
		ok, err := f.Prompter.Confirm(fmt.Sprintf("Delete issue %s, %q, with its comments, attachments and work items? This cannot be undone.", id, shared.Excerpt(issue.Summary)))
		if err != nil {
			return err
		}
		if !ok {
			return clierr.ErrCancel
		}
	}
	if err := adapter.DeleteIssue(ctx, client, id); err != nil {
		return shared.NotFound(err, id, host)
	}
	if f.IOStreams.IsStderrTTY() {
		fmt.Fprintf(f.IOStreams.ErrOut, "%s Deleted issue %s\n", f.IOStreams.ErrColorScheme().SuccessIcon(), id)
	}
	return nil
}
