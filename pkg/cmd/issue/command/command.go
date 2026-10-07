// Package command implements `ytrack issue command`.
package command

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields lists the --json fields of issue command.
var Fields = []string{"issue", "url", "command", "dryRun", "commands"}

type options struct {
	ref      shared.Ref
	query    string
	comment  string
	silent   bool
	dryRun   bool
	exporter *output.Exporter
}

type result struct {
	Issue, URL, Command string
	DryRun              bool
	Commands            []adapter.ParsedCommand
}

func (r result) ExportData(fields []string) map[string]any {
	all := map[string]any{
		"issue": r.Issue, "url": r.URL, "command": r.Command, "dryRun": r.DryRun,
		"commands": shared.ParsedCommands(r.Commands),
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = all[f]
	}
	return out
}

// NewCmdCommand returns the command command.
func NewCmdCommand(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "command {<id> | <url>} <command>",
		Short: "Apply a YouTrack command to an issue",
		Long: `Apply a YouTrack command, such as "State In Progress for me" or
"Priority Critical tag regression", to an issue, as the command dialog in
YouTrack does. The command language is YouTrack's own; see
https://www.jetbrains.com/help/youtrack/cloud/commands.html.

--dry-run asks YouTrack how it parses the command for this issue and prints
each part without changing anything; it exits 1 when a part is not
understood. --comment adds a comment along with the change; the comment is visible to
everyone who can see the issue. --silent
applies the change without sending notifications.

An issue URL must be on a logged-in host, or named with --host.

JSON fields:
  issue     the issue as given
  url       issue web URL
  command   the command text
  dryRun    true when nothing was applied
  commands  [{description, error}]: the parts YouTrack parsed`,
		Example: `  $ ytrack issue command APP-123 'State In Progress for me'
  $ ytrack issue command APP-123 'Priority Critical' --dry-run
  $ ytrack issue command APP-123 'State Fixed' --comment 'Fixed in 2026.3' --silent`,
		Args: cmdutil.ExactArgs(2, "expected an issue ID or URL and a command, e.g. APP-123 'State Fixed'"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			opts.query = strings.TrimSpace(args[1])
			if opts.query == "" {
				return clierr.FlagErrorf("the command is empty")
			}
			if opts.dryRun && (opts.silent || cmd.Flags().Changed("comment")) {
				return clierr.FlagErrorf("`--dry-run` applies nothing, so `--comment` and `--silent` do not apply")
			}
			if err := shared.UseRefHostForWrite(f, ref); err != nil {
				return err
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.comment, "comment", "c", "", "Add a comment `text` with the change")
	fl.BoolVar(&opts.silent, "silent", false, "Apply without sending notifications")
	fl.BoolVarP(&opts.dryRun, "dry-run", "n", false, "Show how YouTrack parses the command without applying it")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, Fields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	ios := f.IOStreams
	ctx := cmd.Context()
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	id := opts.ref.ID
	c := adapter.Command{Query: opts.query, IssueIDs: []string{id}, Comment: opts.comment, Silent: opts.silent}
	res := result{Issue: id, URL: shared.IssueURL(host, id), Command: opts.query, DryRun: opts.dryRun}
	if opts.dryRun {
		if res.Commands, err = adapter.PreviewCommand(ctx, client, c); err != nil {
			return shared.NotFound(err, id, host)
		}
	} else if res.Commands, err = adapter.ApplyCommand(ctx, client, c); err != nil {
		return shared.NotFound(err, id, host)
	}

	switch {
	case opts.exporter != nil:
		if err := opts.exporter.Write(ios, res); err != nil {
			return err
		}
	case opts.dryRun:
		for _, p := range res.Commands {
			prefix := "  "
			if p.Error {
				prefix = "! "
			}
			fmt.Fprintf(ios.Out, "%s%s\n", prefix, output.SanitizeCell(p.Description))
		}
	default:
		fmt.Fprintf(ios.Out, "%s\t%s\n", id, res.URL)
		if ios.IsStderrTTY() {
			fmt.Fprintf(ios.ErrOut, "Applied %q to %s\n", output.SanitizeCell(opts.query), id)
		}
	}
	if opts.dryRun {
		if len(res.Commands) == 0 {
			return errors.New("YouTrack did not recognize the command")
		}
		for _, p := range res.Commands {
			if p.Error {
				return errors.New("YouTrack cannot apply part of the command (marked !); nothing was changed")
			}
		}
		if opts.exporter == nil && ios.IsStderrTTY() {
			fmt.Fprintf(ios.ErrOut, "Dry run: nothing was applied to %s\n", id)
		}
	}
	return nil
}
