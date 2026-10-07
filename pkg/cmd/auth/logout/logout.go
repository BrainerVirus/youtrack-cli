// Package logout implements `ytrack auth logout`.
package logout

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdLogout returns the logout command.
func NewCmdLogout(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove a host's stored token and metadata",
		Long: `Remove the stored token and metadata for the active host, or for the
host given with --host. The token itself is not revoked in YouTrack.`,
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
			store := f.CredentialStore(cfg)
			if cfg.Host(host.Key) == nil {
				token, _, err := store.Get(host.Key)
				if err != nil {
					return err
				}
				if token == "" {
					return clierr.FlagErrorf("not logged in to %s", host.Key)
				}
			}
			if err := store.Delete(host.Key); err != nil {
				return err
			}
			cfg.RemoveHost(host.Key)
			if err := cfg.Save(); err != nil {
				return err
			}
			ok, _ := f.IOStreams.Symbols()
			fmt.Fprintf(f.IOStreams.ErrOut, "%s Logged out of %s\n", ok, host.Key)
			if os.Getenv("YTRACK_TOKEN") != "" {
				fmt.Fprintln(f.IOStreams.ErrOut, "! YTRACK_TOKEN is still set and will keep being used.")
			}
			if d := cfg.DefaultHost(); d != "" && d != host.Key {
				fmt.Fprintf(f.IOStreams.ErrOut, "Default host is now %s\n", d)
			}
			return nil
		},
	}
}
