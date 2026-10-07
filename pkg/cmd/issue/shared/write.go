package shared

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/editor"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/worktime"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter/customfields"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// UseRefHostForWrite is UseRefHost for commands that change data: an issue
// URL must be on a logged-in host (one in hosts.yml) unless --host or
// $YTRACK_HOST names it, whatever the token source. A pasted link never
// picks where a write goes.
func UseRefHostForWrite(f *cmdutil.Factory, ref Ref) error {
	if ref.ServiceURL == "" {
		return nil
	}
	if f.HostFlag == "" && os.Getenv("YTRACK_HOST") == "" {
		want, err := hosts.Parse(ref.ServiceURL)
		if err != nil {
			return clierr.FlagErrorWrap(err)
		}
		cfg, err := f.Config()
		if err != nil {
			return err
		}
		if cfg.Host(want.Key) == nil {
			return fmt.Errorf("refusing to change an issue on %s, which is not a logged-in host; run `ytrack auth login --host %s` or pass --host %s", want.Key, want.Key, want.Key)
		}
	}
	return UseRefHost(f, ref, true)
}

// FieldsFlagHelp documents --field for command help.
const FieldsFlagHelp = `--field "Name=Value" sets a custom field and can be repeated. The value is
checked against the project's field definitions: enum, state, version,
build, owned and group fields take an element name (case is ignored), user
fields a login or "me", date fields YYYY-MM-DD, date-time fields
2026-10-07T14:30 (local time) or RFC 3339, number fields a number, and text
and string fields any text. Multi-value fields take a comma-separated list,
which replaces their values; to add or remove one value, use
` + "`ytrack issue command <id> 'add <Field> <value>'` or `'remove <Field> <value>'`" + `.
"Name=" clears a field unless the project requires a value. Period fields such as
Estimation take a duration like "1w 2d 4h" and are set with a YouTrack
command, so the instance's work schedule applies; everything else is
written through the REST API. For anything else, use ` + "`ytrack issue command`."

// FieldAssign is one --field "Name=Value".
type FieldAssign struct{ Name, Value string }

// ParseFieldFlags parses --field values.
func ParseFieldFlags(raw []string) ([]FieldAssign, error) {
	out := make([]FieldAssign, 0, len(raw))
	for _, r := range raw {
		name, value, ok := strings.Cut(r, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, clierr.FlagErrorf("invalid --field %q: expected Name=Value", r)
		}
		for _, prev := range out {
			if strings.EqualFold(prev.Name, name) {
				return nil, clierr.FlagErrorf("field %q is given more than once; list multiple values as Name=a,b", name)
			}
		}
		out = append(out, FieldAssign{Name: name, Value: value})
	}
	return out, nil
}

// FieldWriter encodes custom field values for one project. It reads the
// project's field definitions and the current user at most once.
type FieldWriter struct {
	Client  *transport.Client
	Project adapter.ProjectInfo
	Zone    *time.Location

	defs []customfields.Definition
	me   string
}

// Encode turns assignments into IssueCustomField values for the REST API and
// commands for the fields written by command.
func (w *FieldWriter) Encode(ctx context.Context, assigns []FieldAssign) (rest []map[string]any, commands []string, err error) {
	if len(assigns) == 0 {
		return nil, nil, nil
	}
	if w.defs == nil {
		if w.defs, err = adapter.ProjectFields(ctx, w.Client, w.Project.ID); err != nil {
			return nil, nil, fmt.Errorf("reading the custom fields of project %s: %w", w.Project.ShortName, err)
		}
	}
	opts := customfields.EncodeOptions{Location: w.Zone, Me: func() (string, error) {
		if w.me == "" {
			u, err := adapter.CurrentUser(ctx, w.Client)
			if err != nil {
				return "", err
			}
			w.me = u.Login
		}
		return w.me, nil
	}}
	for _, a := range assigns {
		d, err := customfields.Find(w.defs, a.Name, w.Project.ShortName)
		if err != nil {
			return nil, nil, err
		}
		if d.NeedsUsers() && strings.TrimSpace(a.Value) != "me" && strings.TrimSpace(a.Value) != "" {
			users, err := adapter.ProjectFieldUsers(ctx, w.Client, w.Project.ID, d.ID)
			if err != nil {
				return nil, nil, fmt.Errorf("reading the users of field %q: %w", d.Name, err)
			}
			d.SetUsers(users)
			for i := range w.defs {
				if w.defs[i].ID == d.ID {
					w.defs[i] = d
				}
			}
		}
		enc, err := d.Encode(a.Value, opts)
		if err != nil {
			return nil, nil, err
		}
		if enc.Command != "" {
			commands = append(commands, enc.Command)
		} else {
			rest = append(rest, enc.Field)
		}
	}
	return rest, commands, nil
}

