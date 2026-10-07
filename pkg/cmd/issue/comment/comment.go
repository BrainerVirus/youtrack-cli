// Package comment implements `ytrack issue comment`.
package comment

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/editor"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields lists the --json fields of issue comment.
var Fields = []string{"id", "url", "issue"}

type options struct {
	ref      shared.Ref
	body     string
	bodyFile string
	editor   bool
	exporter *output.Exporter
}

type created struct{ ID, URL, Issue string }

func (c created) ExportData(fields []string) map[string]any {
	all := map[string]any{"id": c.ID, "url": c.URL, "issue": c.Issue}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = all[f]
	}
	return out
}

// NewCmdComment returns the comment command.
func NewCmdComment(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "comment {<id> | <url>}",
		Short: "Add a comment to an issue",
		Long: `Add a comment to an issue and print its URL.

The text comes from --body, from --body-file (a file, or "-" for standard
input) or from your editor with --editor. With none of them, the editor
opens when ytrack runs in a terminal. The editor is $YTRACK_EDITOR,
$GIT_EDITOR, $VISUAL or $EDITOR.

JSON fields:
  id     comment ID
  url    comment web URL
  issue  readable ID of the issue`,
		Example: `  $ ytrack issue comment APP-123 --body 'Reproduced on 2026.2'
  $ git log -1 --format=%B | ytrack issue comment APP-123 --body-file -
  $ ytrack issue comment APP-123 --editor`,
		Args: cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			bodyGiven := cmd.Flags().Changed("body")
			if err := cmdutil.MutuallyExclusive("specify only one of `--body`, `--body-file` or `--editor`", bodyGiven, opts.bodyFile != "", opts.editor); err != nil {
				return err
			}
			if !bodyGiven && opts.bodyFile == "" && !opts.editor {
				if !f.IOStreams.CanPrompt() {
					return clierr.FlagErrorf("`--body`, `--body-file` or `--editor` required when not running interactively")
				}
				opts.editor = true
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			return run(cmd, f, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.body, "body", "b", "", "Comment `text`")
	fl.StringVarP(&opts.bodyFile, "body-file", "F", "", "Read the comment from `file` (\"-\" for standard input)")
	fl.BoolVarP(&opts.editor, "editor", "e", false, "Write the comment in your editor")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, Fields)
	return cmd
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	ios := f.IOStreams
	// Fail on credentials before the user spends time in the editor.
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	body, err := readBody(f, opts)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("the comment is empty; nothing was posted")
	}

	c, err := adapter.AddComment(cmd.Context(), client, opts.ref.ID, body)
	if err != nil {
		if opts.editor {
			saveDraft(f, body)
		}
		return shared.NotFound(err, opts.ref.ID, host)
	}
	issueID := c.IssueIDReadable
	if issueID == "" {
		issueID = opts.ref.ID
	}
	res := created{ID: c.ID, URL: shared.CommentURL(host, issueID, c.ID), Issue: issueID}
	if opts.exporter != nil {
		return opts.exporter.Write(ios, res)
	}
	fmt.Fprintln(ios.Out, res.URL)
	return nil
}

// saveDraft keeps text written in the editor when posting it failed, so the
// user does not lose it.
func saveDraft(f *cmdutil.Factory, body string) {
	d, err := os.CreateTemp("", "ytrack-comment-*.md")
	if err != nil {
		return
	}
	_, werr := d.WriteString(body)
	if cerr := d.Close(); werr != nil || cerr != nil {
		return
	}
	fmt.Fprintf(f.IOStreams.ErrOut, "! the comment was not posted; your text is saved in %s\n", d.Name())
}

func readBody(f *cmdutil.Factory, opts *options) (string, error) {
	ios := f.IOStreams
	switch {
	case opts.bodyFile == "-":
		b, err := io.ReadAll(ios.In)
		if err != nil {
			return "", fmt.Errorf("reading standard input: %w", err)
		}
		return string(b), nil
	case opts.bodyFile != "":
		b, err := os.ReadFile(opts.bodyFile)
		if err != nil {
			return "", fmt.Errorf("reading the comment file: %w", err)
		}
		return string(b), nil
	case opts.editor:
		text, err := editor.Edit(editor.Command(), ".md", "", ios.In, ios.ErrOut)
		return strings.TrimRight(text, " \t\r\n"), err
	}
	return opts.body, nil
}
