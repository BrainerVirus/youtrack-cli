package shared

import "github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"

// AttachmentFields lists the --json fields of an issue's attachments.
var AttachmentFields = []string{"name", "id", "size", "mimeType", "created", "author", "commentId"}

// AttachmentFieldsHelp documents AttachmentFields for command help.
const AttachmentFieldsHelp = `  name       attachment name
  id         attachment ID
  size       size in bytes
  mimeType   media type
  created    when it was attached (RFC 3339, UTC)
  author     {login, fullName}
  commentId  the comment it is attached to, or null for the issue itself`

// Attachment is an issue attachment for --json output.
type Attachment struct{ adapter.IssueAttachment }

// ExportData implements output.Exportable.
func (a Attachment) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		switch f {
		case "name":
			out[f] = a.Name
		case "id":
			out[f] = a.ID
		case "size":
			out[f] = a.Size
		case "mimeType":
			out[f] = a.MimeType
		case "created":
			out[f] = exportTime(a.Created)
		case "author":
			out[f] = exportUser(a.Author)
		case "commentId":
			if a.CommentID == "" {
				out[f] = nil
			} else {
				out[f] = a.CommentID
			}
		}
	}
	return out
}
