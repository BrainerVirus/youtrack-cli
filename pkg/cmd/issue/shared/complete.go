package shared

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// completionTimeout keeps a shell's tab press responsive on a slow server;
// a request that takes longer completes nothing.
const completionTimeout = 2 * time.Second

// completionLimit caps the issues one completion request fetches.
const completionLimit = 50

// CompleteIssues completes issue IDs for the first maxArgs positional
// arguments (every argument when maxArgs < 0) with the 50 most recently
// updated unresolved issues, narrowed to a project once the word has one
// ("APP-"). Each suggestion carries the issue summary. Errors, such as no
// login, complete nothing and print nothing.
func CompleteIssues(f *cmdutil.Factory, maxArgs int) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if maxArgs >= 0 && len(args) >= maxArgs {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		client, _, err := f.APIClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		query := "#Unresolved sort by: updated desc"
		if project, _, ok := strings.Cut(toComplete, "-"); ok && project != "" && !strings.ContainsAny(project, " {}") {
			query = "project: " + project + " " + query
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
		defer cancel()
		issues, err := adapter.ListIssues(ctx, client, adapter.IssueListOptions{
			Query: query, Attrs: []string{"idReadable", "summary"}, Limit: completionLimit,
		})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []cobra.Completion
		for _, is := range issues {
			if strings.HasPrefix(strings.ToUpper(is.IDReadable), strings.ToUpper(toComplete)) {
				out = append(out, cobra.CompletionWithDesc(is.IDReadable, oneLine(is.Summary)))
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteProjects completes a --project flag with the short names of
// projects that are not archived.
func CompleteProjects(f *cmdutil.Factory) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		client, _, err := f.APIClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
		defer cancel()
		projects, err := adapter.ListProjects(ctx, client)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []cobra.Completion
		for _, p := range projects {
			if !p.Archived && strings.HasPrefix(strings.ToUpper(p.ShortName), strings.ToUpper(toComplete)) {
				out = append(out, cobra.CompletionWithDesc(p.ShortName, oneLine(p.Name)))
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// oneLine makes server text safe for a completion description: one line,
// no control characters (shells print descriptions raw).
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
}
