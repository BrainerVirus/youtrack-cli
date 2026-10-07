package output

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SanitizeText makes server-supplied text safe to print on a terminal: it
// drops C0 and C1 control characters (ESC, CSI, OSC and the rest, also as raw
// 8-bit bytes) and DEL, keeping newlines and tabs. \r\n becomes \n. Like
// go-gh's asciisanitizer it guards against escape-sequence injection, but it
// removes the characters instead of printing caret notation.
func SanitizeText(s string) string {
	return sanitize(strings.ReplaceAll(s, "\r\n", "\n"), false)
}

// SanitizeCell is SanitizeText for a single-line table cell: tabs and line
// breaks become spaces.
func SanitizeCell(s string) string {
	return sanitize(s, true)
}

func sanitize(s string, cell bool) string {
	clean := true
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == utf8.RuneError && size == 1:
			// An invalid byte; 0x80-0x9F would be an 8-bit C1 control.
			continue
		case r == '\n' || r == '\t' || r == '\r':
			if cell {
				b.WriteByte(' ')
			} else if r != '\r' {
				b.WriteRune(r)
			}
		case unicode.IsControl(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
