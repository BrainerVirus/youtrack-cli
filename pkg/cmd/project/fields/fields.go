// Package fields implements `ytrack project fields`.
package fields

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter/customfields"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields are the --json fields of a project custom field.
var Fields = []string{"id", "name", "type", "bundle", "required", "default"}

const fieldsHelp = `  id        project custom field database ID, e.g. 93-10
  name      field name, as --field Name=Value takes it
  type      YouTrack field type, e.g. enum[1], user[1], version[*], period,
            date, date and time, integer, string, text ([*] takes several values)
  bundle    the values the field accepts (groups for a group field); empty
            for user fields and fields without a value set
  required  whether the field must have a value
  default   the value names (user logins) a new issue gets, or null when
            unknown`

// NewCmdFields returns the project fields command.
func NewCmdFields(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:   "fields <project>",
		Short: "List the custom fields of a project",
		Long: `List the custom fields of a project: name, type, whether it is required,
its default and the values it accepts. Those names and values are what
"ytrack issue create --field" and "ytrack issue edit --field" take.

<project> is a short name (APP) or a project name. Reading the field
settings needs permission to read the project's settings; without it the
fields come from an issue of the project, and defaults are unknown.

JSON fields:
` + fieldsHelp,
		Example: `  $ ytrack project fields APP
  $ ytrack project fields APP --json name,type,bundle --jq '.[] | select(.name == "Priority") | .bundle'`,
		Args: cmdutil.ExactArgs(1, "expected one project short name, e.g. APP"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := f.APIClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			project, err := shared.FindProject(ctx, client, args[0])
			if err != nil {
				return err
			}
			defs, settings, err := definitions(cmd, client, project)
			if err != nil {
				return err
			}
			if !settings {
				f.Warnf("your account cannot read the settings of project %s; fields come from one of its issues, without defaults", project.ShortName)
			}
			out := make([]field, len(defs))
			for i, d := range defs {
				out[i] = field{Definition: d, defaultsKnown: settings}
			}
			return render(f, exporter, project.ShortName, out)
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, Fields)
	return cmd
}

// definitions reads the project's field settings, or else, when the user
// cannot read them (an empty list or a 403), the definitions an issue of
// the project shows; settings reports which.
func definitions(cmd *cobra.Command, client *transport.Client, p adapter.ProjectInfo) (defs []customfields.Definition, settings bool, err error) {
	ctx := cmd.Context()
	defs, err = adapter.ProjectFieldSettings(ctx, client, p.ID)
	if err != nil && !forbidden(err) {
		return nil, false, fmt.Errorf("reading the custom fields of project %s: %w", p.ShortName, err)
	}
	if len(defs) > 0 {
		return defs, true, nil
	}
	defs, err = adapter.ProjectIssueFieldDefinitions(ctx, client, p.ShortName)
	if err != nil && !forbidden(err) {
		return nil, false, fmt.Errorf("reading the custom fields of project %s: %w", p.ShortName, err)
	}
	if len(defs) == 0 {
		return nil, false, fmt.Errorf("cannot read the custom fields of project %s: your account cannot read the project settings and no issue of it shows them", p.ShortName)
	}
	return defs, false, nil
}

func forbidden(err error) bool {
	apiErr, ok := errors.AsType[*transport.APIError](err)
	return ok && apiErr.StatusCode == http.StatusForbidden
}

// field exports a definition under the --json field names.
type field struct {
	customfields.Definition
	defaultsKnown bool
}

// bundle is what the field accepts: its values, or a group field's groups.
func (d field) bundle() []string {
	if d.Kind == customfields.KindGroup {
		return nonNil(d.Groups)
	}
	return nonNil(d.Values)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ExportData implements output.Exportable.
func (d field) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "id":
			out[f] = d.ID
		case "name":
			out[f] = d.Name
		case "type":
			out[f] = d.FieldType
		case "bundle":
			out[f] = d.bundle()
		case "required":
			out[f] = !d.CanBeEmpty
		case "default":
			if d.defaultsKnown {
				out[f] = nonNil(d.Defaults)
			} else {
				out[f] = nil
			}
		}
	}
	return out
}

// maxShown caps the bundle values a table row lists.
const maxShown = 8

func render(f *cmdutil.Factory, exporter *output.Exporter, project string, fs []field) error {
	ios := f.IOStreams
	if exporter != nil {
		return exporter.Write(ios, fs)
	}
	if len(fs) == 0 {
		fmt.Fprintf(ios.ErrOut, "project %s has no custom fields\n", project)
		return nil
	}
	tbl := output.NewTable(ios, "name", "type", "required", "default", "values")
	cs := ios.ColorScheme()
	for _, d := range fs {
		required := ""
		if !d.CanBeEmpty {
			required = "yes"
		}
		values := d.bundle()
		shown := strings.Join(values[:min(len(values), maxShown)], ", ")
		if len(values) > maxShown {
			shown += fmt.Sprintf(", ... (%d more)", len(values)-maxShown)
		}
		tbl.StyledRow(
			output.Cell{Text: d.Name, Style: cs.Bold},
			output.Cell{Text: d.FieldType, Style: cs.Dim},
			output.Cell{Text: required},
			output.Cell{Text: strings.Join(d.Defaults, ", ")},
			output.Cell{Text: shown},
		)
	}
	return tbl.Render()
}
