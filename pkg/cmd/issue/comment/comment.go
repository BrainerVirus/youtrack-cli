// Package comment implements `ytrack issue comment`.
package comment

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	deletecmd "github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/comment/delete"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/comment/edit"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/comment/list"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields lists the --json fields of issue comment.
var Fields = []string{"id", "url", "issue", "attachments"}

// Where an --attach file ended up.
const (
	// onComment: attached to the comment.
	onComment = "comment"
	// onIssue: attached to the issue only; the comment shows it when its
	// text names it.
	onIssue = "issue"
	// failed: neither on the comment nor shown by it.
	failed = "failed"
)

type options struct {
	ref    shared.Ref
	body   shared.BodyFlags
	attach []string
	// text is the body once read, so a rerun does not read it again.
	text     string
	bodyRead bool
	posted   bool
	files    []adapter.LocalFile
	exporter *output.Exporter
}

type created struct {
	ID, URL, Issue string
	Files          []fileResult
}

// fileResult is where one --attach file ended up.
type fileResult struct {
	Name string
	// ID is the attachment's ID, "" for a file YouTrack did not keep.
	ID    string
	State string
	// shown: the comment text resolves a reference to it.
	shown bool
}

func (c created) ExportData(fields []string) map[string]any {
	files := make([]map[string]any, len(c.Files))
	for i, f := range c.Files {
		files[i] = map[string]any{"name": f.Name, "id": f.ID, "state": f.State}
	}
	all := map[string]any{"id": c.ID, "url": c.URL, "issue": c.Issue, "attachments": files}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = all[f]
	}
	return out
}

// NewCmdComment returns the comment command: on its own it adds a comment,
// like its add subcommand.
func NewCmdComment(f *cmdutil.Factory) *cobra.Command {
	cmd := newCmdAdd(f)
	cmd.Use = "comment {<id> | <url>}"
	cmd.Short = "Add, list, edit or delete issue comments"
	cmd.Long = `Add a comment to an issue, or with a subcommand list, edit or delete
comments. "ytrack issue comment <id>" is "ytrack issue comment add <id>".

` + addHelp
	add := newCmdAdd(f)
	add.Use = "add {<id> | <url>}"
	add.Example = strings.ReplaceAll(add.Example, "issue comment APP-123", "issue comment add APP-123")
	cmd.Example += `
  $ ytrack issue comment list APP-123
  $ ytrack issue comment edit APP-123 4-17 --body 'Reproduced on 2026.3'
  $ ytrack issue comment delete APP-123 4-17 --yes`
	cmd.AddCommand(add, list.NewCmdList(f), edit.NewCmdEdit(f), deletecmd.NewCmdDelete(f))
	return cmd
}

const addHelp = `Add a comment to an issue and print its URL.

The text comes from --body, from --body-file (a file, or "-" for standard
input) or from your editor with --editor. With none of them, the editor
opens when ytrack runs in a terminal. The editor is $YTRACK_EDITOR,
$GIT_EDITOR, $VISUAL or $EDITOR.

--attach adds a file to the comment (repeat it for more files): once the
comment is posted, the files are uploaded to it. The comment text shows an
attached file inline when it refers to it by name: ![](shot.png) for an
image, [notes](notes.html) for a link. A reference to the path you gave,
such as ![](shots/shot.png), is rewritten to the name. Files the text does
not mention appear as the comment's attachments. A missing or unreadable
file stops the command before anything is posted.

If YouTrack keeps a file on the issue rather than on the comment, the
command warns and exits 0: the comment still shows a file its text names.
It exits 1 naming each file that is neither on the comment nor shown by it.
After either, do not run the command again, since the comment and the
uploaded files already exist: check them with ytrack issue attachments
<id>, and add a missing file with ytrack issue attach <id> <file> (the
comment then shows it if its text names it).

JSON fields:
  id           comment ID
  url          comment web URL
  issue        readable ID of the issue
  attachments  the --attach files: name, id and state, where state is
               comment (on the comment), issue (on the issue only) or
               failed (neither on the comment nor shown by it)`

// newCmdAdd returns the command that adds a comment.
func newCmdAdd(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "add {<id> | <url>}",
		Short: "Add a comment to an issue",
		Long:  addHelp,
		Example: `  $ ytrack issue comment APP-123 --body 'Reproduced on 2026.2'
  $ git log -1 --format=%B | ytrack issue comment APP-123 --body-file -
  $ ytrack issue comment APP-123 --editor
  $ ytrack issue comment APP-123 --body 'Before and after: ![](shot.png)' --attach shot.png --attach notes.html`,
		Args:              cmdutil.ExactArgs(1, "expected one issue ID or URL, e.g. APP-123"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref = ref
			if err := opts.body.Check(f, cmd); err != nil {
				return err
			}
			if opts.files, err = shared.CheckFiles(opts.attach); err != nil {
				return err
			}
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			err = f.WithIssueHostForWrite(cmd.Context(), ref.ID, func() error { return run(cmd, f, opts) })
			if err != nil && opts.body.Editor && !opts.posted && strings.TrimSpace(opts.text) != "" {
				shared.SaveDraft(f, "the comment was not posted", opts.text)
			}
			return err
		},
	}
	opts.body.Add(cmd)
	cmd.Flags().StringArrayVar(&opts.attach, "attach", nil, "Attach a `file` to the comment (repeatable)")
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
	// A rerun on the host that owns the issue reuses the text read the first time.
	if !opts.bodyRead {
		if opts.text, err = opts.body.Read(f, ""); err != nil {
			return err
		}
		opts.bodyRead = true
	}
	body := opts.text
	if strings.TrimSpace(body) == "" {
		return errors.New("the comment is empty; nothing was posted")
	}

	body = shared.InlineRefs(body, opts.attach)
	c, err := adapter.AddComment(cmd.Context(), client, opts.ref.ID, body)
	if err != nil {
		return shared.NotFound(err, opts.ref.ID, host)
	}
	opts.posted = true
	issueID := c.IssueIDReadable
	if issueID == "" {
		issueID = opts.ref.ID
	}
	res := created{ID: c.ID, URL: shared.CommentURL(host, issueID, c.ID), Issue: issueID, Files: []fileResult{}}
	var cause error
	res.Files, cause, err = attach(cmd, client, issueID, c.ID, body, opts.files)
	if err != nil {
		return fmt.Errorf("the comment was posted (%s), but %w; %s", res.URL, err, noRerun(issueID))
	}
	if opts.exporter != nil {
		if err := opts.exporter.Write(ios, res); err != nil {
			return err
		}
	} else if !hasFailed(res.Files) {
		fmt.Fprintln(ios.Out, res.URL)
	}
	return report(f, issueID, res, cause)
}

