// Package shared holds the --json field contract the tag commands share.
package shared

import "github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"

// Fields are the --json fields of a tag.
var Fields = []string{"id", "name", "owner"}

// FieldsHelp documents Fields for command help.
const FieldsHelp = `  id     tag database ID, e.g. 6-12
  name   tag name, as --tag, --add-tag and --remove-tag take it
  owner  login of the user who owns the tag, or null`

// Tag exports an adapter.Tag under the --json field names.
type Tag struct{ adapter.Tag }

// ExportData implements output.Exportable.
func (t Tag) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "id":
			out[f] = t.ID
		case "name":
			out[f] = t.Name
		case "owner":
			if t.Owner == "" {
				out[f] = nil
			} else {
				out[f] = t.Owner
			}
		}
	}
	return out
}
