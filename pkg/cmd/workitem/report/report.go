// Package report implements `ytrack work-item report`.
package report

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/worktime"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	issueshared "github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields are the --json fields of work-item report.
var Fields = []string{"author", "since", "until", "project", "minutes", "duration", "issues", "days", "items"}

type options struct {
	author       string
	since, until string
	from, to     time.Time
	project      string
	exporter     *output.Exporter
}

// total is the time spent on one issue or one day.
type total struct {
	Key     string
	URL     string
	Minutes int
}

func (t total) export(key string) map[string]any {
	m := map[string]any{key: t.Key, "minutes": t.Minutes, "duration": worktime.FormatMinutes(t.Minutes)}
	if t.URL != "" {
		m["url"] = t.URL
	}
	return m
}

type report struct {
	Author, Since, Until, Project string
	Minutes                       int
	Issues, Days                  []total
	Items                         []shared.WorkItem
}

func (r report) ExportData(fields []string) map[string]any {
	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "author":
			out[f] = r.Author
		case "since":
			out[f] = nullable(r.Since)
		case "until":
			out[f] = nullable(r.Until)
		case "project":
			out[f] = nullable(r.Project)
		case "minutes":
			out[f] = r.Minutes
		case "duration":
			out[f] = worktime.FormatMinutes(r.Minutes)
		case "issues":
			list := make([]map[string]any, len(r.Issues))
			for i, t := range r.Issues {
				list[i] = t.export("issue")
			}
			out[f] = list
		case "days":
			list := make([]map[string]any, len(r.Days))
			for i, t := range r.Days {
				list[i] = t.export("date")
			}
			out[f] = list
		case "items":
			list := make([]map[string]any, len(r.Items))
			for i, w := range r.Items {
				list[i] = w.ExportData(shared.Fields)
			}
			out[f] = list
		}
	}
	return out
}

// NewCmdReport returns the report command.
func NewCmdReport(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Sum the time someone spent, per issue and per day",
		Long: `Sum the work items of one author across issues: the total, the time per
issue and the time per day.

--author is a login and defaults to you (me). --since and --until keep the
work items dated on or after, and on or before, a YYYY-MM-DD day; without
them every work item of the author counts. --project keeps the issues of
one project (short name or name).

Durations are summed in minutes and shown as hours and minutes (8h, not 1d),
whatever the instance's work schedule.

JSON fields:
  author    the author's login
  since     the --since day, or null
  until     the --until day, or null
  project   the --project value, or null
  minutes   total minutes
  duration  the total as hours and minutes, e.g. "9h 30m"
  issues    [{issue, url, minutes, duration}], most time first
  days      [{date, minutes, duration}], oldest first
  items     the work items, with the fields of "ytrack work-item list"`,
		Example: `  $ ytrack work-item report --since 2026-10-01 --until 2026-10-31
  $ ytrack work-item report --author jroe --project APP --since 2026-10-01
  $ ytrack work-item report --since 2026-10-01 --json days --jq '.days[] | "\(.date) \(.duration)"'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			opts.author = strings.TrimSpace(opts.author)
			if opts.author == "" {
				return clierr.FlagErrorf("--author needs a login, or me")
			}
			opts.project = strings.TrimSpace(opts.project)
			if strings.ContainsAny(opts.project, "{}") {
				return clierr.FlagErrorf("--project %q contains a brace, which a query term cannot hold", opts.project)
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.author, "author", "a", "me", "Whose work items to sum: a `login`, or me")
	fl.StringVar(&opts.since, "since", "", "Only work items on or after this `day` (YYYY-MM-DD)")
	fl.StringVar(&opts.until, "until", "", "Only work items on or before this `day` (YYYY-MM-DD)")
	fl.StringVarP(&opts.project, "project", "p", "", "Only issues in this `project` (short name or name)")
	_ = cmd.RegisterFlagCompletionFunc("project", issueshared.CompleteProjects(f))
	cmdutil.AddJSONFlags(cmd, &opts.exporter, Fields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	author := opts.author
	if strings.EqualFold(author, "me") {
		if author, err = f.CurrentLogin(ctx, client, host); err != nil {
			return err
		}
	}
	q := adapter.WorkItemQuery{Author: author, Since: opts.from, Until: opts.to}
	if opts.project != "" {
		q.Query = "project: {" + opts.project + "}"
	}
	items, err := adapter.SearchWorkItems(ctx, client, q)
	if err != nil {
		return err
	}
	r := build(items, host)
	r.Author, r.Project = author, opts.project
	if !opts.from.IsZero() {
		r.Since = opts.from.Format(worktime.DateLayout)
	}
	if !opts.to.IsZero() {
		r.Until = opts.to.Format(worktime.DateLayout)
	}
	return render(f, opts.exporter, r)
}

// build sums items per issue and per day.
func build(items []adapter.WorkItem, host hosts.Host) report {
	r := report{Items: []shared.WorkItem{}, Issues: []total{}, Days: []total{}}
	byIssue, byDay := map[string]int{}, map[string]int{}
	for _, it := range items {
		w := shared.New(it, host, "")
		r.Items = append(r.Items, w)
		r.Minutes += w.Minutes
		if _, ok := byIssue[w.Issue]; !ok {
			r.Issues = append(r.Issues, total{Key: w.Issue, URL: issueshared.IssueURL(host, w.Issue)})
		}
		byIssue[w.Issue] += w.Minutes
		if _, ok := byDay[w.Day()]; !ok {
			r.Days = append(r.Days, total{Key: w.Day()})
		}
		byDay[w.Day()] += w.Minutes
	}
	for i := range r.Issues {
		r.Issues[i].Minutes = byIssue[r.Issues[i].Key]
	}
	for i := range r.Days {
		r.Days[i].Minutes = byDay[r.Days[i].Key]
	}
	slices.SortStableFunc(r.Issues, func(a, b total) int {
		return cmp.Or(cmp.Compare(b.Minutes, a.Minutes), cmp.Compare(a.Key, b.Key))
	})
	slices.SortFunc(r.Days, func(a, b total) int { return cmp.Compare(a.Key, b.Key) })
	return r
}

func render(f *cmdutil.Factory, exporter *output.Exporter, r report) error {
	ios := f.IOStreams
	if exporter != nil {
		return exporter.Write(ios, r)
	}
	if len(r.Items) == 0 {
		fmt.Fprintf(ios.ErrOut, "no work items by %s match\n", r.Author)
		return nil
	}
	cs := ios.ColorScheme()
	issues := output.NewTable(ios, "issue", "time")
	for _, t := range r.Issues {
		issues.StyledRow(output.Cell{Text: t.Key, Style: cs.Accent}, output.Cell{Text: worktime.FormatMinutes(t.Minutes), Style: cs.Bold})
	}
	if err := issues.Render(); err != nil {
		return err
	}
	fmt.Fprintln(ios.Out)
	days := output.NewTable(ios, "date", "time")
	for _, t := range r.Days {
		days.StyledRow(output.Cell{Text: t.Key, Style: cs.Accent}, output.Cell{Text: worktime.FormatMinutes(t.Minutes), Style: cs.Bold})
	}
	if err := days.Render(); err != nil {
		return err
	}
	fmt.Fprintf(ios.Out, "\ntotal\t%s\n", worktime.FormatMinutes(r.Minutes))
	if ios.IsStdoutTTY() {
		fmt.Fprintf(ios.ErrOut, "%d work items by %s on %d issues\n", len(r.Items), r.Author, len(r.Issues))
	}
	return nil
}
