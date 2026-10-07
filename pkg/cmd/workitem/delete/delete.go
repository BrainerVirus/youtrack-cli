// Package delete implements `ytrack work-item delete`.
package delete

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	issueshared "github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

type options struct {
	ref    issueshared.Ref
	itemID string
	yes    bool
}

// NewCmdDelete returns the delete command.
func NewCmdDelete(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "delete {<issue-id> | <url>} <work-item-id>",
		Short: "Delete a work item",
		Long: `Delete a work item from an issue.

In a terminal ytrack shows the work item and asks before deleting it.
Otherwise --yes is required.`,
		Example: `  $ ytrack work-item delete APP-123 115-3
  $ ytrack work-item delete APP-123 115-3 --yes`,
		Args: cmdutil.ExactArgs(2, "expected an issue ID or URL and a work item ID, e.g. APP-123 115-3"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := issueshared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			if opts.itemID, err = shared.ParseItemID(args[1]); err != nil {
				return err
			}
			if !opts.yes && !f.IOStreams.CanPrompt() {
				return clierr.FlagErrorf("`--yes` is required to delete a work item when not running interactively")
			}
			if err := issueshared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			return run(cmd, f, opts)
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
	if !opts.yes {
		item, err := adapter.GetWorkItem(ctx, client, opts.ref.ID, opts.itemID)
		if err != nil {
			return shared.NotFound(err, opts.ref.ID, opts.itemID, host)
		}
		w := shared.New(item, host, opts.ref.ID)
		author := ""
		if w.Author != nil {
			author = " by " + w.Author.Login
		}
		ok, err := f.Prompter.Confirm(fmt.Sprintf("Delete work item %s on %s: %s on %s%s, %q?", w.ID, w.Issue, w.Presentation(), w.Day(), author, w.Text))
		if err != nil {
			return err
		}
		if !ok {
			return clierr.ErrCancel
		}
	}
	if err := adapter.DeleteWorkItem(ctx, client, opts.ref.ID, opts.itemID); err != nil {
		return shared.NotFound(err, opts.ref.ID, opts.itemID, host)
	}
	if f.IOStreams.IsStderrTTY() {
		fmt.Fprintf(f.IOStreams.ErrOut, "Deleted work item %s from %s\n", opts.itemID, opts.ref.ID)
	}
	return nil
}
