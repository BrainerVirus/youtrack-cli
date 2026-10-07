// Package version implements `ytrack version`.
package version

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/build"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdVersion returns the version command.
func NewCmdVersion(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show ytrack version information",
		Args:  cmdutil.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprint(f.IOStreams.Out, Format(build.Version, build.Commit, build.Date))
			return err
		},
	}
}

// Format renders version information, e.g. "ytrack version 1.2.0 (2026-10-07)\ncommit abc123\n".
func Format(version, commit, date string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ytrack version %s", version)
	if date != "" {
		fmt.Fprintf(&b, " (%s)", date)
	}
	b.WriteString("\n")
	if commit != "" {
		fmt.Fprintf(&b, "commit %s\n", commit)
	}
	return b.String()
}
