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
		tp.AddHeader(upper, tableprinter.WithColor(ios.ColorScheme().Bold))
	}
	return &Table{tp}
}

// Row adds one row. Fields are sanitized with SanitizeCell, so server data
// cannot inject terminal escapes or break the tab-separated layout.
func (t *Table) Row(fields ...string) {
	for _, f := range fields {
		t.AddField(SanitizeCell(f))
	}
	t.EndRow()
}

// Cell is a table field with an optional style, applied after the field is
// sanitized and fitted to its column (so escapes never count toward widths).
type Cell struct {
	Text  string
	Style func(string) string
}

// StyledRow adds one row of cells, each sanitized like Row.
func (t *Table) StyledRow(cells ...Cell) {
	for _, c := range cells {
		if c.Style != nil {
			t.AddField(SanitizeCell(c.Text), tableprinter.WithColor(c.Style))
		} else {
			t.AddField(SanitizeCell(c.Text))
		}
	}
	t.EndRow()
}
