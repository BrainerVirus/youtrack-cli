// Package list implements `ytrack issue list`.
package list

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/text"
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// DefaultLimit is the number of issues listed without --limit, as in gh.
const DefaultLimit = 30

// tableAttrs are the issue attributes the table shows.
var tableAttrs = []string{"idReadable", "summary", "state", "priority", "assignee", "updated"}

type options struct {
	query    string
	project  string
	assignee string
	state    string
	sort     string
	all      bool
	limit    int
	assist   bool
	exporter *output.Exporter
	// assistExporter is --json with --query-assist, over AssistFields.
	assistExporter *output.Exporter
}

// AssistFields are the --json fields of a --query-assist completion.
var AssistFields = []string{"query", "option", "description", "completionStart", "completionEnd"}

type completion struct {
	adapter.Suggestion
	query string
}

func (c completion) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "query":
			out[f] = c.Apply(c.query)
		case "option":
			out[f] = c.Option
		case "description":
			out[f] = c.Description
		case "completionStart":
			out[f] = c.CompletionStart
		case "completionEnd":
			out[f] = c.CompletionEnd
		}
	}
	return out
}

// assistExporter reads --json, --jq and --template for --query-assist,
// whose fields are AssistFields, not issue fields.
func assistExporter(c *cobra.Command) (*output.Exporter, error) {
	fl := c.Flags()
	jq, _ := fl.GetString("jq")
	tpl, _ := fl.GetString("template")
	fields, _ := fl.GetStringSlice("json")
	if jq != "" && tpl != "" {
		return nil, clierr.FlagErrorf("cannot use `--jq` and `--template` together")
	}
	if !fl.Changed("json") {
		if jq == "" && tpl == "" {
			return nil, nil
		}
		fields = slices.Clone(AssistFields)
	}
	for _, name := range fields {
		if !slices.Contains(AssistFields, name) {
			return nil, clierr.FlagErrorf("unknown JSON field for --query-assist: %q\nAvailable fields:\n  %s", name, strings.Join(AssistFields, "\n  "))
		}
	}
	return &output.Exporter{Fields: fields, Format: output.Format{JQ: jq, Template: tpl}}, nil
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
--project APP adds "project: APP", --assignee jdoe adds "Assignee: jdoe"
(--assignee me sends your own login), --state 'In Progress' adds
"State: {In Progress}" and --sort 'updated desc' adds "sort by: updated desc".
These terms use field names, not keywords such as "for", which YouTrack
translates into the instance's language, so they work on every locale.

Unless you pass --query or --state, only unresolved issues are listed
("#Unresolved" is added to the query); --all lists resolved ones too.

--limit 0 lists every matching issue.

--query-assist lists nothing: it prints YouTrack's completions for the end
of --query, each as the full query it makes and a description, to find a
field name, value or keyword. Quote the query with a trailing space to ask
what may follow it. With --query-assist, --json takes these fields instead:
  query            the full query the completion makes
  option           the completion itself
  description      YouTrack's description of it
  completionStart  where the replaced text starts (UTF-16 units)
  completionEnd    where it ends

JSON fields:
` + shared.IssueFieldsHelp,
		Example: `  $ ytrack issue list -q 'project: APP Assignee: me #Unresolved'
  $ ytrack issue list --project APP --assignee me --state Open --sort 'updated desc'
  $ ytrack issue list --project APP --all
  $ ytrack issue list -q '#Unresolved' --json idReadable,summary,state --jq '.[].idReadable'
  $ ytrack issue list -q 'State: ' --query-assist`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.exporter != nil && looksLikeJQ(opts.query) {
				return clierr.FlagErrorf("-q is --query in `issue list` and %q looks like a jq expression; did you mean --jq?", opts.query)
			}
			if opts.limit < 0 {
				return clierr.FlagErrorf("invalid --limit %d: use a positive number, or 0 for all", opts.limit)
			}
			if opts.assist {
				if opts.query == "" {
					return clierr.FlagErrorf("`--query-assist` completes --query; pass the query to complete with -q")
				}
				return assist(cmd, f, opts.query, opts.assistExporter)
			}
			if _, err := buildQuery(opts); err != nil {
				return err
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.query, "query", "q", "", "YouTrack search `query`, sent as is")
	fl.StringVarP(&opts.project, "project", "p", "", "Only issues in this `project` (short name or name)")
	_ = cmd.RegisterFlagCompletionFunc("project", shared.CompleteProjects(f))
	fl.StringVarP(&opts.assignee, "assignee", "a", "", "Only issues assigned to this `login` (\"me\" for yourself)")
	fl.StringVarP(&opts.state, "state", "s", "", "Only issues in this `state`")
	fl.StringVar(&opts.sort, "sort", "", "Sort by an `attribute`, optionally followed by asc or desc")
	fl.BoolVar(&opts.all, "all", false, "Include resolved issues (no implicit #Unresolved)")
	fl.IntVarP(&opts.limit, "limit", "L", DefaultLimit, "Maximum number of issues to list (0 for all)")
	fl.BoolVar(&opts.assist, "query-assist", false, "Print YouTrack's completions for --query instead of issues")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, shared.IssueFields, cmdutil.WithoutJQShorthand())
	issueFlags := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		if !opts.assist {
			return issueFlags(c, args)
		}
		e, err := assistExporter(c)
		opts.assistExporter = e
		return err
	}
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(opts.assignee), "me") {
		login, err := f.CurrentLogin(cmd.Context(), client, host)
		if err != nil {
			return err
		}
		opts.assignee = login
	}
	query, err := buildQuery(opts)
	if err != nil {
		return err
	}
	attrs := tableAttrs
	if opts.exporter != nil {
		attrs = shared.Attrs(opts.exporter.Fields)
	}
	issues, err := adapter.ListIssues(cmd.Context(), client, adapter.IssueListOptions{Query: query, Attrs: attrs, Limit: opts.limit})
	if err != nil {
		return assigneeHint(err, opts.assignee)
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
	tbl := output.NewTable(ios, "id", "summary", "state", "priority", "assignee", "updated")
	cs := ios.ColorScheme()
	now := f.Clock()
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
		state := is.State()
		resolved := is.Resolved != nil
		tbl.StyledRow(
			output.Cell{Text: is.IDReadable, Style: cs.Accent},
			output.Cell{Text: is.Summary, Style: cs.Bold},
			output.Cell{Text: state, Style: func(s string) string { return cs.State(s, resolved) }},
			output.Cell{Text: is.Priority(), Style: cs.Priority},
			output.Cell{Text: assignee},
			output.Cell{Text: updated, Style: cs.Dim},
		)
	}
	return tbl.Render()
}

// assist prints YouTrack's completions for the end of query.
func assist(cmd *cobra.Command, f *cmdutil.Factory, query string, exporter *output.Exporter) error {
	client, _, err := f.APIClient()
	if err != nil {
		return err
	}
	suggestions, err := adapter.AssistSearch(cmd.Context(), client, query)
	if err != nil {
		return err
	}
	ios := f.IOStreams
	if exporter != nil {
		out := make([]completion, len(suggestions))
		for i, s := range suggestions {
			out[i] = completion{Suggestion: s, query: query}
		}
		return exporter.Write(ios, out)
	}
	if len(suggestions) == 0 {
		fmt.Fprintf(ios.ErrOut, "YouTrack has no completions for %q\n", query)
		return nil
	}
	tbl := output.NewTable(ios, "query", "description")
	cs := ios.ColorScheme()
	for _, s := range suggestions {
		tbl.StyledRow(output.Cell{Text: s.Apply(query), Style: cs.Bold}, output.Cell{Text: s.Description, Style: cs.Dim})
	}
	return tbl.Render()
}

// assigneeHint adds, to YouTrack's "cannot parse the query" answer, the
// likely cause when --assignee is set: YouTrack rejects a search for a user
// it does not know the same way as a malformed query.
func assigneeHint(err error, assignee string) error {
	apiErr, ok := errors.AsType[*transport.APIError](err)
	if !ok || assignee == "" || apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "invalid_query" {
		return err
	}
	return fmt.Errorf("%w; is %q a YouTrack login? (--assignee takes a login or me)", err, strings.TrimSpace(assignee))
}

// DefaultFilter is added to the query when neither --query, --state nor
// --all is given.
const DefaultFilter = "#Unresolved"

// buildQuery appends the convenience flags to --query as YouTrack terms.
func buildQuery(o *options) (string, error) {
	var parts []string
	if q := strings.TrimSpace(o.query); q != "" {
		parts = append(parts, q)
	} else if o.state == "" && !o.all {
		parts = append(parts, DefaultFilter)
	}
	for _, t := range []struct{ flag, attr, value string }{
		{"--project", "project", o.project},
		{"--assignee", adapter.AssigneeField, o.assignee},
		{"--state", adapter.StateField, o.state},
	} {
		if t.value == "" {
			continue
		}
		v, err := term(t.flag, t.value)
		if err != nil {
			return "", err
		}
		parts = append(parts, t.attr+": "+v)
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
// YouTrack's query syntax has no documented escape for a brace inside
// braces, so values with braces are refused rather than mangled.
func term(flag, v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, "{}") {
		return "", clierr.FlagErrorf("%s %q contains a brace, which a query term cannot hold; write the query with --query instead", flag, v)
	}
	if strings.ContainsAny(v, " \t,:#()") {
		return "{" + v + "}", nil
	}
	return v, nil
}

// looksLikeJQ reports whether a --query value is probably a jq expression
// meant for --jq, which -q means in gh.
func looksLikeJQ(q string) bool {
	q = strings.TrimSpace(q)
	return strings.HasPrefix(q, ".") || strings.Contains(q, "|") || strings.Contains(q, "[]")
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
	t, err := term("--sort", strings.Join(attr, " "))
	if err != nil {
		return "", err
	}
	out := "sort by: " + t
	if order != "" {
		out += " " + order
	}
	return out, nil
}
