// Package activities implements `ytrack issue activities`.
package activities

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields are the --json fields of an activity.
var Fields = []string{"id", "timestamp", "author", "category", "field", "added", "removed"}

// Categories maps each --category name to YouTrack's activity category ID,
// in the order help lists them.
var Categories = []struct{ Name, ID string }{
	{"created", "IssueCreatedCategory"},
	{"resolved", "IssueResolvedCategory"},
	{"summary", "SummaryCategory"},
	{"description", "DescriptionCategory"},
	{"fields", "CustomFieldCategory"},
	{"comments", "CommentsCategory"},
	{"attachments", "AttachmentsCategory"},
	{"links", "LinksCategory"},
	{"tags", "TagsCategory"},
	{"work", "WorkItemCategory"},
	{"project", "ProjectCategory"},
	{"visibility", "IssueVisibilityCategory"},
	{"votes", "VotersCategory"},
}

type activity struct{ adapter.Activity }

func (a activity) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "id":
			out[f] = a.ID
		case "timestamp":
			if a.Timestamp.IsZero() {
				out[f] = nil
			} else {
				out[f] = a.Timestamp.UTC().Format(time.RFC3339)
			}
		case "author":
			if a.Author == nil {
				out[f] = nil
			} else {
				out[f] = map[string]any{"login": a.Author.Login, "fullName": a.Author.FullName}
			}
		case "category":
			out[f] = categoryName(a.Category)
		case "field":
			out[f] = a.Field
		case "added":
			out[f] = a.Added
		case "removed":
			out[f] = a.Removed
		}
	}
	return out
}

// categoryName is the --category name of a YouTrack category ID, or the ID
// itself when it has none.
func categoryName(id string) string {
	for _, c := range Categories {
		if c.ID == id {
			return c.Name
		}
	}
	return id
}

// categoryIDs resolves --category values: a name from Categories or a
// YouTrack category ID (ending in "Category"); none means all of
// Categories.
func categoryIDs(values []string) ([]string, error) {
	var ids []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		id := ""
		for _, c := range Categories {
			if strings.EqualFold(c.Name, v) || strings.EqualFold(c.ID, v) {
				id = c.ID
			}
		}
		if id == "" && strings.HasSuffix(v, "Category") {
			id = v
		}
		if id == "" {
			names := make([]string, len(Categories))
			for i, c := range Categories {
				names[i] = c.Name
			}
			return nil, clierr.FlagErrorf("unknown --category %q: use one of %s, or a YouTrack category ID such as CustomFieldCategory", v, strings.Join(names, ", "))
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		for _, c := range Categories {
			ids = append(ids, c.ID)
		}
	}
	return ids, nil
}

func categoriesHelp() string {
	var b strings.Builder
	for _, c := range Categories {
		fmt.Fprintf(&b, "  %-12s %s\n", c.Name, c.ID)
	}
	return b.String()
}

// NewCmdActivities returns the activities command.
func NewCmdActivities(f *cmdutil.Factory) *cobra.Command {
	var (
		exporter   *output.Exporter
		categories []string
		limit      int
	)
	cmd := &cobra.Command{
		Use:     "activities {<id> | <url>}",
		Aliases: []string{"history"},
		Short:   "Show the history of an issue",
		Long: `Show what changed on an issue, oldest first: when, who, what kind of change,
the field, and the values removed and added (entities by name, login or
issue ID).

--category keeps one kind of change; repeat it or separate names with
commas. Without it every kind below is shown. A YouTrack category ID such
as CustomFieldCategory is also accepted.

` + categoriesHelp() + `
--limit caps the number of changes (0, the default, shows all).

JSON fields:
  id         activity ID
  timestamp  when it happened, RFC 3339 UTC
  author     {login, fullName} of who made the change
  category   the --category name, or YouTrack's ID when it has none
  field      the field or attribute that changed, e.g. State, summary
  added      the values added, as text
  removed    the values removed, as text`,
		Example: `  $ ytrack issue activities APP-123
  $ ytrack issue activities APP-123 --category fields,comments
  $ ytrack issue activities APP-123 --category fields --json timestamp,field,added --jq '.[] | select(.field == "State")'`,
		Args:              cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			if limit < 0 {
				return clierr.FlagErrorf("invalid --limit %d: use a positive number, or 0 for all", limit)
			}
			ids, err := categoryIDs(categories)
			if err != nil {
				return err
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			var list []adapter.Activity
			err = f.WithIssueHost(cmd.Context(), ref.ID, func() error {
				client, host, err := f.APIClient()
				if err != nil {
					return err
				}
				list, err = adapter.ListActivities(cmd.Context(), client, ref.ID, adapter.ActivityQuery{Categories: ids, Limit: limit})
				return shared.NotFound(err, ref.ID, host)
			})
			if err != nil {
				return err
			}
			return render(f, exporter, ref.ID, list)
		},
	}
	fl := cmd.Flags()
	fl.StringSliceVarP(&categories, "category", "c", nil, "Only changes of this `kind` (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("category", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		names := make([]string, len(Categories))
		for i, c := range Categories {
			names[i] = c.Name
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
	fl.IntVarP(&limit, "limit", "L", 0, "Maximum number of changes to show (0 for all)")
	cmdutil.AddJSONFlags(cmd, &exporter, Fields)
	return cmd
}

func render(f *cmdutil.Factory, exporter *output.Exporter, id string, list []adapter.Activity) error {
	ios := f.IOStreams
	out := make([]activity, len(list))
	for i, a := range list {
		out[i] = activity{a}
	}
	if exporter != nil {
		return exporter.Write(ios, out)
	}
	if len(out) == 0 {
		fmt.Fprintf(ios.ErrOut, "no matching changes on %s\n", id)
		return nil
	}
	tbl := output.NewTable(ios, "time", "author", "category", "field", "removed", "added")
	cs := ios.ColorScheme()
	for _, a := range out {
		when, login := "", ""
		if !a.Timestamp.IsZero() {
			when = a.Timestamp.UTC().Format("2006-01-02 15:04")
		}
		if a.Author != nil {
			login = a.Author.Login
		}
		tbl.StyledRow(
			output.Cell{Text: when, Style: cs.Dim},
			output.Cell{Text: login},
			output.Cell{Text: categoryName(a.Category), Style: cs.Dim},
			output.Cell{Text: a.Field, Style: cs.Bold},
			output.Cell{Text: strings.Join(a.Removed, ", ")},
			output.Cell{Text: strings.Join(a.Added, ", "), Style: cs.Accent},
		)
	}
	return tbl.Render()
}
