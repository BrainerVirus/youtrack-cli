// Package link implements `ytrack issue link`, and the link type lookup and
// JSON output that `ytrack issue unlink` shares.
package link

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// LinkFields are the --json fields of a link: the links `--list` prints and
// the one `issue link` or `issue unlink` changed.
var LinkFields = []string{"issue", "type", "phrase", "direction", "issues"}

// TypeFields are the --json fields of a link type (`--list-types`).
var TypeFields = []string{"id", "name", "sourceToTarget", "targetToSource", "directed"}

// LinkFieldsHelp documents LinkFields for command help.
const LinkFieldsHelp = `  issue      the issue the link is on, as given
  type       link type name, e.g. Depend
  phrase     what the link says, read as "<issue> <phrase> <each of issues>",
             e.g. "depends on"
  direction  OUTWARD (the issue is the link's source), INWARD (its target)
             or BOTH (an undirected link type)
  issues     the readable IDs of the linked issues`

// TypeHelp documents --type for command help.
const TypeHelp = `--type takes a link phrase as the instance's link types define it, such as
"depends on", "is required for", "subtask of", "parent for", "duplicates" or
"relates to"; case is ignored. The phrase picks the direction: "A depends on
B" and "B is required for A" are the same link. An undirected link type also
answers to its name, e.g. "Relates"; a directed type's name, e.g. "Depend",
does not say which way the link goes and is refused. ` + "`ytrack issue link --list-types`" + `
lists the link types with both phrases.

The phrases are the link types' own, the same in every UI language, unlike
the localized command language: ` + "`ytrack issue command A 'depends on B'`" + ` works
only where YouTrack's command language is English.`

type options struct {
	list      bool
	listTypes bool
	phrase    string
	exporter  *output.Exporter
}

// NewCmdLink returns the link command.
func NewCmdLink(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "link {<id> | <url>} {<target> | <url>}... --type <phrase>",
		Short: "Link issues, or list an issue's links or the link types",
		Long: `Link an issue to one or more target issues, read as
"<id> <phrase> <target>": ` + "`ytrack issue link APP-1 APP-2 --type 'depends on'`" + `
makes APP-1 depend on APP-2. Linking issues that are already linked that way
changes nothing. To remove a link, use "ytrack issue unlink".

--list prints the issue's links, one line per linked issue, and
--list-types the instance's link types.

` + TypeHelp + `

JSON fields of a link (--list and a link that was added):
` + LinkFieldsHelp + `

JSON fields of a link type (--list-types):
  id              link type ID
  name            link type name
  sourceToTarget  the source's phrase, e.g. "is required for"
  targetToSource  the target's phrase, e.g. "depends on"; empty when the
                  type is undirected
  directed        whether the type has a direction`,
		Example: `  $ ytrack issue link APP-1 APP-2 --type 'depends on'
  $ ytrack issue link APP-1 APP-2 APP-3 --type 'subtask of'
  $ ytrack issue link APP-1 APP-9 --type relates
  $ ytrack issue link APP-1 --list
  $ ytrack issue link APP-1 --list --json phrase,issues
  $ ytrack issue link --list-types`,
		ValidArgsFunction: shared.CompleteIssues(f, -1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkMode(opts, cmd, args); err != nil {
				return err
			}
			if opts.listTypes {
				return listTypes(cmd.Context(), f, opts)
			}
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			if opts.list {
				if err := shared.UseRefHost(f, ref, true); err != nil {
					return err
				}
				return f.WithIssueHost(cmd.Context(), ref.ID, func() error { return listLinks(cmd.Context(), f, opts, ref.ID) })
			}
			refs, err := Refs(args)
			if err != nil {
				return err
			}
			if err := shared.UseRefHostForWrite(f, HostRef(refs)); err != nil {
				return err
			}
			return f.WithIssueHostForWrite(cmd.Context(), refs[0].ID, func() error { return add(cmd.Context(), f, opts, refs) })
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&opts.phrase, "type", "", "Link `phrase`, e.g. 'depends on' (see --list-types)")
	fl.BoolVar(&opts.list, "list", false, "List the issue's links")
	fl.BoolVar(&opts.listTypes, "list-types", false, "List the link types")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, slices.Concat(LinkFields, TypeFields))
	return cmd
}