// FindProject finds a project that is not archived by key: an exact short
// name first, then a short name ignoring case, then a name ignoring case,
// which must match one project only.
func FindProject(ctx context.Context, c *transport.Client, key string) (adapter.ProjectInfo, error) {
	ps, err := adapter.ListProjects(ctx, c)
	if err != nil {
		return adapter.ProjectInfo{}, err
	}
	ps = slices.DeleteFunc(ps, func(p adapter.ProjectInfo) bool { return p.Archived })
	for _, match := range []func(adapter.ProjectInfo) bool{
		func(p adapter.ProjectInfo) bool { return p.ShortName == key },
		func(p adapter.ProjectInfo) bool { return strings.EqualFold(p.ShortName, key) },
		func(p adapter.ProjectInfo) bool { return strings.EqualFold(p.Name, key) },
	} {
		var found []string
		var first adapter.ProjectInfo
		for _, p := range ps {
			if match(p) {
				if len(found) == 0 {
					first = p
				}
				found = append(found, p.ShortName)
			}
		}
		switch {
		case len(found) == 1:
			return first, nil
		case len(found) > 1:
			return adapter.ProjectInfo{}, clierr.FlagErrorf("project %q is ambiguous; use one of the short names:\n  %s", key, strings.Join(found, "\n  "))
		}
	}
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.ShortName
	}
	return adapter.ProjectInfo{}, clierr.FlagErrorf("unknown project %q; projects you can use:\n  %s", key, strings.Join(names, "\n  "))
}

// ResolveTags finds the tags called names, exactly or else ignoring case.
func ResolveTags(ctx context.Context, c *transport.Client, names []string) ([]adapter.Tag, error) {
	if len(names) == 0 {
		return nil, nil
	}
	all, err := adapter.ListTags(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]adapter.Tag, 0, len(names))
	for _, n := range names {
		t, ok := findTag(all, n)
		if !ok {
			known := make([]string, len(all))
			for i, t := range all {
				known[i] = t.Name
			}
			return nil, fmt.Errorf("unknown tag %q (`ytrack issue command <id> 'tag %s'` creates it); tags you can use:\n  %s", n, n, strings.Join(known, "\n  "))
		}
		out = append(out, t)
	}
	return out, nil
}

func findTag(all []adapter.Tag, name string) (adapter.Tag, bool) {
	for _, t := range all {
		if t.Name == name {
			return t, true
		}
	}
	for _, t := range all {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return adapter.Tag{}, false
}

// DescriptionFlags are --description, --description-file and --editor.
type DescriptionFlags struct {
	Text   string
	File   string
	Editor bool
	set    bool
}

// Add registers the flags on cmd.
func (d *DescriptionFlags) Add(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVarP(&d.Text, "description", "d", "", "Issue description `text`")
	fl.StringVarP(&d.File, "description-file", "F", "", "Read the description from `file` (\"-\" for standard input)")
	fl.BoolVarP(&d.Editor, "editor", "e", false, "Write the description in your editor")
}

// Check validates the flags after parsing.
func (d *DescriptionFlags) Check(f *cmdutil.Factory, cmd *cobra.Command) error {
	d.set = cmd.Flags().Changed("description")
	if err := cmdutil.MutuallyExclusive("specify only one of `--description`, `--description-file` or `--editor`", d.set, d.File != "", d.Editor); err != nil {
		return err
	}
	if d.Editor && !f.IOStreams.CanPrompt() {
		return clierr.FlagErrorf("`--editor` needs a terminal; use `--description` or `--description-file` instead")
	}
	return nil
}

// Given reports whether any description source was given.
func (d *DescriptionFlags) Given() bool { return d.set || d.File != "" || d.Editor }

// Read returns the description from the chosen source; the editor starts
// with initial.
func (d *DescriptionFlags) Read(f *cmdutil.Factory, initial string) (string, error) {
	ios := f.IOStreams
	switch {
	case d.File == "-":
		b, err := io.ReadAll(ios.In)
		if err != nil {
			return "", fmt.Errorf("reading standard input: %w", err)
		}
		return string(b), nil
	case d.File != "":
		b, err := os.ReadFile(d.File)
		if err != nil {
			return "", fmt.Errorf("reading the description file: %w", err)
		}
		return string(b), nil
	case d.Editor:
		text, err := editor.Edit(editor.Command(), ".md", initial, ios.In, ios.ErrOut)
		return strings.TrimRight(text, " \t\r\n"), err
	}
	return d.Text, nil
}

// ParsedCommands exports YouTrack's parse of a command for --json.
func ParsedCommands(cs []adapter.ParsedCommand) []map[string]any {
	out := make([]map[string]any, len(cs))
	for i, c := range cs {
		out[i] = map[string]any{"description": c.Description, "error": c.Error}
	}
	return out
}

// Zone is the configured timezone (config.yml "timezone"), else the clock's.
func Zone(f *cmdutil.Factory) (*time.Location, error) {
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}
	return worktime.Location(cfg.Timezone(), f.Clock().Location())
}
