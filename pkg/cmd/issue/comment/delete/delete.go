// Package delete implements `ytrack issue comment delete`.
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
	ref       shared.Ref
	commentID string
	yes       bool
}

// NewCmdDelete returns the comment delete command.
func NewCmdDelete(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "delete {<id> | <url>} <comment-id>",
		Short: "Delete a comment",
		Long: `Delete a comment from an issue, with the files attached to it. Find
comment IDs with "ytrack issue comment list".

In a terminal it shows the comment and asks before deleting it; otherwise
--yes is required.`,
		Example: `  $ ytrack issue comment delete APP-123 4-17
  $ ytrack issue comment delete APP-123 4-17 --yes`,
		Args:              cmdutil.ExactArgs(2, "expected an issue ID or URL and a comment ID, e.g. APP-123 4-17"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			if opts.commentID, err = shared.ParseCommentID(args[1]); err != nil {
				return err
			}
			if !opts.yes && !f.IOStreams.CanPrompt() {
				return clierr.FlagErrorf("`--yes` is required to delete a comment when not running interactively")
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
	if !opts.yes {
		c, err := adapter.GetComment(ctx, client, opts.ref.ID, opts.commentID)
		if err != nil {
			return shared.CommentNotFound(err, opts.ref.ID, opts.commentID, host)
		}
		author := ""
		if c.Author != nil {
			author = " by " + c.Author.Login
		}
		ok, err := f.Prompter.Confirm(fmt.Sprintf("Delete comment %s on %s%s, %q?", c.ID, opts.ref.ID, author, shared.Excerpt(c.Text)))
		if err != nil {
			return err
		}
		if !ok {
			return clierr.ErrCancel
		}
	}
	if err := adapter.DeleteComment(ctx, client, opts.ref.ID, opts.commentID); err != nil {
		return shared.CommentNotFound(err, opts.ref.ID, opts.commentID, host)
	}
	if f.IOStreams.IsStderrTTY() {
		fmt.Fprintf(f.IOStreams.ErrOut, "%s Deleted comment %s from %s\n", f.IOStreams.ErrColorScheme().SuccessIcon(), opts.commentID, opts.ref.ID)
	}
	return nil
}
