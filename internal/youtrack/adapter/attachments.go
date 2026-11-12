package adapter

import (
	"context"
	"encoding/base64"
	"net/url"

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

// AttachToIssue uploads files to the issue in one request and returns the
// attachments YouTrack created.
func AttachToIssue(ctx context.Context, c *transport.Client, issueID string, files []File) ([]Attachment, error) {
	parts := make([]transport.FilePart, len(files))
	for i, f := range files {
		parts[i] = transport.FilePart{Name: f.Name, ContentType: f.MimeType, Data: f.Data}
	}
	var out []Attachment
	err := c.SendMultipart(ctx, "/api/issues/"+url.PathEscape(issueID)+"/attachments?fields=id,name,size,mimeType", parts, &out)
	return out, err
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
