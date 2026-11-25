package shared

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/editor"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// BodyFlags are a comment's --body, --body-file and --editor.
type BodyFlags struct {
	Text   string
	File   string
	Editor bool
}

// Add registers the flags on cmd.
func (b *BodyFlags) Add(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVarP(&b.Text, "body", "b", "", "Comment `text`")
	fl.StringVarP(&b.File, "body-file", "F", "", "Read the comment from `file` (\"-\" for standard input)")
	fl.BoolVarP(&b.Editor, "editor", "e", false, "Write the comment in your editor")
}

// Check validates the flags after parsing. With none of them, it picks the
// editor when f can prompt and is a usage error otherwise.
func (b *BodyFlags) Check(f *cmdutil.Factory, cmd *cobra.Command) error {
	given := cmd.Flags().Changed("body")
	if err := cmdutil.MutuallyExclusive("specify only one of `--body`, `--body-file` or `--editor`", given, b.File != "", b.Editor); err != nil {
		return err
	}
	if !given && b.File == "" && !b.Editor {
		if !f.IOStreams.CanPrompt() {
			return clierr.FlagErrorf("`--body`, `--body-file` or `--editor` required when not running interactively")
		}
		b.Editor = true
	}
	return nil
}

// Read returns the comment text from the chosen source; the editor starts
// with initial, and its text loses trailing blank space.
func (b *BodyFlags) Read(f *cmdutil.Factory, initial string) (string, error) {
	ios := f.IOStreams
	switch {
	case b.File == "-":
		data, err := io.ReadAll(ios.In)
		if err != nil {
			return "", fmt.Errorf("reading standard input: %w", err)
		}
		return string(data), nil
	case b.File != "":
		data, err := os.ReadFile(b.File)
		if err != nil {
			return "", fmt.Errorf("reading the comment file: %w", err)
		}
		return string(data), nil
	case b.Editor:
		text, err := editor.Edit(editor.Command(), ".md", initial, ios.In, ios.ErrOut)
		return strings.TrimRight(text, " \t\r\n"), err
	}
	return b.Text, nil
}

// SaveDraft keeps text written in the editor when sending it failed, so the
// user does not lose it; what names the text in the message ("the comment
// was not posted").
func SaveDraft(f *cmdutil.Factory, what, text string) {
	d, err := os.CreateTemp("", "ytrack-comment-*.md")
	if err != nil {
		return
	}
	_, werr := d.WriteString(text)
	if cerr := d.Close(); werr != nil || cerr != nil {
		return
	}
	fmt.Fprintf(f.IOStreams.ErrOut, "! %s; your text is saved in %s\n", what, d.Name())
}
