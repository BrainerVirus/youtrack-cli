// Package view implements `ytrack issue view`.
package view

import (
	"fmt"
	"io"
	"slices"
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

// Fields lists the --json fields of issue view.
var Fields = append(slices.Clone(shared.IssueFields), "comments")

// humanAttrs are the issue attributes the human view shows.
var humanAttrs = []string{
	"idReadable", "summary", "description", "project", "reporter", "created",
	"updated", "resolved", "tags", "customFields", "commentsCount",
}

type options struct {
	ref           shared.Ref
	limitGiven    bool
	web           bool
	comments      bool
	commentsLimit int
	exporter      *output.Exporter
}

// NewCmdView returns the view command.
func NewCmdView(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "view {<id> | <url>}",
		Short: "Show an issue",
		Long: `Show an issue's key fields and description.

The issue is an ID such as APP-123 or its web URL. A URL selects its host.

--comments adds the latest comments (--comments-limit of them). --web opens
the issue in the browser instead. Deleted comments are not shown.

JSON fields:
` + shared.IssueFieldsHelp + `
  comments      [{id, text, author, created, updated, url}]: all comments,
                or the latest --comments-limit when that flag is given`,
		Example: `  $ ytrack issue view APP-123
  $ ytrack issue view https://acme.youtrack.cloud/issue/APP-123 --comments
  $ ytrack issue view APP-123 --json summary,state,comments --jq '.comments | length'`,
		Args: cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			opts.limitGiven = cmd.Flags().Changed("comments-limit")
			if opts.commentsLimit < 1 {
				return clierr.FlagErrorf("invalid --comments-limit %d: use a positive number", opts.commentsLimit)
			}
			if err := cmdutil.MutuallyExclusive("cannot use `--web` with `--json` or `--comments`", opts.web, opts.exporter != nil || opts.comments); err != nil {
				return err
			}
			if err := shared.UseRefHost(f, ref, !opts.web); err != nil {
				return err
			}
			if opts.web {
				return openWeb(f, ref.ID)
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.BoolVarP(&opts.web, "web", "w", false, "Open the issue in the browser")
	fl.BoolVarP(&opts.comments, "comments", "c", false, "Show the latest comments")
	fl.IntVar(&opts.commentsLimit, "comments-limit", 10, "Number of latest comments --comments shows")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, Fields)
	return cmd
}

func openWeb(f *cmdutil.Factory, id string) error {
	cfg, err := f.Config()
	if err != nil {
		return err
	}
	host, err := f.ResolveHost(cfg)
	if err != nil {
		return err
	}
	u := shared.IssueURL(host, id)
	if f.IOStreams.IsStderrTTY() {
		fmt.Fprintf(f.IOStreams.ErrOut, "Opening %s in your browser.\n", u)
	}
	return f.Browser.Browse(u)
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	id := opts.ref.ID

	if opts.exporter != nil {
		wantComments := slices.Contains(opts.exporter.Fields, "comments")
		var extra []string
		if wantComments {
			extra = []string{"idReadable"} // comment URLs need it
		}
		if wantComments && opts.limitGiven {
			extra = append(extra, "commentsCount")
		}
		issue, err := adapter.GetIssue(ctx, client, id, shared.Attrs(opts.exporter.Fields, extra...))
		if err != nil {
			return shared.NotFound(err, id, host)
		}
		out := shared.Issue{Issue: issue, Host: host}
		if wantComments {
			skip, limit := 0, 0
			if opts.limitGiven {
				skip, limit = max(0, issue.CommentsCount-opts.commentsLimit), opts.commentsLimit
			}
			if out.Comments, err = adapter.ListComments(ctx, client, id, skip, limit); err != nil {
				return err
			}
		}
		return opts.exporter.Write(f.IOStreams, out)
	}

	issue, err := adapter.GetIssue(ctx, client, id, humanAttrs)
	if err != nil {
		return shared.NotFound(err, id, host)
	}
	var comments []adapter.Comment
	if opts.comments && issue.CommentsCount > 0 {
		skip := max(0, issue.CommentsCount-opts.commentsLimit)
		if comments, err = adapter.ListComments(ctx, client, id, skip, opts.commentsLimit); err != nil {
			return err
		}
	}
	p := printer{w: f.IOStreams.Out, tty: f.IOStreams.IsStdoutTTY(), now: time.Now()}
	p.issue(shared.Issue{Issue: issue, Host: host}, opts.comments, comments)
	return nil
}

type printer struct {
	w   io.Writer
	tty bool
	now time.Time
}

func (p printer) time(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if p.tty {
		return text.RelativeTimeAgo(p.now, t)
	}
	return t.UTC().Format(time.RFC3339)
}

// kv prints one "Key: value" line; both are sanitized onto one line.
func (p printer) kv(key, value string) {
	if value != "" {
		fmt.Fprintf(p.w, "%-11s %s\n", output.SanitizeCell(key)+":", output.SanitizeCell(value))
	}
}

func userString(u *adapter.User) string {
	if u == nil {
		return ""
	}
	if u.FullName == "" || u.FullName == u.Login {
		return u.Login
	}
	return u.FullName + " (" + u.Login + ")"
}

func (p printer) issue(is shared.Issue, showComments bool, comments []adapter.Comment) {
	fmt.Fprintf(p.w, "%s: %s\n", output.SanitizeCell(is.IDReadable), output.SanitizeCell(is.Summary))
	if is.Project != nil {
		project := is.Project.ShortName
		if is.Project.Name != "" && is.Project.Name != project {
			project += " (" + is.Project.Name + ")"
		}
		p.kv("Project", project)
	}
	p.kv("Reporter", userString(is.Reporter))
	p.kv("Created", p.time(is.Created))
	p.kv("Updated", p.time(is.Updated))
	if is.Resolved != nil {
		p.kv("Resolved", p.time(*is.Resolved))
	}
	p.kv("Tags", strings.Join(is.Tags, ", "))
	for _, cf := range is.CustomFields {
		value := cf.String()
		if cf.Name == adapter.AssigneeField {
			value = userString(is.Assignee())
		}
		p.kv(cf.Name, value)
	}

	fmt.Fprintln(p.w)
	desc := strings.TrimSpace(output.SanitizeText(is.Description))
	if desc == "" {
		desc = "No description provided."
	}
	fmt.Fprintln(p.w, desc)
	fmt.Fprintln(p.w)

	switch {
	case showComments && len(comments) > 0:
		fmt.Fprintf(p.w, "Comments (latest %d of %d):\n", len(comments), is.CommentsCount)
		for _, c := range comments {
			fmt.Fprintf(p.w, "\n%s • %s\n", output.SanitizeCell(userString(c.Author)), p.time(c.Created))
			fmt.Fprintln(p.w, text.Indent(strings.TrimSpace(output.SanitizeText(c.Text)), "  "))
		}
		fmt.Fprintln(p.w)
	case showComments:
		fmt.Fprintln(p.w, "No comments.")
	case is.CommentsCount > 0:
		fmt.Fprintf(p.w, "%s; use --comments to view them.\n", text.Pluralize(is.CommentsCount, "comment"))
	}
	fmt.Fprintf(p.w, "View this issue on YouTrack: %s\n", shared.IssueURL(is.Host, is.IDReadable))
}
