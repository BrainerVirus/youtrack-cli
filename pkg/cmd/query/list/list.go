// Package list implements `ytrack query list`.
package list

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields are the --json fields of a saved search.
var Fields = []string{"id", "name", "query", "owner"}

type savedQuery struct{ adapter.SavedQuery }

func (q savedQuery) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "id":
			out[f] = q.ID
		case "name":
			out[f] = q.Name
		case "query":
			out[f] = q.Query
		case "owner":
			if q.Owner == "" {
				out[f] = nil
			} else {
				out[f] = q.Owner
			}
		}
	}
	return out
}

// NewCmdList returns the query list command.
func NewCmdList(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the saved searches you can see",
		Long: `List the saved searches you can see: ID, name, owner and query. A query
runs as is with "ytrack issue list --query" or "ytrack issue count".

JSON fields:
  id     saved search ID
  name   saved search name
  query  the YouTrack search query it runs
  owner  login of its owner, or null`,
		Example: `  $ ytrack query list
  $ ytrack issue list -q "$(ytrack query list --json name,query --jq '.[] | select(.name == "My open") | .query')"`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := f.APIClient()
			if err != nil {
				return err
			}
			qs, err := adapter.ListSavedQueries(cmd.Context(), client)
			if err != nil {
				return err
			}
			out := make([]savedQuery, len(qs))
			for i, q := range qs {
				out[i] = savedQuery{q}
			}
			ios := f.IOStreams
			if exporter != nil {
				return exporter.Write(ios, out)
			}
			if len(out) == 0 {
				fmt.Fprintln(ios.ErrOut, "no saved searches")
				return nil
			}
			tbl := output.NewTable(ios, "id", "name", "owner", "query")
			cs := ios.ColorScheme()
			for _, q := range out {
				tbl.StyledRow(
					output.Cell{Text: q.ID, Style: cs.Dim},
					output.Cell{Text: q.Name, Style: cs.Bold},
					output.Cell{Text: q.Owner},
					output.Cell{Text: q.Query, Style: cs.Accent},
				)
			}
			return tbl.Render()
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, Fields)
	return cmd
}
