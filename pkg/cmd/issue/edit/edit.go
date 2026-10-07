// Package edit implements `ytrack issue edit`.
package edit

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
	ref         shared.Ref
	summary     string
	summarySet  bool
	description shared.DescriptionFlags
	fields      []string
	addTags     []string
	removeTags  []string
	assignee    string
	assigns     []shared.FieldAssign
	exporter    *output.Exporter
}

// NewCmdEdit returns the edit command.
func NewCmdEdit(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "edit {<id> | <url>}",
		Short: "Edit an issue",
		Long: `Change an issue's summary, description, custom fields or tags, and print
its ID and URL. Only what you pass is sent; everything else stays as is.

The description comes from --description, from --description-file (a file,
or "-" for standard input) or from your editor with --editor, which starts
with the current description.

` + shared.FieldsFlagHelp + `

--assignee is short for --field "Assignee=<login>"; --assignee "" unassigns.
--add-tag and --remove-tag take existing tag names and can be repeated.

An issue URL must be on a logged-in host, or named with --host.

JSON fields:
` + shared.IssueFieldsHelp,
		Example: `  $ ytrack issue edit APP-123 --summary 'Updated summary'
  $ ytrack issue edit APP-123 --field Priority=Critical --field 'Fix versions=2026.3,2026.4'
  $ ytrack issue edit APP-123 --editor
  $ ytrack issue edit APP-123 --add-tag backend --remove-tag triage --assignee me`,
		Args: cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			if err := opts.description.Check(cmd); err != nil {
				return err
			}
			opts.summarySet = cmd.Flags().Changed("summary")
			if opts.summarySet && strings.TrimSpace(opts.summary) == "" {
				return clierr.FlagErrorf("the summary cannot be empty")
			}
			raw := opts.fields
			if cmd.Flags().Changed("assignee") {
				raw = append(raw, adapter.AssigneeField+"="+opts.assignee)
			}
			if opts.assigns, err = shared.ParseFieldFlags(raw); err != nil {
				return err
			}
			if !opts.summarySet && !opts.description.Given() && len(opts.assigns) == 0 && len(opts.addTags) == 0 && len(opts.removeTags) == 0 {
				return clierr.FlagErrorf("nothing to change: pass --summary, a description flag, --field, --assignee, --add-tag or --remove-tag")
			}
			if err := shared.UseRefHostForWrite(f, ref); err != nil {
				return err
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.summary, "summary", "s", "", "New summary (title)")
	opts.description.Add(cmd)
	fl.StringArrayVarP(&opts.fields, "field", "f", nil, "Set a custom field: \"Name=Value\" (repeatable)")
	fl.StringArrayVar(&opts.addTags, "add-tag", nil, "Add a tag by `name` (repeatable)")
	fl.StringArrayVar(&opts.removeTags, "remove-tag", nil, "Remove a tag by `name` (repeatable)")
	fl.StringVarP(&opts.assignee, "assignee", "a", "", "Assignee `login`, \"me\", or \"\" to unassign")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, shared.IssueFields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	ios := f.IOStreams
	ctx := cmd.Context()
	id := opts.ref.ID
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}

	var changed []string
	ch := adapter.IssueChange{}
	var commands []string
	if len(opts.assigns) > 0 {
		p, err := adapter.GetIssueProject(ctx, client, id)
		if err != nil {
			return shared.NotFound(err, id, host)
		}
		zone, err := shared.Zone(f)
		if err != nil {
			return err
		}
		w := &shared.FieldWriter{Client: client, Project: adapter.ProjectInfo{ID: p.ProjectID, ShortName: p.ShortName}, Zone: zone}
		if ch.CustomFields, commands, err = w.Encode(ctx, opts.assigns); err != nil {
			return err
		}
		for _, a := range opts.assigns {
			changed = append(changed, a.Name)
		}
	}
	addTags, err := shared.ResolveTags(ctx, client, opts.addTags)
	if err != nil {
		return err
	}
	removeTags, err := shared.ResolveTags(ctx, client, opts.removeTags)
	if err != nil {
		return err
	}
	if opts.summarySet {
		s := strings.TrimSpace(opts.summary)
		ch.Summary = &s
		changed = append(changed, "summary")
	}
	if opts.description.Given() {
		initial := ""
		if opts.description.Editor {
			cur, err := adapter.GetIssue(ctx, client, id, []string{"description"})
			if err != nil {
				return shared.NotFound(err, id, host)
			}
			initial = cur.Description
		}
		text, err := opts.description.Read(f, initial)
		if err != nil {
			return err
		}
		ch.Description = &text
		changed = append(changed, "description")
	}

	var jsonFields []string
	if opts.exporter != nil {
		jsonFields = opts.exporter.Fields
	}
	attrs := shared.Attrs(jsonFields, "idReadable", "summary")
	var issue adapter.Issue
	fresh := false
	if !ch.Empty() {
		if issue, err = adapter.UpdateIssue(ctx, client, id, ch, attrs); err != nil {
			return shared.NotFound(err, id, host)
		}
		fresh = true
	}
	for _, t := range addTags {
		if err := adapter.AddIssueTag(ctx, client, id, t.ID); err != nil {
			return shared.NotFound(err, id, host)
		}
		changed, fresh = append(changed, "+"+t.Name), false
	}
	for _, t := range removeTags {
		if err := adapter.RemoveIssueTag(ctx, client, id, t.ID); err != nil {
			return fmt.Errorf("removing tag %q from %s: %w", t.Name, id, err)
		}
		changed, fresh = append(changed, "-"+t.Name), false
	}
	for _, c := range commands {
		if _, err := adapter.ApplyCommand(ctx, client, adapter.Command{Query: c, IssueIDs: []string{id}}); err != nil {
			return fmt.Errorf("`%s` failed on %s: %w", c, id, err)
		}
		fresh = false
	}
	if !fresh {
		if issue, err = adapter.GetIssue(ctx, client, id, attrs); err != nil {
			return shared.NotFound(err, id, host)
		}
	}
	if opts.exporter != nil {
		return opts.exporter.Write(ios, shared.Issue{Issue: issue, Host: host})
	}
	fmt.Fprintf(ios.Out, "%s\t%s\n", issue.IDReadable, shared.IssueURL(host, issue.IDReadable))
	if ios.IsStderrTTY() {
		fmt.Fprintf(ios.ErrOut, "Updated %s (%s): %s\n", issue.IDReadable, output.SanitizeCell(strings.Join(changed, ", ")), output.SanitizeCell(issue.Summary))
	}
	return nil
}
