// Package query implements `ytrack query`.
package query

import (
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/query/list"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdQuery returns the query command group.
func NewCmdQuery(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query <command>",
		Short: "List saved searches",
		RunE:  cmdutil.GroupRunE,
	}
	cmd.AddCommand(list.NewCmdList(f))
	return cmd
}
