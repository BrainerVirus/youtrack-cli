package shared

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// CommentFields are the --json fields of a comment.
var CommentFields = []string{"id", "text", "author", "created", "updated", "url", "attachments"}

// CommentFieldsHelp documents CommentFields for command help.
const CommentFieldsHelp = `  id           comment ID, e.g. 4-17
  text         comment source text
  author       {login, fullName}
  created      creation time (RFC 3339, UTC)
  updated      last edit time, or null when never edited
  url          comment web URL
  attachments  [{name, id, size, mimeType}] on the comment`

// Comment exports an adapter.Comment under the --json field names.
type Comment struct {
	adapter.Comment
	Host hosts.Host
	// IssueID is the readable ID of the comment's issue, for its URL.
	IssueID string
	Files   []adapter.Attachment
}

func (c Comment) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "id":
			out[f] = c.ID
		case "text":
			out[f] = c.Text
		case "author":
			out[f] = exportUser(c.Author)
		case "created":
			out[f] = exportTime(c.Created)
		case "updated":
			out[f] = exportTime(c.Updated)
		case "url":
			out[f] = CommentURL(c.Host, c.IssueID, c.ID)
		case "attachments":
			out[f] = exportAttachments(c.Files)
		}
	}
	return out
}

var commentIDPattern = regexp.MustCompile(`^[0-9]+-[0-9]+$`)

// ParseCommentID checks a comment ID such as 4-17.
func ParseCommentID(arg string) (string, error) {
	if !commentIDPattern.MatchString(arg) {
		return "", clierr.FlagErrorf("invalid comment ID %q: expected an ID such as 4-17 (see ytrack issue comment list)", arg)
	}
	return arg, nil
}

// CommentNotFound turns a 404 on a comment into a clear message.
func CommentNotFound(err error, issueID, commentID string, host hosts.Host) error {
	if apiErr, ok := errors.AsType[*transport.APIError](err); ok && apiErr.StatusCode == http.StatusNotFound {
		return NotFoundf(apiErr, "comment %s not found on issue %s on %s (or you cannot see it)", commentID, issueID, host.Key)
	}
	return err
}

// CommentDeleted is the error for a comment deleted in the YouTrack UI,
// which can still be read but no longer edited.
func CommentDeleted(issueID, commentID string) error {
	return fmt.Errorf("comment %s on %s is deleted", commentID, issueID)
}

// Excerpt is text on one line, safe for a terminal, cut to 60 characters,
// for a confirmation prompt.
func Excerpt(text string) string {
	s := []rune(strings.TrimSpace(output.SanitizeCell(text)))
	if len(s) <= 60 {
		return string(s)
	}
	return strings.TrimSpace(string(s[:59])) + "…"
}
