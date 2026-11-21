// Package unlink implements `ytrack issue unlink`.
package unlink

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/link"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

type options struct {
	phrase   string
	exporter *output.Exporter
}

// NewCmdUnlink returns the unlink command.
func NewCmdUnlink(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "unlink {<id> | <url>} {<target> | <url>}... --type <phrase>",
		Short: "Remove links between issues",
		Long: `Remove the links that read "<id> <phrase> <target>":
` + "`ytrack issue unlink APP-1 APP-2 --type 'depends on'`" + ` makes APP-1 no longer
depend on APP-2. A target that is not linked that way is an error, and
nothing is removed.

` + link.TypeHelp + `

JSON fields (the removed link):
` + link.LinkFieldsHelp,
		Example: `  $ ytrack issue unlink APP-1 APP-2 --type 'depends on'
  $ ytrack issue unlink APP-1 APP-9 --type relates`,
		Args:              cmdutil.MinimumArgs(2, "expected an issue ID or URL and one or more target issues, e.g. APP-1 APP-2 --type 'depends on'"),
		ValidArgsFunction: shared.CompleteIssues(f, -1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.phrase) == "" {
				return clierr.FlagErrorf("`--type` is required, e.g. --type 'depends on'; `ytrack issue link --list-types` lists the link types")
			}
			refs, err := link.Refs(args)
			if err != nil {
				return err
			}
			if err := shared.UseRefHostForWrite(f, link.HostRef(refs)); err != nil {
				return err
			}
			return f.WithIssueHostForWrite(cmd.Context(), refs[0].ID, func() error { return run(cmd.Context(), f, opts, refs) })
		},
	}
	cmd.Flags().StringVar(&opts.phrase, "type", "", "Link `phrase`, e.g. 'depends on' (see `issue link --list-types`)")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, link.LinkFields)
	return cmd
}

func run(ctx context.Context, f *cmdutil.Factory, opts *options, refs []shared.Ref) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	end, err := link.ResolveType(ctx, client, opts.phrase)
	if err != nil {
		return err
	}
	issue := refs[0].ID
	linked, err := adapter.LinkedIssues(ctx, client, issue, end.LinkID)
	if err != nil {
		return shared.NotFound(err, issue, host)
	}
	// YouTrack removes a linked issue by its database ID; check every
	// target before removing any.
	dbIDs := make([]string, 0, len(refs)-1)
	var missing []string
	for _, t := range refs[1:] {
		id, ok := find(linked, t.ID)
		if !ok {
			missing = append(missing, t.ID)
		}
		dbIDs = append(dbIDs, id)
	}
	if len(missing) > 0 {
		return fmt.Errorf("no such link: %s %s %s; nothing was removed (see `ytrack issue link %s --list`)",
			issue, output.SanitizeCell(end.Phrase), strings.Join(missing, ", "), issue)
	}
	res := link.Result{Issue: issue, IssueLink: adapter.IssueLink{LinkEnd: end, Issues: []string{}}}
	for i, t := range refs[1:] {
		if err := adapter.RemoveLink(ctx, client, issue, end.LinkID, dbIDs[i]); err != nil {
			return err
		}
		res.Issues = append(res.Issues, t.ID)
	}
	ios := f.IOStreams
	if opts.exporter != nil {
		return opts.exporter.Write(ios, res)
	}
	fmt.Fprintf(ios.ErrOut, "%s Removed link: %s %s %s\n", ios.ErrColorScheme().SuccessIcon(), issue, output.SanitizeCell(end.Phrase), strings.Join(res.Issues, ", "))
	return nil
}

// find returns the database ID of the linked issue that id names, by
// readable or database ID.
func find(linked []adapter.LinkedIssue, id string) (string, bool) {
	for _, l := range linked {
		if strings.EqualFold(l.IDReadable, id) || l.ID == id {
			return l.ID, true
		}
	}
	return "", false
}
