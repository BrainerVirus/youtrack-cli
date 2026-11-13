package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// File is a file to attach to an issue or a comment.
type File struct {
	// Name is the attachment name; a comment's text shows it inline with
	// ![](Name).
	Name     string
	MimeType string
	Data     []byte
}

// Attachment is a file attached to an issue.
type Attachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
}

// LocalFile is a file on disk to upload as an issue attachment. The upload
// streams a regular file from Path rather than holding it in memory.
type LocalFile struct {
	Name     string
	MimeType string
	Path     string
	// Size is the length the file had when it was checked; the upload fails
	// if the file changes size before it is sent.
	Size int64
	// Pipe marks a file that can be read only once, such as a FIFO: Data
	// holds its content, and a redirect that would resend it fails.
	Pipe bool
	Data []byte
}

// AttachToIssue uploads files to the issue in one request and returns the
// attachments YouTrack created.
func AttachToIssue(ctx context.Context, c *transport.Client, issueID string, files []LocalFile) ([]Attachment, error) {
	parts := make([]transport.FilePart, len(files))
	for i, f := range files {
		open := openFile(f.Path)
		if f.Pipe {
			open = readOnce(f.Path, f.Data)
		}
		parts[i] = transport.FilePart{Name: f.Name, ContentType: f.MimeType, Size: f.Size, Open: open}
	}
	var out []Attachment
	err := c.SendMultipart(ctx, "/api/issues/"+url.PathEscape(issueID)+"/attachments?fields=id,name,size,mimeType", parts, &out)
	return out, err
}

// readOnce serves a pipe's content for the first send only: a pipe cannot be
// reopened, so a redirect asking to resend the upload fails naming it rather
// than sending a body that differs from the first.
func readOnce(path string, data []byte) func() (io.ReadCloser, error) {
	sent := false
	return func() (io.ReadCloser, error) {
		if sent {
			return nil, fmt.Errorf("cannot attach %s: the server redirected the upload, and a pipe cannot be read again to resend it", path)
		}
		sent = true
		return io.NopCloser(bytes.NewReader(data)), nil
	}
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

// commentAttachments encodes files as IssueComment attachments: YouTrack
// takes base64Content as a data URI, which carries the media type.
func commentAttachments(files []File) []map[string]string {
	out := make([]map[string]string, len(files))
	for i, f := range files {
		mt := f.MimeType
		if mt == "" {
			mt = "application/octet-stream"
		}
		out[i] = map[string]string{
			"name":          f.Name,
			"base64Content": "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(f.Data),
		}
	}
	return out
}
