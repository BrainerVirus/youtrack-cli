package output

import (
	"strings"

	"github.com/cli/go-gh/v2/pkg/tableprinter"

	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
)

// Table prints aligned columns on a terminal and tab-separated values when
// piped. Headers are shown only on a terminal, so piped output is pure data.
type Table struct {
	tableprinter.TablePrinter
}

// NewTable returns a Table writing to ios.Out with the given column headers.
func NewTable(ios *iostreams.IOStreams, headers ...string) *Table {
	tp := tableprinter.New(ios.Out, ios.IsStdoutTTY(), ios.TerminalWidth())
	if ios.IsStdoutTTY() && len(headers) > 0 {
		upper := make([]string, len(headers))
		for i, h := range headers {
			upper[i] = strings.ToUpper(h)
		}
		tp.AddHeader(upper)
	}
	return &Table{tp}
}

// Row adds one row.
func (t *Table) Row(fields ...string) {
	for _, f := range fields {
		t.AddField(f)
	}
	t.EndRow()
}
