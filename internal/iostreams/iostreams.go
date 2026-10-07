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

	stdinTTY     bool
	stdoutTTY    bool
	stderrTTY    bool
	colorEnabled bool
	termWidth    int
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
	ios.colorEnabled = t.IsColorEnabled()
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
