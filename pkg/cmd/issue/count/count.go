// Package count implements `ytrack issue count`.
package count

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields are the --json fields of issue count.
var Fields = []string{"query", "count"}

// poll is how issue count waits while YouTrack is still counting.
var poll = adapter.CountPoll{Interval: 100 * time.Millisecond, MaxInterval: 2 * time.Second, Timeout: time.Minute}

type result struct {
	Query string
	Count int
}

func (r result) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "query":
			out[f] = r.Query
		case "count":
			out[f] = r.Count
		}
	}
	return out
}

// NewCmdCount returns the count command.
func NewCmdCount(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:   "count [<query>]",
		Short: "Count the issues that match a query",
		Long: `Print the number of issues that match a YouTrack search query, without
listing them. The query is YouTrack's own syntax, sent as is, as with
"ytrack issue list --query"; no query counts every issue you can see.

YouTrack may answer that it is still counting; ytrack then asks again,
waiting a little longer each time, for up to a minute.

JSON fields:
  query  the query as sent
  count  the number of matching issues`,
		Example: `  $ ytrack issue count 'project: APP #Unresolved'
  $ ytrack issue count 'Assignee: me State: {In Progress}' --json count --jq .count`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			client, _, err := f.APIClient()
			if err != nil {
				return err
			}
			n, err := adapter.CountIssues(cmd.Context(), client, query, poll)
			if err != nil {
				if apiErr, ok := errors.AsType[*transport.APIError](err); ok && apiErr.StatusCode == http.StatusBadRequest {
					return fmt.Errorf("%w\n`ytrack issue list -q %s --query-assist` shows YouTrack's completions for the query", err, shared.ShellArg(query))
				}
				return err
			}
			if exporter != nil {
				return exporter.Write(f.IOStreams, result{Query: query, Count: n})
			}
			fmt.Fprintln(f.IOStreams.Out, strconv.Itoa(n))
			return nil
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, Fields)
	return cmd
}
