// Package auth implements `ytrack auth`.
package auth

import (
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/auth/login"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/auth/logout"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/auth/status"
	authswitch "github.com/BrainerVirus/youtrack-cli/pkg/cmd/auth/switch"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/auth/token"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdAuth returns the auth command group.
func NewCmdAuth(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <command>",
		Short: "Authenticate ytrack with YouTrack hosts",
		RunE:  cmdutil.GroupRunE,
	}
	cmd.AddCommand(login.NewCmdLogin(f))
	cmd.AddCommand(logout.NewCmdLogout(f))
	cmd.AddCommand(status.NewCmdStatus(f))
	cmd.AddCommand(token.NewCmdToken(f))
	cmd.AddCommand(authswitch.NewCmdSwitch(f))
	return cmd
}
