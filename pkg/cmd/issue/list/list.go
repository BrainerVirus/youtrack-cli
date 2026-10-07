// Package list implements `ytrack issue list`.
package list

import (
	"fmt"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/text"
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// DefaultLimit is the number of issues listed without --limit, as in gh.
const DefaultLimit = 30

// tableAttrs are the issue attributes the table shows.
var tableAttrs = []string{"idReadable", "summary", "state", "assignee", "updated"}

type options struct {
	query    string
	project  string
	assignee string
	state    string
	sort     string
	limit    int
	exporter *output.Exporter
}

// NewCmdList returns the list command.
func NewCmdList(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List issues",
		Long: `List issues that match a YouTrack search query.

--query takes YouTrack's own search syntax and is sent unchanged. --project,
--assignee, --state and --sort add the matching query terms to it:
--project APP adds "project: APP", --assignee me adds "for: me", --state
'In Progress' adds "State: {In Progress}" and --sort 'updated desc' adds
"sort by: updated desc". Without any of them, every issue you can see is
listed in YouTrack's default order.

--limit 0 lists every matching issue.

JSON fields:
` + shared.IssueFieldsHelp,
		Example: `  $ ytrack issue list -q 'project: APP for: me #Unresolved'
  $ ytrack issue list --project APP --assignee me --state Open --sort 'updated desc'
  $ ytrack issue list -q '#Unresolved' --json idReadable,summary,state --jq '.[].idReadable'`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.limit < 0 {
				return clierr.FlagErrorf("invalid --limit %d: use a positive number, or 0 for all", opts.limit)
			}
			query, err := buildQuery(opts)
			if err != nil {
				return err
			}
			return run(cmd, f, opts, query)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.query, "query", "q", "", "YouTrack search `query`, sent as is")
	fl.StringVarP(&opts.project, "project", "p", "", "Only issues in this `project` (short name or name)")
	fl.StringVarP(&opts.assignee, "assignee", "a", "", "Only issues assigned to this `login` (\"me\" for yourself)")
	fl.StringVarP(&opts.state, "state", "s", "", "Only issues in this `state`")
	fl.StringVar(&opts.sort, "sort", "", "Sort by an `attribute`, optionally followed by asc or desc")
	fl.IntVarP(&opts.limit, "limit", "L", DefaultLimit, "Maximum number of issues to list (0 for all)")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, shared.IssueFields, cmdutil.WithoutJQShorthand())
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options, query string) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	attrs := tableAttrs
	if opts.exporter != nil {
		attrs = shared.Attrs(opts.exporter.Fields)
	}
	issues, err := adapter.ListIssues(cmd.Context(), client, adapter.IssueListOptions{Query: query, Attrs: attrs, Limit: opts.limit})
	if err != nil {
		return err
	}

	ios := f.IOStreams
	if opts.exporter != nil {
		out := make([]shared.Issue, len(issues))
		for i, is := range issues {
			out[i] = shared.Issue{Issue: is, Host: host}
		}
		return opts.exporter.Write(ios, out)
	}
	if len(issues) == 0 {
		fmt.Fprintln(ios.ErrOut, "no issues match your search")
		return nil
	}
	tbl := output.NewTable(ios, "id", "summary", "state", "assignee", "updated")
	now := time.Now()
	for _, is := range issues {
		assignee := ""
		if u := is.Assignee(); u != nil {
			assignee = u.Login
		}
		updated := ""
		if !is.Updated.IsZero() {
			if ios.IsStdoutTTY() {
				updated = text.RelativeTimeAgo(now, is.Updated)
			} else {
				updated = is.Updated.Format(time.RFC3339)
			}
		}
		tbl.Row(is.IDReadable, is.Summary, is.State(), assignee, updated)
	}
	return tbl.Render()
}

// buildQuery appends the convenience flags to --query as YouTrack terms.
func buildQuery(o *options) (string, error) {
	var parts []string
	if q := strings.TrimSpace(o.query); q != "" {
		parts = append(parts, q)
	}
	if o.project != "" {
		parts = append(parts, "project: "+term(o.project))
	}
	if o.assignee != "" {
		parts = append(parts, "for: "+term(o.assignee))
	}
	if o.state != "" {
		parts = append(parts, adapter.StateField+": "+term(o.state))
	}
	if o.sort != "" {
		s, err := sortTerm(o.sort)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " "), nil
}

// term quotes a query value with braces when it is more than one word.
func term(v string) string {
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, " \t,:#{}()") {
		return "{" + v + "}"
	}
	return v
}

func sortTerm(s string) (string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", clierr.FlagErrorf("--sort needs an attribute, e.g. --sort 'updated desc'")
	}
	attr, order := fields, ""
	if last := strings.ToLower(fields[len(fields)-1]); last == "asc" || last == "desc" {
		attr, order = fields[:len(fields)-1], last
		if len(attr) == 0 {
			return "", clierr.FlagErrorf("--sort needs an attribute before %q", last)
		}
	}
	out := "sort by: " + term(strings.Join(attr, " "))
	if order != "" {
		out += " " + order
	}
	return out, nil
}
