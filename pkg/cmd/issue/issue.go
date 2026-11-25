// Package issue implements `ytrack issue`.
package issue

import (
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/activities"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/attach"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/attachments"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/command"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/comment"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/count"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/create"
	deletecmd "github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/delete"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/download"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/edit"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/link"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/list"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/unlink"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/view"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/vote"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/watchers"
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
	cmd.AddCommand(count.NewCmdCount(f))
	cmd.AddCommand(activities.NewCmdActivities(f))
	cmd.AddCommand(comment.NewCmdComment(f))
	cmd.AddCommand(attach.NewCmdAttach(f))
	cmd.AddCommand(attachments.NewCmdAttachments(f))
	cmd.AddCommand(download.NewCmdDownload(f))
	cmd.AddCommand(create.NewCmdCreate(f))
	cmd.AddCommand(edit.NewCmdEdit(f))
	cmd.AddCommand(deletecmd.NewCmdDelete(f))
	cmd.AddCommand(command.NewCmdCommand(f))
	cmd.AddCommand(link.NewCmdLink(f))
	cmd.AddCommand(unlink.NewCmdUnlink(f))
	cmd.AddCommand(watchers.NewCmdWatchers(f))
	cmd.AddCommand(vote.NewCmdVote(f))
	cmd.AddCommand(vote.NewCmdUnvote(f))
	return cmd
}