func checkMode(opts *options, cmd *cobra.Command, args []string) error {
	typeSet := cmd.Flags().Changed("type")
	switch {
	case opts.listTypes:
		if opts.list || typeSet || len(args) > 0 {
			return clierr.FlagErrorf("`--list-types` takes no issue, `--list` or `--type`")
		}
		return CheckJSON(opts.exporter, TypeFields, "--list-types")
	case opts.list:
		if typeSet || len(args) != 1 {
			return clierr.FlagErrorf("`--list` takes one issue ID or URL and no `--type`, e.g. ytrack issue link APP-123 --list")
		}
	default:
		if len(args) < 2 {
			return clierr.FlagErrorf("expected an issue ID or URL and one or more target issues, e.g. APP-1 APP-2 --type 'depends on'; or --list or --list-types")
		}
		if strings.TrimSpace(opts.phrase) == "" {
			return clierr.FlagErrorf("`--type` is required, e.g. --type 'depends on'; `ytrack issue link --list-types` lists the link types")
		}
	}
	return CheckJSON(opts.exporter, LinkFields, "a link")
}

// CheckJSON refuses --json fields that what is printed does not have.
func CheckJSON(e *output.Exporter, fields []string, what string) error {
	if e == nil {
		return nil
	}
	for _, name := range e.Fields {
		if !slices.Contains(fields, name) {
			return clierr.FlagErrorf("JSON field %q does not apply to %s; use one of: %s", name, what, strings.Join(fields, ", "))
		}
	}
	return nil
}

// Refs parses the issue and target arguments.
func Refs(args []string) ([]shared.Ref, error) {
	refs := make([]shared.Ref, len(args))
	for i, arg := range args {
		ref, err := shared.ParseRef(arg)
		if err != nil {
			return nil, err
		}
		refs[i] = ref
	}
	if err := sameHost(refs); err != nil {
		return nil, err
	}
	return refs, nil
}

func sameHost(refs []shared.Ref) error {
	host := HostRef(refs)
	for _, r := range refs {
		if r.ServiceURL != "" && !strings.EqualFold(strings.TrimRight(r.ServiceURL, "/"), strings.TrimRight(host.ServiceURL, "/")) {
			return clierr.FlagErrorf("issue URLs on different hosts (%s, %s); a link joins issues on one host", host.ServiceURL, r.ServiceURL)
		}
	}
	return nil
}

// HostRef returns the first ref given as a URL, which names the host.
func HostRef(refs []shared.Ref) shared.Ref {
	for _, r := range refs {
		if r.ServiceURL != "" {
			return r
		}
	}
	return shared.Ref{}
}

// ResolveType reads the instance's link types and returns the end --type
// names; an unknown phrase or a directed type's name is a usage error that
// lists the valid values.
func ResolveType(ctx context.Context, c *transport.Client, phrase string) (adapter.LinkEnd, error) {
	types, err := adapter.ListLinkTypes(ctx, c)
	if err != nil {
		return adapter.LinkEnd{}, err
	}
	if end, ok := adapter.FindLinkEnd(types, phrase); ok {
		return end, nil
	}
	var valid []string
	for _, t := range types {
		for _, e := range t.Ends() {
			if e.Phrase != "" {
				valid = append(valid, strconv.Quote(e.Phrase))
			}
		}
		if !t.Directed {
			valid = append(valid, strconv.Quote(t.Name))
		}
	}
	list := "\nValid --type values: " + strings.Join(valid, ", ")
	for _, t := range types {
		if t.Directed && strings.EqualFold(t.Name, strings.TrimSpace(phrase)) {
			return adapter.LinkEnd{}, clierr.FlagErrorf("%q is a directed link type; --type needs the phrase that says which way the link goes, %q or %q%s",
				phrase, t.TargetToSource, t.SourceToTarget, list)
		}
	}
	return adapter.LinkEnd{}, clierr.FlagErrorf("no single link type matches --type %q%s", phrase, list)
}

