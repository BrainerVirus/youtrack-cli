// Package create implements `ytrack issue create`.
package create

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

type options struct {
	project     string
	summary     string
	description shared.DescriptionFlags
	fields      []string
	tags        []string
	assignee    string
	assigns     []shared.FieldAssign
	exporter    *output.Exporter
}

// NewCmdCreate returns the create command.
func NewCmdCreate(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an issue",
		Long: `Create an issue and print its ID and URL.

--project takes a project's short name (APP) or name. In a terminal,
ytrack asks for a missing project or summary and offers to open your editor
for the description; otherwise --project and --summary are required.

The description comes from --description, from --description-file (a file,
or "-" for standard input) or from your editor with --editor. The editor is
$YTRACK_EDITOR, $GIT_EDITOR, $VISUAL or $EDITOR.

` + shared.FieldsFlagHelp + `

--assignee is short for --field "Assignee=<login>". --tag adds an existing
tag and can be repeated.

JSON fields:
` + shared.IssueFieldsHelp,
		Example: `  $ ytrack issue create
  $ ytrack issue create --project APP --summary 'OAuth callback fails' --description-file notes.md
  $ ytrack issue create -p APP -s 'Crash on start' --field Priority=Critical --field Type=Bug --assignee me
  $ ytrack issue create -p APP -s 'Spike' --field 'Estimation=2d' --tag backend --json idReadable,url`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.description.Check(cmd); err != nil {
				return err
			}
			raw := opts.fields
			if cmd.Flags().Changed("assignee") {
				raw = append(raw, adapter.AssigneeField+"="+opts.assignee)
			}
			var err error
			if opts.assigns, err = shared.ParseFieldFlags(raw); err != nil {
				return err
			}
			if !f.IOStreams.CanPrompt() && (opts.project == "" || strings.TrimSpace(opts.summary) == "") {
				return clierr.FlagErrorf("`--project` and `--summary` are required when not running interactively")
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.project, "project", "p", "", "Project short name or name")
	fl.StringVarP(&opts.summary, "summary", "s", "", "Issue summary (title)")
	opts.description.Add(cmd)
	fl.StringArrayVarP(&opts.fields, "field", "f", nil, "Set a custom field: \"Name=Value\" (repeatable)")
	fl.StringArrayVar(&opts.tags, "tag", nil, "Add a tag by `name` (repeatable)")
	fl.StringVarP(&opts.assignee, "assignee", "a", "", "Assignee `login`, or \"me\"")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, shared.IssueFields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	ios := f.IOStreams
	ctx := cmd.Context()
	// Fail on credentials before asking anything.
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	if err := prompt(f, opts); err != nil {
		return err
	}
	project, err := shared.FindProject(ctx, client, opts.project)
	if err != nil {
		return err
	}
	zone, err := shared.Zone(f)
	if err != nil {
		return err
	}
	w := &shared.FieldWriter{Client: client, Project: project, Zone: zone}
	fields, commands, err := w.Encode(ctx, opts.assigns)
	if err != nil {
		return err
	}
	tags, err := shared.ResolveTags(ctx, client, opts.tags)
	if err != nil {
		return err
	}
	summary := strings.TrimSpace(opts.summary)
	ch := adapter.IssueChange{ProjectID: project.ID, Summary: &summary, CustomFields: fields}
	for _, t := range tags {
		ch.TagIDs = append(ch.TagIDs, t.ID)
	}
	if opts.description.Given() {
		text, err := opts.description.Read(f, "")
		if err != nil {
			return err
		}
		ch.Description = &text
	}

	var jsonFields []string
	if opts.exporter != nil {
		jsonFields = opts.exporter.Fields
	}
	attrs := shared.Attrs(jsonFields, "idReadable", "summary")
	issue, err := adapter.CreateIssue(ctx, client, ch, attrs)
	if err != nil {
		return err
	}
	for _, c := range commands {
		if _, err := adapter.ApplyCommand(ctx, client, adapter.Command{Query: c, IssueIDs: []string{issue.IDReadable}}); err != nil {
			fmt.Fprintf(ios.Out, "%s\t%s\n", issue.IDReadable, shared.IssueURL(host, issue.IDReadable))
			return fmt.Errorf("created %s, but `%s` failed: %w", issue.IDReadable, c, err)
		}
	}
	if len(commands) > 0 && opts.exporter != nil {
		if issue, err = adapter.GetIssue(ctx, client, issue.IDReadable, attrs); err != nil {
			return err
		}
	}
	if opts.exporter != nil {
		return opts.exporter.Write(ios, shared.Issue{Issue: issue, Host: host})
	}
	fmt.Fprintf(ios.Out, "%s\t%s\n", issue.IDReadable, shared.IssueURL(host, issue.IDReadable))
	if ios.IsStderrTTY() {
		fmt.Fprintf(ios.ErrOut, "Created %s: %s\n", issue.IDReadable, output.SanitizeCell(issue.Summary))
	}
	return nil
}

// prompt asks for what is missing when running in a terminal.
func prompt(f *cmdutil.Factory, opts *options) error {
	if !f.IOStreams.CanPrompt() {
		return nil
	}
	p := f.Prompter
	var err error
	for opts.project == "" {
		if opts.project, err = p.Input("Project:", ""); err != nil {
			return err
		}
	}
	for strings.TrimSpace(opts.summary) == "" {
		if opts.summary, err = p.Input("Summary:", ""); err != nil {
			return err
		}
	}
	if !opts.description.Given() {
		open, err := p.Confirm("Write a description in your editor?")
		if err != nil {
			return err
		}
		opts.description.Editor = open
	}
	return nil
}
