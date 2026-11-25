// Package create implements `ytrack tag create`.
package create

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/tag/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdCreate returns the tag create command.
func NewCmdCreate(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a tag",
		Long: `Create a tag that you own, so "ytrack issue create --tag" and
"ytrack issue edit --add-tag" can use it. It prints the new tag's name.

A tag you can already see with that name (ignoring case) is not created
again: the command exits 1 and names it.

JSON fields:
` + shared.FieldsHelp,
		Example: `  $ ytrack tag create regression
  $ ytrack tag create 'needs review' --json id,name`,
		Args: cmdutil.ExactArgs(1, "expected one tag name, e.g. regression"),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return clierr.FlagErrorf("the tag name is empty")
			}
			client, _, err := f.APIClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			all, err := adapter.ListTags(ctx, client)
			if err != nil {
				return err
			}
			for _, t := range all {
				if strings.EqualFold(t.Name, name) {
					return fmt.Errorf("tag %q already exists (ID %s); --tag and --add-tag take it as is", t.Name, t.ID)
				}
			}
			t, err := adapter.CreateTag(ctx, client, name)
			if err != nil {
				return fmt.Errorf("creating tag %q: %w", name, err)
			}
			if exporter != nil {
				return exporter.Write(f.IOStreams, shared.Tag{Tag: t})
			}
			fmt.Fprintln(f.IOStreams.Out, t.Name)
			return nil
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, shared.Fields)
	return cmd
}
