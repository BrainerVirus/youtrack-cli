// Package list implements `ytrack tag list`.
package list

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/tag/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdList returns the tag list command.
func NewCmdList(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the tags you can use",
		Long: `List the tags you can see: ID, name and owner. Their names are what
"ytrack issue create --tag" and "ytrack issue edit --add-tag/--remove-tag"
take; "ytrack tag create" adds one.

JSON fields:
` + shared.FieldsHelp,
		Example: `  $ ytrack tag list
  $ ytrack tag list --json name --jq '.[].name'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := f.APIClient()
			if err != nil {
				return err
			}
			tags, err := adapter.ListTags(cmd.Context(), client)
			if err != nil {
				return err
			}
			out := make([]shared.Tag, len(tags))
			for i, t := range tags {
				out[i] = shared.Tag{Tag: t}
			}
			return render(f, exporter, out)
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, shared.Fields)
	return cmd
}

func render(f *cmdutil.Factory, exporter *output.Exporter, tags []shared.Tag) error {
	ios := f.IOStreams
	if exporter != nil {
		return exporter.Write(ios, tags)
	}
	if len(tags) == 0 {
		fmt.Fprintln(ios.ErrOut, "no tags; `ytrack tag create <name>` creates one")
		return nil
	}
	tbl := output.NewTable(ios, "id", "name", "owner")
	cs := ios.ColorScheme()
	for _, t := range tags {
		tbl.StyledRow(
			output.Cell{Text: t.ID, Style: cs.Dim},
			output.Cell{Text: t.Name, Style: cs.Bold},
			output.Cell{Text: t.Owner},
		)
	}
	return tbl.Render()
}
