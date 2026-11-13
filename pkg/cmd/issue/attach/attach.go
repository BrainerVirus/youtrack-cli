// Package attach implements `ytrack issue attach`.
package attach

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields lists the --json fields of issue attach.
var Fields = []string{"name", "id", "size", "mimeType"}

type attachment struct{ adapter.Attachment }

func (a attachment) ExportData(fields []string) map[string]any {
	all := map[string]any{"name": a.Name, "id": a.ID, "size": a.Size, "mimeType": a.MimeType}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = all[f]
	}
	return out
}

// NewCmdAttach returns the attach command.
func NewCmdAttach(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:   "attach {<id> | <url>} <file>...",
		Short: "Attach files to an issue",
		Long: `Attach files to an issue and print each one as "<name><TAB><id>".

The files go up in one request, each named after its file name. A missing
or unreadable file stops the command before anything is uploaded. To put a
file in a comment instead, use "ytrack issue comment --attach".

JSON fields:
  name      attachment name
  id        attachment ID
  size      size in bytes
  mimeType  media type`,
		Example: `  $ ytrack issue attach APP-123 dump.sql repro.sh
  $ ytrack issue attach APP-123 trace.log --json name,id,size`,
		Args: cmdutil.MinimumArgs(2, "expected an issue ID or URL and at least one file, e.g. APP-123 log.txt"),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return shared.CompleteIssues(f, 1)(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			files, err := shared.ReadFiles(args[1:])
			if err != nil {
				return err
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			client, host, err := f.APIClient()
			if err != nil {
				return err
			}
			created, err := adapter.AttachToIssue(cmd.Context(), client, ref.ID, files)
			if err != nil {
				return shared.NotFoundElsewhere(cmd.Context(), f, err, ref.ID, host)
			}
			out := make([]attachment, len(created))
			for i, a := range created {
				out[i] = attachment{a}
			}
			if exporter != nil {
				return exporter.Write(f.IOStreams, out)
			}
			for _, a := range out {
				fmt.Fprintf(f.IOStreams.Out, "%s\t%s\n", output.SanitizeCell(a.Name), a.ID)
			}
			return nil
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, Fields)
	return cmd
}
