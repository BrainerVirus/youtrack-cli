// Package list implements `ytrack issue comment list`.
package list

import (
	"fmt"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

type options struct {
	ref      shared.Ref
	limit    int
	exporter *output.Exporter
}

// NewCmdList returns the comment list command.
func NewCmdList(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "list {<id> | <url>}",
		Short: "List the comments of an issue",
		Long: `List the latest comments of an issue, oldest first: ID, author, when it
was written and its text on one line. Use the ID with "ytrack issue comment
edit" and "ytrack issue comment delete"; --json text gives the full text.

--limit sets how many of the latest comments to list (default 30); --limit 0
lists all of them. Deleted comments are not listed.

JSON fields:
` + shared.CommentFieldsHelp,
		Example: `  $ ytrack issue comment list APP-123
  $ ytrack issue comment list APP-123 --limit 0 --json id,author,text`,
		Args:              cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			if opts.limit < 0 {
				return clierr.FlagErrorf("invalid --limit %d: use 0 or a positive number", opts.limit)
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			var out []shared.Comment
			err = f.WithIssueHost(cmd.Context(), ref.ID, func() error {
				out, err = fetch(cmd, f, opts)
				return err
			})
			if err != nil {
				return err
			}
			return render(f, opts, out)
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "L", 30, "Number of latest comments to list (0 for all)")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, shared.CommentFields)
	return cmd
}

func fetch(cmd *cobra.Command, f *cmdutil.Factory, opts *options) ([]shared.Comment, error) {
	client, host, err := f.APIClient()
	if err != nil {
		return nil, err
	}
	ctx := cmd.Context()
	// The issue gives the readable ID for comment URLs and the count that
	// places the latest --limit comments.
	issue, err := adapter.GetIssue(ctx, client, opts.ref.ID, []string{"idReadable", "commentsCount"})
	if err != nil {
		return nil, shared.NotFound(err, opts.ref.ID, host)
	}
	skip := 0
	if opts.limit > 0 {
		skip = max(0, issue.CommentsCount-opts.limit)
	}
	comments, err := adapter.ListComments(ctx, client, opts.ref.ID, skip, opts.limit)
	if err != nil {
		return nil, shared.NotFound(err, opts.ref.ID, host)
	}
	files := map[string][]adapter.Attachment{}
	if opts.exporter != nil && slices.Contains(opts.exporter.Fields, "attachments") {
		all, err := adapter.ListAttachments(ctx, client, opts.ref.ID)
		if err != nil {
			return nil, shared.NotFound(err, opts.ref.ID, host)
		}
		for _, a := range all {
			if a.CommentID != "" {
				files[a.CommentID] = append(files[a.CommentID], a.Attachment)
			}
		}
	}
	out := make([]shared.Comment, len(comments))
	for i, c := range comments {
		out[i] = shared.Comment{Comment: c, Host: host, IssueID: issue.IDReadable, Files: files[c.ID]}
	}
	return out, nil
}

func render(f *cmdutil.Factory, opts *options, comments []shared.Comment) error {
	ios := f.IOStreams
	if opts.exporter != nil {
		return opts.exporter.Write(ios, comments)
	}
	if len(comments) == 0 {
		fmt.Fprintf(ios.ErrOut, "%s has no comments\n", opts.ref.ID)
		return nil
	}
	tbl := output.NewTable(ios, "id", "author", "created", "text")
	cs := ios.ColorScheme()
	for _, c := range comments {
		author := ""
		if c.Author != nil {
			author = c.Author.Login
		}
		created := ""
		if !c.Created.IsZero() {
			created = c.Created.UTC().Format(time.RFC3339)
		}
		tbl.StyledRow(
			output.Cell{Text: c.ID, Style: cs.Dim},
			output.Cell{Text: author, Style: cs.Bold},
			output.Cell{Text: created},
			output.Cell{Text: c.Text},
		)
	}
	return tbl.Render()
}
