package output

import (
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
)

func TestSanitize(t *testing.T) {
	tests := []struct{ name, in, text, cell string }{
		{"plain text", "Login fails — ünïcode", "Login fails — ünïcode", "Login fails — ünïcode"},
		{"a CSI color sequence", "\x1b[31mred\x1b[0m", "[31mred[0m", "[31mred[0m"},
		{"an OSC 8 hyperlink and an OSC title", "\x1b]8;;https://evil\x07click\x1b]8;;\x07 \x1b]0;pwned\x1b\\", "]8;;https://evilclick]8;; ]0;pwned\\", "]8;;https://evilclick]8;; ]0;pwned\\"},
		{"a C1 CSI rune and raw 8-bit C1 bytes", "a\u009b31mb\x9bc\x90d", "a31mbcd", "a31mbcd"},
		{"tabs, CRLF and newlines", "a\tb\r\nc\nd\re", "a\tb\nc\nde", "a b  c d e"},
		{"backspace, bell and DEL", "x\by\az\x7f", "xyz", "xyz"},
	}
	for _, tt := range tests {
		t.Run("given "+tt.name+", it removes control characters", func(t *testing.T) {
			if got := SanitizeText(tt.in); got != tt.text {
				t.Errorf("SanitizeText = %q, want %q", got, tt.text)
			}
			if got := SanitizeCell(tt.in); got != tt.cell {
				t.Errorf("SanitizeCell = %q, want %q", got, tt.cell)
			}
		})
	}

	t.Run("table rows are sanitized", func(t *testing.T) {
		ios, _, out, _ := iostreams.Test()
		tbl := NewTable(ios, "id", "summary")
		tbl.Row("APP-1", "evil\x1b]0;title\x07\tsummary\nline")
		if err := tbl.Render(); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); got != "APP-1\tevil]0;title summary line\n" || strings.ContainsRune(got, 0x1b) {
			t.Errorf("got %q", got)
		}
	})
}
