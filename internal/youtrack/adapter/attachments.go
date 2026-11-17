package adapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// Attachment is a file attached to an issue or a comment.
type Attachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
}

// LocalFile is a file on disk to upload as an issue or comment attachment.
// The upload streams a regular file from Path rather than holding it in
// memory.
type LocalFile struct {
	// Name is the attachment name; a comment's text shows it inline with
	// ![](Name).
	Name     string
	MimeType string
	Path     string
	// Size is the length the file had when it was checked; the upload fails
	// if the file changes size before it is sent.
	Size int64
	// Pipe marks a file that can be read only once, such as a FIFO: Data
	// holds its content, which a redirect resends.
	Pipe bool
	Data []byte
}

// AttachToIssue uploads files to the issue in one request and returns the
// attachments YouTrack created.
func AttachToIssue(ctx context.Context, c *transport.Client, issueID string, files []LocalFile) ([]Attachment, error) {
	return upload(ctx, c, "/api/issues/"+url.PathEscape(issueID)+"/attachments?fields=id,name,size,mimeType", files)
}

// AttachToComment uploads files to an existing comment in one request, the
// way the web UI does. YouTrack ignores files sent inside the JSON that
// creates a comment, so a comment gets its files only through this upload.
func AttachToComment(ctx context.Context, c *transport.Client, issueID, commentID string, files []LocalFile) error {
	_, err := upload(ctx, c, "/api/issues/"+url.PathEscape(issueID)+"/comments/"+url.PathEscape(commentID)+"/attachments?fields=id,name", files)
	return err
}

// CommentAttachments reads the attachments a comment has.
func CommentAttachments(ctx context.Context, c *transport.Client, issueID, commentID string) ([]Attachment, error) {
	var w struct {
		Attachments []Attachment `json:"attachments"`
	}
	err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments/"+url.PathEscape(commentID)+"?fields=attachments(id,name)", &w)
	return w.Attachments, err
}

func upload(ctx context.Context, c *transport.Client, path string, files []LocalFile) ([]Attachment, error) {
	parts := make([]transport.FilePart, len(files))
	for i, f := range files {
		open := openFile(f.Path)
		if f.Pipe {
			open = inMemory(f.Data)
		}
		parts[i] = transport.FilePart{Name: f.Name, ContentType: f.MimeType, Size: f.Size, Open: open}
	}
	var out []Attachment
	err := c.SendMultipart(ctx, path, parts, &out)
	return out, err
}

// inMemory serves a pipe's content, read once in full when it was checked,
// afresh for each send, so a redirect resends the same bytes.
func inMemory(data []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
}

func openFile(path string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("cannot attach %s: %w", path, err)
		}
		return f, nil
	}
}
