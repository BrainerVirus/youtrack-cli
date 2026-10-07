// Package prompter asks the user for input on the terminal. Prompts go to
// stderr so stdout stays clean for data.
package prompter

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
)

// Prompter asks questions. End of input (Ctrl-D) returns clierr.ErrCancel.
type Prompter interface {
	Input(prompt, defaultValue string) (string, error)
	// Password reads a line without echoing it.
	Password(prompt string) (string, error)
	// Select returns the index of the chosen option.
	Select(prompt string, options []string, defaultIndex int) (int, error)
}

type linePrompter struct {
	in  io.Reader
	r   *bufio.Reader
	out io.Writer
}

// New returns a Prompter that reads from in and writes prompts to out. When in
// is a terminal, Password disables echo.
func New(in io.Reader, out io.Writer) Prompter {
	return &linePrompter{in: in, r: bufio.NewReader(in), out: out}
}

func (p *linePrompter) readLine() (string, error) {
	line, err := p.r.ReadString('\n')
	if errors.Is(err, io.EOF) && line == "" {
		return "", clierr.ErrCancel
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (p *linePrompter) Input(prompt, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(p.out, "? %s (%s) ", prompt, defaultValue)
	} else {
		fmt.Fprintf(p.out, "? %s ", prompt)
	}
	line, err := p.readLine()
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}

func (p *linePrompter) Password(prompt string) (string, error) {
	fmt.Fprintf(p.out, "? %s ", prompt)
	if f, ok := p.in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := p.readTerminalSecret(int(f.Fd()))
		fmt.Fprintln(p.out)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := p.readLine()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// readTerminalSecret reads without echo. Ctrl-C would normally kill the
// process with echo still off, so SIGINT is caught, the terminal state is
// restored and the prompt is cancelled.
func (p *linePrompter) readTerminalSecret(fd int) ([]byte, error) {
	state, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	return readHidden(
		func() ([]byte, error) { return term.ReadPassword(fd) },
		func() { _ = term.Restore(fd, state) },
		interrupt,
	)
}

// readHidden runs read until it returns or an interrupt arrives. On interrupt
// it calls restore and returns clierr.ErrCancel; end of input also cancels.
func readHidden(read func() ([]byte, error), restore func(), interrupt <-chan os.Signal) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		b, err := read()
		done <- result{b, err}
	}()
	select {
	case r := <-done:
		if errors.Is(r.err, io.EOF) {
			return nil, clierr.ErrCancel
		}
		return r.b, r.err
	case <-interrupt:
		restore()
		return nil, clierr.ErrCancel
	}
}

func (p *linePrompter) Select(prompt string, options []string, defaultIndex int) (int, error) {
	fmt.Fprintf(p.out, "? %s\n", prompt)
	for i, o := range options {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, o)
	}
	for {
		answer, err := p.Input("Choose a number:", strconv.Itoa(defaultIndex+1))
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(answer)
		if err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		fmt.Fprintf(p.out, "Enter a number between 1 and %d.\n", len(options))
	}
}
