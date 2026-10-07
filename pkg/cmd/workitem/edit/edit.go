// Package edit implements `ytrack work-item edit`.
package edit

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/worktime"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	issueshared "github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

type options struct {
	ref      issueshared.Ref
	itemID   string
	duration string
	date     string
	typeName string
	text     string
	exporter *output.Exporter
}

// NewCmdEdit returns the edit command.
func NewCmdEdit(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "edit {<issue-id> | <url>} <work-item-id>",
		Short: "Change a work item",
		Long: `Change the duration, date, text or type of a work item. Attributes you do
not pass stay as they are; --text '' clears the text.

--duration and --date take the same values as in ` + "`work-item add`" + `.
` + shared.DateHelp + `

JSON fields:
` + shared.FieldsHelp,
		Example: `  $ ytrack work-item edit APP-123 115-3 --duration 2h
  $ ytrack work-item edit APP-123 115-3 --date 2026-10-06 --text 'Pairing on the fix'
  $ ytrack work-item edit APP-123 115-3 --type Testing`,
		Args: cmdutil.ExactArgs(2, "expected an issue ID or URL and a work item ID, e.g. APP-123 115-3"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := issueshared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			if opts.itemID, err = shared.ParseItemID(args[1]); err != nil {
				return err
			}
			fl := cmd.Flags()
			if !fl.Changed("duration") && !fl.Changed("date") && !fl.Changed("text") && !fl.Changed("type") {
				return clierr.FlagErrorf("nothing to change: pass `--duration`, `--date`, `--text` or `--type`")
			}
			ch := adapter.WorkItemChange{}
			if fl.Changed("duration") {
				if ch.Minutes, err = worktime.ParseDuration(opts.duration); err != nil {
					return clierr.FlagErrorWrap(err)
				}
			}
			if fl.Changed("text") {
				ch.Text = &opts.text
			}
			if fl.Changed("type") && opts.typeName == "" {
				return clierr.FlagErrorf("`--type` needs a work item type name")
			}
			if err := issueshared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			if fl.Changed("date") {
				d, err := shared.WorkDate(f, opts.date)
				if err != nil {
					return err
				}
				ch.Date = &d
			}
			return run(cmd, f, opts, ch)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.duration, "duration", "d", "", "New time spent, e.g. 1h30m, 90m or 1.5h")
	fl.StringVar(&opts.date, "date", "", "New day: auto (today) or YYYY-MM-DD")
	fl.StringVar(&opts.typeName, "type", "", "New work item type `name`")
	fl.StringVar(&opts.text, "text", "", "New description")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, shared.Fields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options, ch adapter.WorkItemChange) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if opts.typeName != "" {
		if ch.TypeID, err = shared.ResolveType(ctx, client, host, opts.ref.ID, opts.typeName); err != nil {
			return err
		}
	}
	item, err := adapter.UpdateWorkItem(ctx, client, opts.ref.ID, opts.itemID, ch)
	if err != nil {
		return shared.NotFound(err, opts.ref.ID, opts.itemID, host)
	}
	res := shared.New(item, host, opts.ref.ID)
	ios := f.IOStreams
	if opts.exporter != nil {
		return opts.exporter.Write(ios, res)
	}
	fmt.Fprintln(ios.Out, res.ID)
	if ios.IsStderrTTY() {
		fmt.Fprintf(ios.ErrOut, "Updated work item %s on %s: %s on %s\n", res.ID, res.Issue, res.Presentation(), res.Day())
	}
	return nil
}
