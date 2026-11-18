package adapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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
	parts := make([]transport.FilePart, len(files))
	for i, f := range files {
		open := openFile(f.Path)
		if f.Pipe {
			open = inMemory(f.Data)
		}
		parts[i] = transport.FilePart{Name: f.Name, ContentType: f.MimeType, Size: f.Size, Open: open}
	}
	var out []Attachment
	err := c.SendMultipart(ctx, "/api/issues/"+url.PathEscape(issueID)+"/attachments?fields=id,name,size,mimeType", parts, &out)
	return out, err
}

// LinkToComment makes attachments already uploaded to the issue the
// comment's attachments. YouTrack has no upload endpoint for a comment and ignores files sent
// inside the JSON that creates one, so a comment gets its files by
// uploading them to the issue and linking them here.
func LinkToComment(ctx context.Context, c *transport.Client, issueID, commentID string, attachments []Attachment) error {
	ids := make([]map[string]string, len(attachments))
	for i, a := range attachments {
		ids[i] = map[string]string{"id": a.ID}
	}
	return c.SendJSON(ctx, http.MethodPost, commentPath(issueID, commentID)+"?fields=attachments(id,name)", map[string]any{"attachments": ids}, nil)
}

// CommentAttachments reads the attachments a comment has.
func CommentAttachments(ctx context.Context, c *transport.Client, issueID, commentID string) ([]Attachment, error) {
	var w struct {
		Attachments []Attachment `json:"attachments"`
	}
	err := c.GetJSON(ctx, commentPath(issueID, commentID)+"?fields=attachments(id,name)", &w)
	return w.Attachments, err
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
