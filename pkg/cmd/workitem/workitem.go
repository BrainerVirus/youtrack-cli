// Package workitem implements `ytrack work-item`.
package workitem

import (
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem/add"
	deletecmd "github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem/delete"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem/edit"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem/list"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdWorkItem returns the work-item command group.
func NewCmdWorkItem(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "work-item <command>",
		Aliases: []string{"workitem"},
		Short:   "Track time spent on issues",
		RunE:    cmdutil.GroupRunE,
	}
	cmd.AddCommand(list.NewCmdList(f))
	cmd.AddCommand(add.NewCmdAdd(f))
	cmd.AddCommand(edit.NewCmdEdit(f))
	cmd.AddCommand(deletecmd.NewCmdDelete(f))
	return cmd
}
