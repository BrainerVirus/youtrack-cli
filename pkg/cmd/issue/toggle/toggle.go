// Package toggle implements `ytrack issue vote`, `unvote`, `watch` and
// `unwatch`. Votes change only through a YouTrack command; the star is
// written through the REST API, which does not depend on the instance's
// language.
package toggle

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// VoteFields are the --json fields of vote and unvote.
var VoteFields = []string{"issue", "url", "votes", "voted"}

// WatchFields are the --json fields of watch and unwatch.
var WatchFields = []string{"issue", "url", "watching"}

// state is the switch as read back after the command.
type state struct {
	On    bool
	Votes int // vote count; votes only
}

// spec describes one of the four commands.
type spec struct {
	verb, short, long, done string
	// command is the YouTrack command keyword applied; "" when apply
	// changes the switch through the REST API.
	command string
	apply   func(ctx context.Context, c *transport.Client, id string, on bool) error
	on      bool
	fields  []string
	help    string
	read    func(ctx context.Context, c *transport.Client, id string) (state, error)
	export  func(r result, field string) any
	text    func(id string, s state) string
}

type result struct {
	Issue, URL string
	State      state
	spec       *spec
}

func (r result) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "issue":
			out[f] = r.Issue
		case "url":
			out[f] = r.URL
		default:
			out[f] = r.spec.export(r, f)
		}
	}
	return out
}

func readVotes(ctx context.Context, c *transport.Client, id string) (state, error) {
	v, err := adapter.GetVotes(ctx, c, id)
	return state{On: v.HasVote, Votes: v.Votes}, err
}

func readStar(ctx context.Context, c *transport.Client, id string) (state, error) {
	w, err := adapter.GetWatchers(ctx, c, id)
	return state{On: w.HasStar}, err
}

const voteHelp = `JSON fields:
  issue  the issue as given
  url    the issue's web URL
  votes  the issue's vote count after the change
  voted  true when you vote for the issue`

const watchHelp = `JSON fields:
  issue     the issue as given
  url       the issue's web URL
  watching  true when you starred the issue`

const commandNote = `The REST API cannot change this (it marks it read-only), so ytrack applies
YouTrack's %q command, then reads the issue back: if it did not change,
it exits 1 instead of reporting success. "ytrack issue command <id> %s
--assist" shows how YouTrack parses the command.`

const starNote = `ytrack changes the star through the REST API, then reads the issue back:
if it did not change, it exits 1 instead of reporting success.`

func voteExport(r result, f string) any {
	switch f {
	case "votes":
		return r.State.Votes
	case "voted":
		return r.State.On
	}
	return nil
}

func watchExport(r result, f string) any {
	if f == "watching" {
		return r.State.On
	}
	return nil
}

func voteText(id string, s state) string { return fmt.Sprintf("%s\t%d", id, s.Votes) }

func watchText(id string, s state) string {
	if s.On {
		return id + "\twatching"
	}
	return id + "\tnot watching"
}

// NewCmdVote returns the vote command.
func NewCmdVote(f *cmdutil.Factory) *cobra.Command {
	return newCmd(f, &spec{
		verb: "vote", command: "vote", on: true, short: "Vote for an issue", done: "Voted for",
		long: `Vote for an issue, as the vote button in YouTrack does, and print its vote
count. YouTrack does not let you vote for an issue you reported. Voting
again changes nothing.`,
		fields: VoteFields, help: voteHelp, read: readVotes, export: voteExport, text: voteText,
	})
}

// NewCmdUnvote returns the unvote command.
func NewCmdUnvote(f *cmdutil.Factory) *cobra.Command {
	return newCmd(f, &spec{
		verb: "unvote", command: "unvote", short: "Remove your vote from an issue", done: "Removed your vote from",
		long: `Remove your vote from an issue and print its vote count. Removing a vote
you have not cast changes nothing.`,
		fields: VoteFields, help: voteHelp, read: readVotes, export: voteExport, text: voteText,
	})
}

