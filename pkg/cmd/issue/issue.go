// Package issue implements `ytrack issue`.
package issue

import (
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/command"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/comment"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/create"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/edit"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/list"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/view"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdIssue returns the issue command group.
func NewCmdIssue(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue <command>",
		Short: "Work with YouTrack issues",
		RunE:  cmdutil.GroupRunE,
	}
	cmd.AddCommand(list.NewCmdList(f))
	cmd.AddCommand(view.NewCmdView(f))
	cmd.AddCommand(comment.NewCmdComment(f))
	cmd.AddCommand(create.NewCmdCreate(f))
	cmd.AddCommand(edit.NewCmdEdit(f))
	cmd.AddCommand(command.NewCmdCommand(f))
	return cmd
}
