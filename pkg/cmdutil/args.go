package cmdutil

import (
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
)

// ExactArgs is cobra.ExactArgs returning a usage error with msg.
func ExactArgs(n int, msg string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return clierr.FlagErrorf("%s", msg)
		}
		return nil
	}
}

// NoArgs rejects positional arguments with a usage error.
func NoArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return clierr.FlagErrorf("unexpected argument %q for %q", args[0], cmd.CommandPath())
	}
	return nil
}

// MutuallyExclusive returns a usage error when more than one condition holds.
func MutuallyExclusive(message string, conditions ...bool) error {
	n := 0
	for _, c := range conditions {
		if c {
			n++
		}
	}
	if n > 1 {
		return clierr.FlagErrorf("%s", message)
	}
	return nil
}

// GroupRunE makes a command group print its help when called bare and fail
// with a usage error on an unknown subcommand, instead of exiting 0.
func GroupRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	return clierr.FlagErrorf("unknown command %q for %q", args[0], cmd.CommandPath())
}