// report warns about files that went to the issue rather than the comment
// and fails naming those that are neither on the comment nor shown by it,
// with cause, the upload error if any.
func report(f *cmdutil.Factory, issueID string, res created, cause error) error {
	var shown, listed, lost []string
	for _, r := range res.Files {
		switch {
		case r.State == failed:
			lost = append(lost, r.Name)
		case r.State == onIssue && r.shown:
			shown = append(shown, r.Name)
		case r.State == onIssue:
			listed = append(listed, r.Name)
		}
	}
	if len(shown) > 0 {
		f.Warnf("%s %s on the issue %s, not the comment; the comment shows %s", strings.Join(shown, ", "), isAre(shown), issueID, themIt(shown))
	}
	if len(listed) > 0 {
		f.Warnf("%s %s on the issue %s, not the comment; see ytrack issue attachments %s", strings.Join(listed, ", "), isAre(listed), issueID, issueID)
	}
	if len(lost) > 0 {
		why := ""
		if cause != nil {
			why = " (" + cause.Error() + ")"
		}
		return fmt.Errorf("the comment was posted (%s), but %d of %d files are neither on it nor shown by it: %s%s; %s; add them with ytrack issue attach %s <file>",
			res.URL, len(lost), len(res.Files), strings.Join(lost, ", "), why, noRerun(issueID), issueID)
	}
	if len(shown)+len(listed) > 0 {
		f.Warnf("%s", noRerun(issueID))
	}
	return nil
}

func noRerun(issueID string) string {
	return "do not run the command again (the comment and its uploads exist); check with ytrack issue attachments " + issueID
}

func hasFailed(files []fileResult) bool {
	return slices.ContainsFunc(files, func(r fileResult) bool { return r.State == failed })
}

func isAre(names []string) string {
	if len(names) == 1 {
		return "is"
	}
	return "are"
}

func themIt(names []string) string {
	if len(names) == 1 {
		return "it"
	}
	return "them"
}

// attach uploads files to the comment, then reads the comment back and
// reports where each file ended up. A file the comment did not get and
// YouTrack did not keep anywhere goes up to the issue, where YouTrack
// resolves the comment's references to it. text is the comment text as
// posted. cause is the upload error, if any; err is set only when it cannot
// tell where the files are.
func attach(cmd *cobra.Command, client *transport.Client, issueID, commentID, text string, files []adapter.LocalFile) (results []fileResult, cause, err error) {
	if len(files) == 0 {
		return []fileResult{}, nil, nil
	}
	ctx := cmd.Context()
	// Where an upload error leaves the files is read back below.
	uploaded, upErr := adapter.AttachToComment(ctx, client, issueID, commentID, files)
	seen, err := adapter.ReadCommentFiles(ctx, client, issueID, commentID)
	if err != nil {
		return nil, upErr, fmt.Errorf("checking its attachments failed: %w", err)
	}
	kept, onCmt := byName(uploaded), byName(seen.Attachments)
	var retry []adapter.LocalFile
	for _, f := range files {
		if onCmt[f.Name] == "" && kept[f.Name] == "" {
			retry = append(retry, f)
		}
	}
	var retryErr error
	if len(retry) > 0 {
		var more []adapter.Attachment
		more, retryErr = adapter.AttachToIssue(ctx, client, issueID, retry)
		maps.Copy(kept, byName(more))
		// The text resolves a reference only once the file exists.
		if seen, err = adapter.ReadCommentFiles(ctx, client, issueID, commentID); err != nil {
			return nil, errors.Join(upErr, retryErr), fmt.Errorf("checking its attachments failed: %w", err)
		}
		onCmt = byName(seen.Attachments)
	}
	out := make([]fileResult, len(files))
	for i, f := range files {
		r := fileResult{Name: f.Name, State: failed}
		switch {
		case onCmt[f.Name] != "":
			r.ID, r.State = onCmt[f.Name], onComment
		case kept[f.Name] != "":
			r.ID = kept[f.Name]
			r.shown = seen.Shows(r.ID)
			if r.shown || !refersTo(text, f.Name) {
				r.State = onIssue
			}
		}
		out[i] = r
	}
	return out, errors.Join(upErr, retryErr), nil
}

// refersTo reports whether the comment text links or embeds the file by
// name, as ![](name) or [label](name).
func refersTo(text, name string) bool { return strings.Contains(text, "]("+name+")") }

// byName maps attachment names to their IDs.
func byName(as []adapter.Attachment) map[string]string {
	m := make(map[string]string, len(as))
	for _, a := range as {
		m[a.Name] = a.ID
	}
	return m
}