// NewCmdWatch returns the watch command.
func NewCmdWatch(f *cmdutil.Factory) *cobra.Command {
	return newCmd(f, &spec{
		verb: "watch", apply: adapter.SetStar, on: true, short: "Star an issue to get its notifications", done: "Starred",
		long: `Star an issue, as the star in YouTrack does, so you get its notifications.
Starring an issue you starred changes nothing.`,
		fields: WatchFields, help: watchHelp, read: readStar, export: watchExport, text: watchText,
	})
}

// NewCmdUnwatch returns the unwatch command.
func NewCmdUnwatch(f *cmdutil.Factory) *cobra.Command {
	return newCmd(f, &spec{
		verb: "unwatch", apply: adapter.SetStar, short: "Remove your star from an issue", done: "Unstarred",
		long: `Remove your star from an issue. You may still get its notifications
through a tag or a saved search ("ytrack issue watchers" shows how each
watcher watches).`,
		fields: WatchFields, help: watchHelp, read: readStar, export: watchExport, text: watchText,
	})
}

func newCmd(f *cmdutil.Factory, s *spec) *cobra.Command {
	var exporter *output.Exporter
	note := starNote
	if s.command != "" {
		note = fmt.Sprintf(commandNote, s.command, s.command)
	}
	cmd := &cobra.Command{
		Use:               s.verb + " {<id> | <url>}",
		Short:             s.short,
		Long:              s.long + "\n\n" + note + "\n\n" + s.help,
		Example:           fmt.Sprintf("  $ ytrack issue %[1]s APP-123\n  $ ytrack issue %[1]s APP-123 --json %[2]s", s.verb, s.fields[len(s.fields)-1]),
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
				return run(cmd.Context(), f, exporter, s, ref.ID)
			})
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, s.fields)
	return cmd
}

func run(ctx context.Context, f *cmdutil.Factory, exporter *output.Exporter, s *spec, id string) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	if s.apply != nil {
		if err := s.apply(ctx, client, id, s.on); err != nil {
			return shared.NotFound(err, id, host)
		}
	} else if err := applyCommand(ctx, client, s.command, id, host); err != nil {
		return err
	}
	st, err := s.read(ctx, client, id)
	if err != nil {
		return shared.NotFound(err, id, host)
	}
	if st.On != s.on {
		return unchanged(s, id)
	}
	res := result{Issue: id, URL: shared.IssueURL(host, id), State: st, spec: s}
	ios := f.IOStreams
	if exporter != nil {
		return exporter.Write(ios, res)
	}
	fmt.Fprintln(ios.Out, s.text(id, st))
	if ios.IsStderrTTY() {
		fmt.Fprintf(ios.ErrOut, "%s %s %s\n", ios.ErrColorScheme().SuccessIcon(), s.done, id)
	}
	return nil
}

func assistHint(command, id string) string {
	return fmt.Sprintf("`ytrack issue command %s %s --assist`", id, command)
}

func applyCommand(ctx context.Context, client *transport.Client, command, id string, host hosts.Host) error {
	_, err := adapter.ApplyCommand(ctx, client, adapter.Command{Query: command, IssueIDs: []string{id}})
	if err == nil {
		return nil
	}
	if apiErr, ok := errors.AsType[*transport.APIError](err); ok && apiErr.StatusCode == http.StatusBadRequest {
		return fmt.Errorf("%w\nnothing was changed; YouTrack rejected the %q command: %s shows how it parses", err, command, assistHint(command, id))
	}
	return shared.NotFound(err, id, host)
}

// unchanged is the error when the read-back shows the switch did not move.
func unchanged(s *spec, id string) error {
	if s.command == "" {
		return fmt.Errorf("YouTrack accepted the change, but the star on %s did not change", id)
	}
	why := ""
	if s.command == "vote" {
		why = " (YouTrack does not let you vote for an issue you reported)"
	}
	return fmt.Errorf("YouTrack accepted the %q command, but %s did not change%s; %s shows how YouTrack parses it", s.command, id, why, assistHint(s.command, id))
}
