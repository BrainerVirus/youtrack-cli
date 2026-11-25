// Package edit implements `ytrack issue comment edit`.
package edit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields lists the --json fields of issue comment edit.
var Fields = []string{"id", "text", "author", "created", "updated", "url"}

type options struct {
	ref       shared.Ref
	commentID string
	body      shared.BodyFlags
	// text is the new text once read, so a rerun on the host that owns the
	// issue does not read it again.
	text     string
	textRead bool
	sent     bool
	exporter *output.Exporter
}

// NewCmdEdit returns the comment edit command.
func NewCmdEdit(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "edit {<id> | <url>} <comment-id>",
		Short: "Edit a comment",
		Long: `Replace the text of a comment and print its URL. Find comment IDs with
"ytrack issue comment list".

The new text comes from --body, from --body-file (a file, or "-" for
standard input) or from your editor with --editor, which opens on the
current text. With none of them, the editor opens when ytrack runs in a
terminal. An empty text is refused; an unchanged one sends nothing.

JSON fields:
  id       comment ID
  text     comment source text
  author   {login, fullName}
  created  creation time (RFC 3339, UTC)
  updated  last edit time (RFC 3339, UTC)
  url      comment web URL`,
		Example: `  $ ytrack issue comment edit APP-123 4-17 --body 'Reproduced on 2026.3'
  $ ytrack issue comment edit APP-123 4-17 --body-file notes.md
  $ ytrack issue comment edit APP-123 4-17 --editor`,
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
			if err := opts.body.Check(f, cmd); err != nil {
				return err
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			err = f.WithIssueHostForWrite(cmd.Context(), ref.ID, func() error { return run(cmd, f, opts) })
			if err != nil && opts.body.Editor && !opts.sent && strings.TrimSpace(opts.text) != "" {
				shared.SaveDraft(f, "the comment was not changed", opts.text)
			}
			return err
		},
	}
	opts.body.Add(cmd)
	cmdutil.AddJSONFlags(cmd, &opts.exporter, Fields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	// Read the comment first: the editor opens on its text, and a missing
	// comment fails before the user writes anything.
	c, err := adapter.GetComment(ctx, client, opts.ref.ID, opts.commentID)
	if err != nil {
		return shared.CommentNotFound(err, opts.ref.ID, opts.commentID, host)
	}
	if c.Deleted {
		return shared.CommentDeleted(opts.ref.ID, opts.commentID)
	}
	if !opts.textRead {
		if opts.text, err = opts.body.Read(f, c.Text); err != nil {
			return err
		}
		opts.textRead = true
	}
	if strings.TrimSpace(opts.text) == "" {
		return errors.New("the new text is empty; the comment was not changed")
	}
	if opts.text == c.Text {
		f.Warnf("the text is unchanged; nothing was sent")
	} else {
		if c, err = adapter.UpdateComment(ctx, client, opts.ref.ID, opts.commentID, opts.text); err != nil {
			return shared.CommentNotFound(err, opts.ref.ID, opts.commentID, host)
		}
		opts.sent = true
	}
	issueID := c.IssueIDReadable
	if issueID == "" {
		issueID = opts.ref.ID
	}
	out := shared.Comment{Comment: c, Host: host, IssueID: issueID}
	if opts.exporter != nil {
		return opts.exporter.Write(f.IOStreams, out)
	}
	fmt.Fprintln(f.IOStreams.Out, shared.CommentURL(host, issueID, c.ID))
	return nil
}
