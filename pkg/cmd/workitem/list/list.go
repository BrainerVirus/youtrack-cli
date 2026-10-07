// Package list implements `ytrack work-item list`.
package list

import (
	"fmt"
	"time"

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
	ref          issueshared.Ref
	author       string
	since, until string
	from, to     time.Time
	exporter     *output.Exporter
}

// NewCmdList returns the list command.
func NewCmdList(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:     "list {<issue-id> | <url>}",
		Aliases: []string{"ls"},
		Short:   "List the work items of an issue",
		Long: `List the time spent on an issue: date, duration, author, type and text.

--author keeps the items of one login ("me" for yourself). --since and
--until keep the items dated on or after, and on or before, a YYYY-MM-DD day.

JSON fields:
` + shared.FieldsHelp,
		Example: `  $ ytrack work-item list APP-123
  $ ytrack work-item list APP-123 --author me --since 2026-10-01
  $ ytrack work-item list APP-123 --json id,date,duration --jq 'map(.duration.minutes) | add'`,
		Args: cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := issueshared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			for _, d := range []struct {
				flag, value string
				out         *time.Time
			}{{"--since", opts.since, &opts.from}, {"--until", opts.until, &opts.to}} {
				if d.value == "" {
					continue
				}
				day, err := worktime.ParseDay(d.value)
				if err != nil {
					return clierr.FlagErrorf("invalid %s %q: use YYYY-MM-DD", d.flag, d.value)
				}
				*d.out = day
			}
			if !opts.from.IsZero() && !opts.to.IsZero() && opts.to.Before(opts.from) {
				return clierr.FlagErrorf("--until %s is before --since %s", opts.until, opts.since)
			}
			if err := issueshared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.author, "author", "a", "", "Only work items by this `login` (\"me\" for yourself)")
	fl.StringVar(&opts.since, "since", "", "Only work items on or after this `day` (YYYY-MM-DD)")
	fl.StringVar(&opts.until, "until", "", "Only work items on or before this `day` (YYYY-MM-DD)")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, shared.Fields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	author := opts.author
	if author == "me" {
		me, err := adapter.CurrentUser(ctx, client)
		if err != nil {
			return err
		}
		author = me.Login
	}
	items, err := adapter.ListWorkItems(ctx, client, opts.ref.ID)
	if err != nil {
		return issueshared.NotFound(err, opts.ref.ID, host)
	}

	var out []shared.WorkItem
	for _, it := range items {
		if author != "" && (it.Author == nil || it.Author.Login != author) {
			continue
		}
		day := it.Date.Truncate(24 * time.Hour) // the UTC day, as YouTrack stores it
		if !opts.from.IsZero() && day.Before(opts.from) {
			continue
		}
		if !opts.to.IsZero() && day.After(opts.to) {
			continue
		}
		out = append(out, shared.New(it, host, opts.ref.ID))
	}

	ios := f.IOStreams
	if opts.exporter != nil {
		if out == nil {
			out = []shared.WorkItem{}
		}
		return opts.exporter.Write(ios, out)
	}
	if len(out) == 0 {
		fmt.Fprintf(ios.ErrOut, "no work items on %s match\n", opts.ref.ID)
		return nil
	}
	tbl := output.NewTable(ios, "id", "date", "duration", "author", "type", "text")
	total := 0
	for _, w := range out {
		login := ""
		if w.Author != nil {
			login = w.Author.Login
		}
		tbl.Row(w.ID, w.Day(), w.Presentation(), login, w.Type, w.Text)
		total += w.Minutes
	}
	if err := tbl.Render(); err != nil {
		return err
	}
	if ios.IsStdoutTTY() {
		fmt.Fprintf(ios.ErrOut, "\n%d work items, %s in total\n", len(out), worktime.FormatMinutes(total))
	}
	return nil
}
