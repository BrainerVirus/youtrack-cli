// Package watchers implements `ytrack issue watchers`.
package watchers

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields are the --json fields of a watcher.
var Fields = []string{"login", "fullName", "starred"}

type watcher struct{ adapter.Watcher }

func (w watcher) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "login":
			out[f] = w.User.Login
		case "fullName":
			out[f] = w.User.FullName
		case "starred":
			out[f] = w.Starred
		}
	}
	return out
}

// NewCmdWatchers returns the watchers command.
func NewCmdWatchers(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:   "watchers {<id> | <url>}",
		Short: "List the users who watch an issue",
		Long: `List the users who watch an issue and get its notifications: login, full
name, and whether they starred it (the others watch it through a tag or a
saved search).

JSON fields:
  login     the watcher's login
  fullName  the watcher's full name
  starred   true when the watcher starred the issue`,
		Example: `  $ ytrack issue watchers APP-123
  $ ytrack issue watchers APP-123 --json login --jq '.[].login'`,
		Args:              cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			var ws adapter.IssueWatchers
			err = f.WithIssueHost(cmd.Context(), ref.ID, func() error {
				client, host, err := f.APIClient()
				if err != nil {
					return err
				}
				ws, err = adapter.GetWatchers(cmd.Context(), client, ref.ID)
				return shared.NotFound(err, ref.ID, host)
			})
			if err != nil {
				return err
			}
			ios := f.IOStreams
			out := make([]watcher, len(ws.Watchers))
			for i, w := range ws.Watchers {
				out[i] = watcher{w}
			}
			if exporter != nil {
				return exporter.Write(ios, out)
			}
			if len(out) == 0 {
				fmt.Fprintf(ios.ErrOut, "nobody watches %s\n", ref.ID)
				return nil
			}
			tbl := output.NewTable(ios, "login", "name", "starred")
			cs := ios.ColorScheme()
			for _, w := range out {
				starred := ""
				if w.Starred {
					starred = "yes"
				}
				tbl.StyledRow(
					output.Cell{Text: w.User.Login, Style: cs.Bold},
					output.Cell{Text: w.User.FullName},
					output.Cell{Text: starred, Style: cs.Dim},
				)
			}
			return tbl.Render()
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, Fields)
	return cmd
}
