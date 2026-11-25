// Package tag implements `ytrack tag`.
package tag

import (
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/tag/create"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/tag/list"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdTag returns the tag command group.
func NewCmdTag(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag <command>",
		Short: "List and create issue tags",
		RunE:  cmdutil.GroupRunE,
	}
	cmd.AddCommand(list.NewCmdList(f))
	cmd.AddCommand(create.NewCmdCreate(f))
	return cmd
}
