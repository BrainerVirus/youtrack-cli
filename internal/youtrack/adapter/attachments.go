package adapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// Attachment is a file attached to an issue or a comment.
type Attachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
}

// IssueAttachment is a file on an issue as ListAttachments returns it:
// the attachment, who added it and when, and where to download it.
type IssueAttachment struct {
	Attachment
	Created time.Time
	Author  *User
	// CommentID is the comment the file is attached to, or "" for a file
	// attached to the issue itself.
	CommentID string
	// URL is the signed file URL, a path from the server root that includes
	// the service path; Download fetches it.
	URL string
}

const issueAttachmentFields = "id,name,size,mimeType,url,created,author(login,fullName),comment(id)"

type wireIssueAttachment struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	MimeType string    `json:"mimeType"`
	URL      string    `json:"url"`
	Created  *int64    `json:"created"`
	Author   *wireUser `json:"author"`
	Comment  *struct {
		ID string `json:"id"`
	} `json:"comment"`
}

// ListAttachments returns every attachment of an issue, those on its
// comments included.
func ListAttachments(ctx context.Context, c *transport.Client, issueID string) ([]IssueAttachment, error) {
	wire, err := transport.Paginate(0, PageSize, 0, func(skip, top int) ([]wireIssueAttachment, error) {
		var page []wireIssueAttachment
		err := c.GetJSON(ctx, fmt.Sprintf("/api/issues/%s/attachments?fields=%s&$skip=%d&$top=%d", url.PathEscape(issueID), issueAttachmentFields, skip, top), &page)
		return page, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]IssueAttachment, len(wire))
	for i, w := range wire {
		a := IssueAttachment{
			Attachment: Attachment{ID: w.ID, Name: w.Name, Size: w.Size, MimeType: w.MimeType},
			Created:    millis(w.Created), Author: w.Author.domain(), URL: w.URL,
		}
		if w.Comment != nil {
			a.CommentID = w.Comment.ID
		}
		out[i] = a
	}
	return out, nil
}

// DownloadAttachment streams the file of an attachment to w and returns the
// number of bytes written.
func DownloadAttachment(ctx context.Context, c *transport.Client, a IssueAttachment, w io.Writer) (int64, error) {
	if a.URL == "" {
		return 0, fmt.Errorf("YouTrack gave no download URL for %s", a.ID)
	}
	return c.Download(ctx, a.URL, w)
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
	var out []Attachment
	err := c.SendMultipart(ctx, "/api/issues/"+url.PathEscape(issueID)+"/attachments?fields=id,name,size,mimeType", fileParts(files), &out)
	return out, err
}

// AttachToComment uploads files to a comment in one request and returns the
// attachments YouTrack created. The comment's attachments collection takes
// the same multipart upload as the issue's; YouTrack ignores files sent
// inside the JSON that creates a comment and attachment IDs sent in a
// comment update.
func AttachToComment(ctx context.Context, c *transport.Client, issueID, commentID string, files []LocalFile) ([]Attachment, error) {
	var out []Attachment
	err := c.SendMultipart(ctx, commentPath(issueID, commentID)+"/attachments?fields=id,name,size,mimeType", fileParts(files), &out)
	return out, err
}

func fileParts(files []LocalFile) []transport.FilePart {
	parts := make([]transport.FilePart, len(files))
	for i, f := range files {
		open := openFile(f.Path)
		if f.Pipe {
			open = inMemory(f.Data)
		}
		parts[i] = transport.FilePart{Name: f.Name, ContentType: f.MimeType, Size: f.Size, Open: open}
	}
	return parts
}

// CommentFiles is what a posted comment shows of its files.
type CommentFiles struct {
	// Attachments are the files attached to the comment itself.
	Attachments []Attachment
	// TextPreview is the comment text as YouTrack renders it. A reference
	// such as ![](name) that YouTrack resolved to an attachment, the issue's
	// included, links /api/files/<attachment id>.
	TextPreview string
}

// ReadCommentFiles reads a comment's attachments and rendered text.
func ReadCommentFiles(ctx context.Context, c *transport.Client, issueID, commentID string) (CommentFiles, error) {
	var w struct {
		Attachments []Attachment `json:"attachments"`
		TextPreview string       `json:"textPreview"`
	}
	err := c.GetJSON(ctx, commentPath(issueID, commentID)+"?fields=attachments(id,name),textPreview", &w)
	return CommentFiles{Attachments: w.Attachments, TextPreview: w.TextPreview}, err
}

// Shows reports whether the rendered text resolves a reference to the
// attachment with that ID: an image carries data-attachment-id, and both an
// image and a link point at /api/files/<id>.
func (cf CommentFiles) Shows(attachmentID string) bool {
	if attachmentID == "" {
		return false
	}
	return strings.Contains(cf.TextPreview, `data-attachment-id="`+attachmentID+`"`) ||
		strings.Contains(cf.TextPreview, "/api/files/"+attachmentID+"?") ||
		strings.Contains(cf.TextPreview, "/api/files/"+attachmentID+`"`)
}

func commentPath(issueID, commentID string) string {
	return "/api/issues/" + url.PathEscape(issueID) + "/comments/" + url.PathEscape(commentID)
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
