// Package editor opens text in the user's editor, as git and gh do.
package editor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/google/shlex"
)

// Command returns the editor command line: $YTRACK_EDITOR, $GIT_EDITOR,
// $VISUAL or $EDITOR, else a platform default.
func Command() string {
	for _, k := range []string{"YTRACK_EDITOR", "GIT_EDITOR", "VISUAL", "EDITOR"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// split turns the editor command into arguments. A command that names an
// existing file is taken whole, so Windows paths and paths with spaces work
// without quoting; anything else is split like a shell would.
func split(command string) ([]string, error) {
	if st, err := os.Stat(command); err == nil && !st.IsDir() {
		return []string{command}, nil
	}
	args, err := shlex.Split(command)
	if err != nil || len(args) == 0 {
		return nil, fmt.Errorf("invalid editor command %q", command)
	}
	return args, nil
}

// Edit writes initial to a temporary file whose name ends in suffix, runs
// command (split like a shell would, with the file appended) and returns the
// file's content after the editor exits. The editor reads in and writes to
// out, which should be the terminal.
func Edit(command, suffix, initial string, in io.Reader, out io.Writer) (string, error) {
	args, err := split(command)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "ytrack-*"+suffix)
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := f.WriteString(initial); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	cmd := exec.Command(args[0], append(args[1:], path)...) //nolint:gosec // running the user's chosen editor is the feature
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, out
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", fmt.Errorf("editor %q exited with status %d", args[0], exitErr.ExitCode())
		}
		return "", fmt.Errorf("running editor %q: %w", args[0], err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
