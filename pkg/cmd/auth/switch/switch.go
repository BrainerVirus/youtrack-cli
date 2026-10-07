// Package authswitch implements `ytrack auth switch`.
package authswitch

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdSwitch returns the switch command.
func NewCmdSwitch(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "switch",
		Short: "Change the default host",
		Long: `Make another logged-in host the default. With --host, switch to that host;
otherwise choose from a list (interactive terminals only).`,
		Args: cmdutil.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			keys := cfg.HostKeys()
			if len(keys) == 0 {
				return clierr.AuthErrorf("not logged in to any YouTrack host; run `ytrack auth login`")
			}
			var target string
			switch {
			case f.HostFlag != "":
				h, err := hosts.Parse(f.HostFlag)
				if err != nil {
					return clierr.FlagErrorWrap(err)
				}
				if !slices.Contains(keys, h.Key) {
					return clierr.FlagErrorf("not logged in to %s; known hosts: %v", h.Key, keys)
				}
				target = h.Key
			case len(keys) == 1:
				target = keys[0]
			case f.IOStreams.CanPrompt():
				current := max(slices.Index(keys, cfg.DefaultHost()), 0)
				i, err := f.Prompter.Select("Default host:", keys, current)
				if err != nil {
					return err
				}
				target = keys[i]
			default:
				return clierr.FlagErrorf("--host is required when not running interactively")
			}
			if err := cfg.SetDefaultHost(target); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			ok, _ := f.IOStreams.Symbols()
			fmt.Fprintf(f.IOStreams.ErrOut, "%s Default host is now %s\n", ok, target)
			return nil
		},
	}
}
