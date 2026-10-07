package output

import (
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
)

type issue struct{ ID, Summary, Secret string }

func (i issue) ExportData(fields []string) map[string]any {
	all := map[string]any{"id": i.ID, "summary": i.Summary, "secret": i.Secret}
	out := map[string]any{}
	for _, f := range fields {
		out[f] = all[f]
	}
	return out
}

func TestExporter(t *testing.T) {
	data := []issue{{"APP-1", "One", "x"}, {"APP-2", "Two", "y"}}

	t.Run("it emits only the requested fields", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		e := &Exporter{Fields: []string{"id"}}
		if err := e.Write(ios, data); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); got != `[{"id":"APP-1"},{"id":"APP-2"}]`+"\n" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("it applies a jq filter to the projected data", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		e := &Exporter{Fields: []string{"id", "summary"}, Format: Format{JQ: `.[] | "\(.id) \(.summary)"`}}
		if err := e.Write(ios, data); err != nil {
			t.Fatal(err)
		}
		if out.String() != "APP-1 One\nAPP-2 Two\n" {
			t.Errorf("got %q", out)
		}
	})

	t.Run("it renders a Go template over the projected data", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		e := &Exporter{Fields: []string{"id"}, Format: Format{Template: `{{range .}}{{.id}};{{.secret}}|{{end}}`}}
		if err := e.Write(ios, data); err != nil {
			t.Fatal(err)
		}
		if out.String() != "APP-1;<no value>|APP-2;<no value>|" {
			t.Errorf("got %q", out)
		}
	})
}

func TestWriteJSON(t *testing.T) {
	t.Run("on a color terminal it pretty-prints with color", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		ios.SetStdoutTTY(true)
		ios.SetColorEnabled(true)
		if err := WriteJSON(ios, strings.NewReader(`{"a":1}`), Format{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\x1b[") || !strings.Contains(out.String(), "\n  ") {
			t.Errorf("got %q", out)
		}
	})

	t.Run("when piped it copies the bytes unchanged", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		if err := WriteJSON(ios, strings.NewReader(`{"a":1}`), Format{}); err != nil {
			t.Fatal(err)
		}
		if out.String() != `{"a":1}` {
			t.Errorf("got %q", out)
		}
	})
}

func TestTable(t *testing.T) {
	t.Run("when piped it prints tab-separated rows without a header", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		tbl := NewTable(ios, "id", "summary")
		tbl.Row("APP-1", "One")
		if err := tbl.Render(); err != nil {
			t.Fatal(err)
		}
		if out.String() != "APP-1\tOne\n" {
			t.Errorf("got %q", out)
		}
	})

	t.Run("on a terminal it prints an upper-case header and aligned columns", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		ios.SetStdoutTTY(true)
		tbl := NewTable(ios, "id", "summary")
		tbl.Row("APP-10", "One")
		if err := tbl.Render(); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "ID") || strings.Index(lines[0], "SUMMARY") != strings.Index(lines[1], "One") {
			t.Errorf("got %q", out)
		}
	})
}

func TestCopy(t *testing.T) {
	t.Run("on a terminal it ends the output with a newline", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		ios.SetStdoutTTY(true)
		_ = Copy(ios, strings.NewReader(`{"a":1}`))
		if out.String() != `{"a":1}`+"\n" {
			t.Errorf("got %q", out)
		}
	})
	t.Run("when piped it adds nothing", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		_ = Copy(ios, strings.NewReader(`{"a":1}`))
		if out.String() != `{"a":1}` {
			t.Errorf("got %q", out)
		}
	})
	t.Run("on a terminal an empty body stays empty", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		ios.SetStdoutTTY(true)
		_ = Copy(ios, strings.NewReader(""))
		if out.Len() != 0 {
			t.Errorf("got %q", out)
		}
	})
}
