// Package add implements `ytrack work-item add`.
package add

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

// MeetingText is the text of a --meeting work item without --text. It is
// workit's default meeting work item text.
const MeetingText = "Meetings"

type options struct {
	ref      issueshared.Ref
	duration string
	minutes  int
	date     string
	typeName string
	text     string
	textSet  bool
	meeting  bool
	exporter *output.Exporter
}

// NewCmdAdd returns the add command.
func NewCmdAdd(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "add {<issue-id> | <url>} --duration <duration>",
		Short: "Log time on an issue",
		Long: `Add a work item (spent time) to an issue and print its ID.

--duration takes hours and minutes: 1h30m, 1h 30m, 90m, 1.5h,
"2 hours 15 minutes", or a bare number of minutes (90). Days and weeks are
not accepted because their length depends on the YouTrack work schedule.

` + shared.DateHelp + `

--type takes a work item type name from the project's time tracking
settings (case is ignored); an unknown name lists the valid ones.

--meeting logs meeting time: the text defaults to "Meetings", as in workit.
Any --text or --type you give still applies.

JSON fields:
` + shared.FieldsHelp,
		Example: `  $ ytrack work-item add APP-123 --duration 1h30m --text 'Code review'
  $ ytrack work-item add APP-123 --duration 45m --date 2026-10-06 --type Development
  $ ytrack work-item add TEAM-1 --duration 30m --meeting
  $ ytrack work-item add APP-123 --duration 1.5h --json id,date,duration`,
		Args: cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := issueshared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			if !cmd.Flags().Changed("duration") {
				return clierr.FlagErrorf("`--duration` is required, e.g. --duration 1h30m")
			}
			if opts.minutes, err = worktime.ParseDuration(opts.duration); err != nil {
				return clierr.FlagErrorWrap(err)
			}
			opts.textSet = cmd.Flags().Changed("text")
			if err := issueshared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.duration, "duration", "d", "", "Time spent, e.g. 1h30m, 90m or 1.5h")
	fl.StringVar(&opts.date, "date", "auto", "Day of the work: auto (today) or YYYY-MM-DD")
	fl.StringVar(&opts.typeName, "type", "", "Work item type `name`, e.g. Development")
	fl.StringVar(&opts.text, "text", "", "Description of the work")
	fl.BoolVar(&opts.meeting, "meeting", false, "Log meeting time (text defaults to \""+MeetingText+"\")")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, shared.Fields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	date, err := shared.WorkDate(f, opts.date)
	if err != nil {
		return err
	}
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	ch := adapter.WorkItemChange{Minutes: opts.minutes, Date: &date}
	switch {
	case opts.textSet:
		ch.Text = &opts.text
	case opts.meeting:
		text := MeetingText
		ch.Text = &text
	}
	if opts.typeName != "" {
		if ch.TypeID, err = shared.ResolveType(ctx, client, host, opts.ref.ID, opts.typeName); err != nil {
			return err
		}
	}
	item, err := adapter.AddWorkItem(ctx, client, opts.ref.ID, ch)
	if err != nil {
		return issueshared.NotFound(err, opts.ref.ID, host)
	}
	res := shared.New(item, host, opts.ref.ID)
	ios := f.IOStreams
	if opts.exporter != nil {
		return opts.exporter.Write(ios, res)
	}
	fmt.Fprintln(ios.Out, res.ID)
	if ios.IsStderrTTY() {
		fmt.Fprintf(ios.ErrOut, "Logged %s on %s for %s: %s\n", res.Presentation(), res.Issue, res.Day(), issueshared.IssueURL(host, res.Issue))
	}
	return nil
}
