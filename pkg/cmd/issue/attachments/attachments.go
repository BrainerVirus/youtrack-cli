// Package attachments implements `ytrack issue attachments`.
package attachments

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdAttachments returns the attachments command.
func NewCmdAttachments(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:   "attachments {<id> | <url>}",
		Short: "List the attachments of an issue",
		Long: `List the files attached to an issue and to its comments: name, ID, size,
media type, when and by whom it was attached, and the comment it is on
(empty for a file on the issue itself).

To save them, use "ytrack issue download".

JSON fields:
` + shared.AttachmentFieldsHelp,
		Example: `  $ ytrack issue attachments APP-123
  $ ytrack issue attachments APP-123 --json name,size,commentId`,
		Args:              cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			var list []adapter.IssueAttachment
			err = f.WithIssueHost(cmd.Context(), ref.ID, func() error {
				client, host, err := f.APIClient()
				if err != nil {
					return err
				}
				list, err = adapter.ListAttachments(cmd.Context(), client, ref.ID)
				return shared.NotFound(err, ref.ID, host)
			})
			if err != nil {
				return err
			}
			ios := f.IOStreams
			if exporter != nil {
				out := make([]shared.Attachment, len(list))
				for i, a := range list {
					out[i] = shared.Attachment{IssueAttachment: a}
				}
				return exporter.Write(ios, out)
			}
			if len(list) == 0 {
				fmt.Fprintf(ios.ErrOut, "%s has no attachments\n", ref.ID)
				return nil
			}
			tbl := output.NewTable(ios, "name", "id", "size", "type", "created", "author", "comment")
			cs := ios.ColorScheme()
			for _, a := range list {
				author := ""
				if a.Author != nil {
					author = a.Author.Login
				}
				created := ""
				if !a.Created.IsZero() {
					created = a.Created.UTC().Format(time.RFC3339)
				}
				tbl.StyledRow(
					output.Cell{Text: a.Name, Style: cs.Bold},
					output.Cell{Text: a.ID, Style: cs.Dim},
					output.Cell{Text: strconv.FormatInt(a.Size, 10)},
					output.Cell{Text: a.MimeType, Style: cs.Dim},
					output.Cell{Text: created},
					output.Cell{Text: author},
					output.Cell{Text: a.CommentID, Style: cs.Dim},
				)
			}
			return tbl.Render()
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, shared.AttachmentFields)
	return cmd
}
