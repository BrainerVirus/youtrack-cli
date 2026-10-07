// Package output renders command results: JSON (optionally filtered by jq or
// a Go template), and tables.
//
// The exporter follows github.com/cli/cli pkg/cmdutil/json_flags.go
// (Copyright (c) 2019 GitHub Inc., MIT); see NOTICE.
package output

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"github.com/cli/go-gh/v2/pkg/jq"
	"github.com/cli/go-gh/v2/pkg/jsonpretty"
	"github.com/cli/go-gh/v2/pkg/template"

	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
)

// Format selects how JSON is written.
type Format struct {
	// JQ filters the JSON through a jq expression.
	JQ string
	// Template formats the JSON with a Go template (go-gh template functions).
	Template string
}

// WriteJSON writes the JSON document in r to ios.Out: filtered by JQ,
// formatted by Template, pretty and colored on a color terminal, or verbatim.
func WriteJSON(ios *iostreams.IOStreams, r io.Reader, f Format) error {
	w := ios.Out
	switch {
	case f.JQ != "":
		indent := ""
		if ios.IsStdoutTTY() {
			indent = "  "
		}
		return jq.EvaluateFormatted(r, w, f.JQ, indent, ios.ColorEnabled())
	case f.Template != "":
		t := template.New(w, ios.TerminalWidth(), ios.ColorEnabled())
		if err := t.Parse(f.Template); err != nil {
			return err
		}
		if err := t.Execute(r); err != nil {
			return err
		}
		return t.Flush()
	case ios.ColorEnabled():
		return jsonpretty.Format(w, r, "  ", true)
	default:
		return Copy(ios, r)
	}
}

// Copy writes r to ios.Out unchanged, adding a final newline on a terminal
// when the content lacks one so the shell prompt starts on its own line.
func Copy(ios *iostreams.IOStreams, r io.Reader) error {
	lw := &lastByteWriter{w: ios.Out}
	if _, err := io.Copy(lw, r); err != nil {
		return err
	}
	if ios.IsStdoutTTY() && lw.n > 0 && lw.last != '\n' {
		_, err := io.WriteString(ios.Out, "\n")
		return err
	}
	return nil
}

type lastByteWriter struct {
	w    io.Writer
	n    int64
	last byte
}

func (l *lastByteWriter) Write(p []byte) (int, error) {
	n, err := l.w.Write(p)
	if n > 0 {
		l.n += int64(n)
		l.last = p[n-1]
	}
	return n, err
}

// Exportable is implemented by results that can be projected onto a list of
// JSON field names.
type Exportable interface {
	ExportData(fields []string) map[string]any
}

// Exporter writes results as JSON restricted to the requested fields.
type Exporter struct {
	Fields []string
	Format
}

// Write projects data (an Exportable, or a slice or map of them) onto the
// exporter's fields and writes it.
func (e *Exporter) Write(ios *iostreams.IOStreams, data any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e.exportData(reflect.ValueOf(data))); err != nil {
		return err
	}
	return WriteJSON(ios, &buf, e.Format)
}

var exportableType = reflect.TypeFor[Exportable]()

func (e *Exporter) exportData(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		if v.Type().Implements(exportableType) {
			return v.Interface().(Exportable).ExportData(e.Fields)
		}
		return e.exportData(v.Elem())
	case reflect.Slice:
		a := make([]any, v.Len())
		for i := range v.Len() {
			a[i] = e.exportData(v.Index(i))
		}
		return a
	case reflect.Map:
		m := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			m[iter.Key().String()] = e.exportData(iter.Value())
		}
		return m
	case reflect.Struct:
		if v.Type().Implements(exportableType) {
			return v.Interface().(Exportable).ExportData(e.Fields)
		}
	}
	if !v.IsValid() {
		return nil
	}
	return v.Interface()
}
