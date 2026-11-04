package iostreams

import (
	"regexp"
	"strings"
)

// ANSI SGR sequences used by ColorScheme.
const (
	sgrReset  = "\x1b[0m"
	sgrBold   = "\x1b[1m"
	sgrDim    = "\x1b[2m"
	sgrRed    = "\x1b[31m"
	sgrGreen  = "\x1b[32m"
	sgrYellow = "\x1b[33m"
	sgrCyan   = "\x1b[36m"
)

// ColorScheme styles text for one stream. When color is off every method
// returns its input unchanged, so plain output stays byte-identical.
type ColorScheme struct {
	enabled bool
}

// ColorScheme returns the scheme for stdout.
func (s *IOStreams) ColorScheme() *ColorScheme { return &ColorScheme{enabled: s.colorEnabled} }

// ErrColorScheme returns the scheme for stderr.
func (s *IOStreams) ErrColorScheme() *ColorScheme { return &ColorScheme{enabled: s.errColorEnabled} }

// Enabled reports whether the scheme adds escapes.
func (c *ColorScheme) Enabled() bool { return c.enabled }

func (c *ColorScheme) wrap(sgr, s string) string {
	if !c.enabled || s == "" {
		return s
	}
	return sgr + s + sgrReset
}

// Bold is for headings, summaries and the command to run next.
func (c *ColorScheme) Bold(s string) string { return c.wrap(sgrBold, s) }

// Dim is for labels and secondary detail.
func (c *ColorScheme) Dim(s string) string { return c.wrap(sgrDim, s) }

// Accent is for identifiers: issue IDs, command names and flags.
func (c *ColorScheme) Accent(s string) string { return c.wrap(sgrCyan, s) }

// Green marks success and resolved states.
func (c *ColorScheme) Green(s string) string { return c.wrap(sgrGreen, s) }

// Yellow marks open and in-progress states.
func (c *ColorScheme) Yellow(s string) string { return c.wrap(sgrYellow, s) }

// Red marks errors and critical priorities.
func (c *ColorScheme) Red(s string) string { return c.wrap(sgrRed, s) }

// SuccessIcon is a check mark, green when color is on.
func (c *ColorScheme) SuccessIcon() string { return c.Green("✓") }

var (
	resolvedStates = []string{
		"fixed", "done", "verified", "resolved", "closed", "completed", "complete",
		"won't fix", "wont fix", "duplicate", "obsolete", "can't reproduce", "cannot reproduce",
		"incomplete", "answered", "shipped", "released", "merged", "approved",
	}
	openStates = []string{
		"open", "submitted", "in progress", "reopened", "to do", "todo", "new", "backlog",
		"in review", "review", "to verify", "waiting", "on hold", "pending", "investigating",
		"to be discussed", "in development", "development", "testing", "in testing",
	}
	criticalPriorities = []string{"critical", "show-stopper", "showstopper", "blocker", "urgent", "highest"}
	majorPriorities    = []string{"major", "high"}
)

func matches(name string, set []string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, v := range set {
		if n == v {
			return true
		}
	}
	return false
}

// State colors a State value by meaning: resolved states green, open and
// in-progress states yellow. resolved is the issue's own resolved flag, when
// known; unknown names are left plain.
func (c *ColorScheme) State(name string, resolved bool) string {
	switch {
	case name == "":
		return name
	case resolved || matches(name, resolvedStates):
		return c.Green(name)
	case matches(name, openStates):
		return c.Yellow(name)
	}
	return name
}

// Priority colors a Priority value: critical red, major yellow.
func (c *ColorScheme) Priority(name string) string {
	switch {
	case matches(name, criticalPriorities):
		return c.Red(name)
	case matches(name, majorPriorities):
		return c.Yellow(name)
	}
	return name
}

var codeSpan = regexp.MustCompile("`[^`]+`")

// Error renders an error line in red with every `command` in it bold, so the
// command that unblocks the user stands out.
func (c *ColorScheme) Error(line string) string {
	if !c.enabled {
		return line
	}
	return sgrRed + codeSpan.ReplaceAllStringFunc(line, func(m string) string {
		return sgrBold + m + sgrReset + sgrRed
	}) + sgrReset
}
