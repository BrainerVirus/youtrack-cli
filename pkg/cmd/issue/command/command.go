// Package command implements `ytrack issue command`.
package command

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields lists the --json fields of issue command.
var Fields = []string{"issue", "url", "issues", "command", "dryRun", "commands"}

type options struct {
	refs     []shared.Ref
	query    string
	comment  string
	silent   bool
	dryRun   bool
	exporter *output.Exporter
}

type target struct{ Issue, URL string }

type result struct {
	Issues   []target
	Command  string
	DryRun   bool
	Commands []adapter.ParsedCommand
}

func (r result) ExportData(fields []string) map[string]any {
	issues := make([]map[string]any, len(r.Issues))
	for i, t := range r.Issues {
		issues[i] = map[string]any{"issue": t.Issue, "url": t.URL}
	}
	all := map[string]any{
		"issue": r.Issues[0].Issue, "url": r.Issues[0].URL, "issues": issues,
		"command": r.Command, "dryRun": r.DryRun,
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
		Use:   "command {<id> | <url>}... <command>",
		Short: "Apply a YouTrack command to one or more issues",
		Long: `Apply a YouTrack command, such as "State In Progress for me" or
"Priority Critical tag regression", to one or more issues, as the command
dialog in YouTrack does. The last argument is the command; every argument
before it is an issue, and all of them change in one request. The command
language is YouTrack's own; see
https://www.jetbrains.com/help/youtrack/cloud/commands.html.

--dry-run asks YouTrack how it parses the command for these issues and prints
each part without changing anything; it exits 1 when a part is not
understood. --comment adds a comment along with the change; the comment is visible to
everyone who can see the issue. --silent
applies the change without sending notifications.

An issue URL must be on a logged-in host, or named with --host; issue URLs
in one call must share a host.

JSON fields:
  issue     the first issue as given
  url       the first issue's web URL
  issues    [{issue, url}]: every issue, in the order given
  command   the command text
  dryRun    true when nothing was applied
  commands  [{description, error}]: the parts YouTrack parsed; description
            is as YouTrack sent it, which may be HTML`,
		Example: `  $ ytrack issue command APP-123 'State In Progress for me'
  $ ytrack issue command APP-123 'Priority Critical' --dry-run
  $ ytrack issue command APP-123 'State Fixed' --comment 'Fixed in 2026.3' --silent
  $ ytrack issue command APP-123 APP-124 APP-130 'Assignee me' --dry-run`,
		Args: cmdutil.MinimumArgs(2, "expected one or more issue IDs or URLs and a command, e.g. APP-123 'State Fixed'"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, query := args[:len(args)-1], args[len(args)-1]
			var hostRef shared.Ref
			for _, arg := range ids {
				ref, err := shared.ParseRef(arg)
				if err != nil {
					return err
				}
				if ref.ServiceURL != "" {
					if hostRef.ServiceURL != "" && !strings.EqualFold(strings.TrimRight(hostRef.ServiceURL, "/"), strings.TrimRight(ref.ServiceURL, "/")) {
						return clierr.FlagErrorf("issue URLs on different hosts (%s, %s); run one command per host", hostRef.ServiceURL, ref.ServiceURL)
					}
					hostRef = ref
				}
				opts.refs = append(opts.refs, ref)
			}
			opts.query = strings.TrimSpace(query)
			if opts.query == "" {
				return clierr.FlagErrorf("the command is empty")
			}
			if opts.dryRun && (opts.silent || cmd.Flags().Changed("comment")) {
				return clierr.FlagErrorf("`--dry-run` applies nothing, so `--comment` and `--silent` do not apply")
			}
			if err := shared.UseRefHostForWrite(f, hostRef); err != nil {
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
	ids := make([]string, len(opts.refs))
	res := result{Command: opts.query, DryRun: opts.dryRun}
	for i, ref := range opts.refs {
		ids[i] = ref.ID
		res.Issues = append(res.Issues, target{Issue: ref.ID, URL: shared.IssueURL(host, ref.ID)})
	}
	idList := strings.Join(ids, ", ")
	c := adapter.Command{Query: opts.query, IssueIDs: ids, Comment: opts.comment, Silent: opts.silent}
	if opts.dryRun {
		if res.Commands, err = adapter.PreviewCommand(ctx, client, c); err != nil {
			return shared.NotFound(err, idList, host)
		}
	} else if res.Commands, err = adapter.ApplyCommand(ctx, client, c); err != nil {
		return shared.NotFound(err, idList, host)
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
			fmt.Fprintf(ios.Out, "%s%s\n", prefix, output.SanitizeCell(plainText(p.Description)))
		}
	default:
		for _, t := range res.Issues {
			fmt.Fprintf(ios.Out, "%s\t%s\n", t.Issue, t.URL)
		}
		if ios.IsStderrTTY() {
			fmt.Fprintf(ios.ErrOut, "Applied %q to %s\n", output.SanitizeCell(opts.query), idList)
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
			fmt.Fprintf(ios.ErrOut, "Dry run: nothing was applied to %s\n", idList)
		}
	}
	return nil
}

// htmlTag matches a real tag only: "<" followed by a letter (start tag) or
// "/" and a letter (end tag), so literal text such as "a < b > c" survives.
var htmlTag = regexp.MustCompile(`</?[A-Za-z][^<>]*>`)

// plainText renders YouTrack's HTML command description, such as
// `Set Assignee <span class="bold">admin</span>`, as plain text: tags dropped,
// then entities decoded (so an escaped "&lt;b&gt;" stays as text). The caller
// sanitizes the result, which removes control characters that entities such
// as "&#27;" decode to. --json keeps the description as YouTrack sent it.
func plainText(s string) string {
	return html.UnescapeString(htmlTag.ReplaceAllString(s, ""))
}
