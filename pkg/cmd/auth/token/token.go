// Package token implements `ytrack auth token`.
package token

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdToken returns the token command.
func NewCmdToken(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print the token for the active host",
		Long: `Print the token ytrack uses for the active host, or for --host, to
standard output. This is the only command that prints a token unmasked.`,
		Args: cmdutil.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			host, err := f.ResolveHost(cfg)
			if err != nil {
				return err
			}
			token, _, err := f.ResolveToken(cfg, host)
			if err != nil {
				return err
			}
			if token == "" {
				return clierr.AuthErrorf("no token for %s; run `ytrack auth login --host %s`", host.Key, host.Key)
			}
			_, err = fmt.Fprintln(f.IOStreams.Out, token)
			return err
		},
	}
}
