// Package iostreams bundles stdin/stdout/stderr with the terminal facts
// commands need: whether each stream is a TTY, whether color is on, and the
// terminal width. Modelled on github.com/cli/cli pkg/iostreams (MIT).
package iostreams

import (
	"bytes"
	"io"
	"os"

	"github.com/cli/go-gh/v2/pkg/term"
)

// IOStreams carries the process streams and their terminal capabilities.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	stdinTTY        bool
	stdoutTTY       bool
	stderrTTY       bool
	colorEnabled    bool // stdout
	errColorEnabled bool // stderr
	termWidth       int
}

// System returns streams bound to the process's stdin, stdout and stderr.
func System() *IOStreams {
	t := term.FromEnv()
	ios := &IOStreams{
		In:        os.Stdin,
		Out:       os.Stdout,
		ErrOut:    os.Stderr,
		stdinTTY:  term.IsTerminal(os.Stdin),
		stdoutTTY: t.IsTerminalOutput(),
		stderrTTY: term.IsTerminal(os.Stderr),
		termWidth: 80,
	}
	// Color only on a terminal, and never under NO_COLOR, CLICOLOR=0 or
	// TERM=dumb; CLICOLOR_FORCE forces it (go-gh's rules, plus TERM=dumb).
	dumb := os.Getenv("TERM") == "dumb"
	ios.colorEnabled = t.IsColorEnabled() && !dumb && colorAllowed()
	ios.errColorEnabled = !dumb && colorAllowed() && (ios.stderrTTY || forced())
	if w, _, err := t.Size(); err == nil && w > 0 {
		ios.termWidth = w
	}
	return ios
}

// Test returns streams backed by buffers, with every TTY flag off.
func Test() (ios *IOStreams, stdin, stdout, stderr *bytes.Buffer) {
	stdin, stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	return &IOStreams{In: stdin, Out: stdout, ErrOut: stderr, termWidth: 80}, stdin, stdout, stderr
}

func (s *IOStreams) IsStdinTTY() bool  { return s.stdinTTY }
func (s *IOStreams) IsStdoutTTY() bool { return s.stdoutTTY }
func (s *IOStreams) IsStderrTTY() bool { return s.stderrTTY }
func (s *IOStreams) ColorEnabled() bool {
	return s.colorEnabled
}
func (s *IOStreams) TerminalWidth() int { return s.termWidth }

func (s *IOStreams) SetStdinTTY(v bool)     { s.stdinTTY = v }
func (s *IOStreams) SetStdoutTTY(v bool)    { s.stdoutTTY = v }
func (s *IOStreams) SetStderrTTY(v bool)    { s.stderrTTY = v }
func (s *IOStreams) SetColorEnabled(v bool) { s.colorEnabled = v }

// ErrColorEnabled reports whether stderr gets color.
func (s *IOStreams) ErrColorEnabled() bool { return s.errColorEnabled }

// SetErrColorEnabled turns stderr color on or off.
func (s *IOStreams) SetErrColorEnabled(v bool) { s.errColorEnabled = v }

// DisableColor turns color off on every stream (--no-color, --json).
func (s *IOStreams) DisableColor() {
	s.colorEnabled = false
	s.errColorEnabled = false
}

// colorAllowed is false under NO_COLOR (any value, even with
// CLICOLOR_FORCE) and CLICOLOR=0 unless forced.
func colorAllowed() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return os.Getenv("CLICOLOR") != "0" || forced()
}

func forced() bool {
	v := os.Getenv("CLICOLOR_FORCE")
	return v != "" && v != "0"
}

// CanPrompt reports whether interactive prompts are allowed: both stdin and
// stdout must be terminals, so piped and scripted runs never block on input.
func (s *IOStreams) CanPrompt() bool {
	return s.stdinTTY && s.stdoutTTY
}

// Symbols returns check and cross marks, colored when color is on.
func (s *IOStreams) Symbols() (ok, fail string) {
	if s.colorEnabled {
		return "\x1b[32m✓\x1b[0m", "\x1b[31mX\x1b[0m"
	}
	return "✓", "X"
}
