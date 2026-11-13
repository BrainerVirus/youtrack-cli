package cmdtest

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// parseDataURI decodes "data:<type>;base64,<data>".
func parseDataURI(s string) (mediaType string, data []byte, ok bool) {
	rest, found := strings.CutPrefix(s, "data:")
	if !found {
		return "", nil, false
	}
	head, b64, found := strings.Cut(rest, ",")
	mediaType, found2 := strings.CutSuffix(head, ";base64")
	if !found || !found2 {
		return "", nil, false
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	return mediaType, data, err == nil
}

// attachToIssue stores the files of a multipart request as issue attachments.
// Like a server that needs the size up front, it refuses a chunked upload
// with no Content-Length.
func (yt *FakeYouTrack) attachToIssue(w http.ResponseWriter, r *http.Request) {
	is, ok := yt.issue(r.PathValue("id"))
	if !ok {
		notFound(w, r.PathValue("id"))
		return
	}
	if r.ContentLength < 0 {
		writeJSON(w, http.StatusLengthRequired, map[string]any{"error": "length_required", "error_description": "an upload needs a Content-Length"})
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": err.Error()})
		return
	}
	id := is["idReadable"].(string)
	yt.mu.Lock()
	defer yt.mu.Unlock()
	if yt.Attachments == nil {
		yt.Attachments = map[string][]map[string]any{}
	}
	created := []map[string]any{}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": err.Error()})
			return
		}
		data, _ := io.ReadAll(p)
		a := map[string]any{
			"id":   fmt.Sprintf("8-%d", 300+len(yt.Attachments[id])),
			"name": p.FileName(), "size": len(data), "mimeType": p.Header.Get("Content-Type"),
			"content": string(data), "requestContentLength": r.ContentLength, "$type": "IssueAttachment",
		}
		yt.Attachments[id] = append(yt.Attachments[id], a)
		created = append(created, a)
	}
	writeProjected(w, r, 200, created)
}