// Result is a link for --json output.
type Result struct {
	Issue string
	adapter.IssueLink
}

// ExportData implements output.Exportable.
func (r Result) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "issue":
			out[f] = r.Issue
		case "type":
			out[f] = r.Type.Name
		case "phrase":
			out[f] = r.Phrase
		case "direction":
			out[f] = r.Direction
		case "issues":
			out[f] = r.Issues
		}
	}
	return out
}

type linkType struct{ adapter.LinkType }

// ExportData implements output.Exportable.
func (t linkType) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "id":
			out[f] = t.ID
		case "name":
			out[f] = t.Name
		case "sourceToTarget":
			out[f] = t.SourceToTarget
		case "targetToSource":
			out[f] = t.TargetToSource
		case "directed":
			out[f] = t.Directed
		}
	}
	return out
}

func add(ctx context.Context, f *cmdutil.Factory, opts *options, refs []shared.Ref) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	end, err := ResolveType(ctx, client, opts.phrase)
	if err != nil {
		return err
	}
	issue := refs[0].ID
	res := Result{Issue: issue, IssueLink: adapter.IssueLink{LinkEnd: end, Issues: []string{}}}
	for _, t := range refs[1:] {
		if err := adapter.AddLink(ctx, client, issue, end.LinkID, t.ID); err != nil {
			return shared.NotFound(err, issue+" or "+t.ID, host)
		}
		res.Issues = append(res.Issues, t.ID)
	}
	ios := f.IOStreams
	if opts.exporter != nil {
		return opts.exporter.Write(ios, res)
	}
	fmt.Fprintf(ios.ErrOut, "%s %s %s %s\n", ios.ErrColorScheme().SuccessIcon(), issue, output.SanitizeCell(end.Phrase), strings.Join(res.Issues, ", "))
	return nil
}

func listLinks(ctx context.Context, f *cmdutil.Factory, opts *options, issue string) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	links, err := adapter.ListIssueLinks(ctx, client, issue)
	if err != nil {
		return shared.NotFound(err, issue, host)
	}
	out := []Result{}
	for _, l := range links {
		if len(l.Issues) > 0 {
			out = append(out, Result{Issue: issue, IssueLink: l})
		}
	}
	ios := f.IOStreams
	if opts.exporter != nil {
		return opts.exporter.Write(ios, out)
	}
	if len(out) == 0 {
		fmt.Fprintf(ios.ErrOut, "%s has no links\n", issue)
		return nil
	}
	tbl := output.NewTable(ios, "link", "issue")
	cs := ios.ColorScheme()
	for _, l := range out {
		for _, id := range l.Issues {
			tbl.StyledRow(output.Cell{Text: l.Phrase, Style: cs.Dim}, output.Cell{Text: id, Style: cs.Bold})
		}
	}
	return tbl.Render()
}

func listTypes(ctx context.Context, f *cmdutil.Factory, opts *options) error {
	client, _, err := f.APIClient()
	if err != nil {
		return err
	}
	types, err := adapter.ListLinkTypes(ctx, client)
	if err != nil {
		return err
	}
	ios := f.IOStreams
	if opts.exporter != nil {
		out := make([]linkType, len(types))
		for i, t := range types {
			out[i] = linkType{t}
		}
		return opts.exporter.Write(ios, out)
	}
	tbl := output.NewTable(ios, "name", "outward", "inward", "directed")
	cs := ios.ColorScheme()
	for _, t := range types {
		tbl.StyledRow(
			output.Cell{Text: t.Name, Style: cs.Bold},
			output.Cell{Text: t.SourceToTarget},
			output.Cell{Text: t.TargetToSource},
			output.Cell{Text: strconv.FormatBool(t.Directed), Style: cs.Dim},
		)
	}
	return tbl.Render()
}
