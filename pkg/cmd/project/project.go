// Package project implements `ytrack project`.
package project

import (
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/project/fields"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdProject returns the project command group.
func NewCmdProject(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project <command>",
		Short: "Look up YouTrack projects",
		RunE:  cmdutil.GroupRunE,
	}
	cmd.AddCommand(fields.NewCmdFields(f))
	return cmd
}
