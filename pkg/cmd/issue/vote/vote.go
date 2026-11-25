// Package vote implements `ytrack issue vote` and `ytrack issue unvote`.
package vote

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields are the --json fields of vote and unvote.
var Fields = []string{"issue", "url", "votes", "voted"}

const fieldsHelp = `JSON fields:
  issue  the issue as given
  url    the issue's web URL
  votes  the issue's vote count after the change
  voted  true when you now vote for the issue`

type result struct {
	Issue, URL string
	Votes      adapter.IssueVotes
}

func (r result) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "issue":
			out[f] = r.Issue
		case "url":
			out[f] = r.URL
		case "votes":
			out[f] = r.Votes.Votes
		case "voted":
			out[f] = r.Votes.HasVote
		}
	}
	return out
}

// NewCmdVote returns the vote command.
func NewCmdVote(f *cmdutil.Factory) *cobra.Command {
	return newCmd(f, true)
}

// NewCmdUnvote returns the unvote command.
func NewCmdUnvote(f *cmdutil.Factory) *cobra.Command {
	return newCmd(f, false)
}

func newCmd(f *cmdutil.Factory, on bool) *cobra.Command {
	var exporter *output.Exporter
	verb, short, long := "vote", "Vote for an issue", `Vote for an issue, as the vote button in YouTrack does, and print its vote
count. YouTrack does not let you vote for an issue you reported. Voting
again changes nothing.`
	if !on {
		verb, short, long = "unvote", "Remove your vote from an issue", `Remove your vote from an issue and print its vote count. Removing a vote
you have not cast changes nothing.`
	}
	cmd := &cobra.Command{
		Use:               verb + " {<id> | <url>}",
		Short:             short,
		Long:              long + "\n\n" + fieldsHelp,
		Example:           fmt.Sprintf("  $ ytrack issue %[1]s APP-123\n  $ ytrack issue %[1]s APP-123 --json votes,voted", verb),
		Args:              cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			if err := shared.UseRefHostForWrite(f, ref); err != nil {
				return err
			}
			return f.WithIssueHostForWrite(cmd.Context(), ref.ID, func() error {
				return run(cmd, f, exporter, ref.ID, verb)
			})
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, Fields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, exporter *output.Exporter, id, verb string) error {
	ctx := cmd.Context()
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	// Issue.voters is read-only in the REST API; the vote and unvote
	// commands are how YouTrack's own UI changes it.
	if _, err := adapter.ApplyCommand(ctx, client, adapter.Command{Query: verb, IssueIDs: []string{id}}); err != nil {
		if apiErr, ok := errors.AsType[*transport.APIError](err); ok && apiErr.StatusCode == http.StatusBadRequest {
			return fmt.Errorf("%w\nnothing was changed (YouTrack does not let you vote for an issue you reported)", err)
		}
		return shared.NotFound(err, id, host)
	}
	votes, err := adapter.GetVotes(ctx, client, id)
	if err != nil {
		return shared.NotFound(err, id, host)
	}
	res := result{Issue: id, URL: shared.IssueURL(host, id), Votes: votes}
	ios := f.IOStreams
	if exporter != nil {
		return exporter.Write(ios, res)
	}
	fmt.Fprintf(ios.Out, "%s\t%d\n", id, votes.Votes)
	if ios.IsStderrTTY() {
		what := "Voted for"
		if verb == "unvote" {
			what = "Removed your vote from"
		}
		fmt.Fprintf(ios.ErrOut, "%s %s %s (%d votes)\n", ios.ErrColorScheme().SuccessIcon(), what, id, votes.Votes)
	}
	return nil
}
