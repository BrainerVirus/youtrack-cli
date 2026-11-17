package cmdtest

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
)

// redirectingUploads holds the fakes set by RedirectUploads.
var redirectingUploads sync.Map

// RedirectUploads makes yt answer an issue upload with a 307 to the same URL,
// as a proxy that wants the request resent, unless it was already redirected.
func (yt *FakeYouTrack) RedirectUploads() { redirectingUploads.Store(yt, true) }

// attachToIssue stores the files of a multipart request as issue attachments.
func (yt *FakeYouTrack) attachToIssue(w http.ResponseWriter, r *http.Request) {
	is, ok := yt.issue(r.PathValue("id"))
	if !ok {
		notFound(w, r.PathValue("id"))
		return
	}
	if _, ok := redirectingUploads.Load(yt); ok && r.URL.Query().Get("redirected") == "" {
		http.Redirect(w, r, r.RequestURI+"&redirected=1", http.StatusTemporaryRedirect)
		return
	}
	parts, ok := readParts(w, r)
	if !ok {
		return
	}
	id := is["idReadable"].(string)
	yt.mu.Lock()
	defer yt.mu.Unlock()
	if yt.Attachments == nil {
		yt.Attachments = map[string][]map[string]any{}
	}
	created := []map[string]any{}
	for _, p := range parts {
		a := map[string]any{
			"id":   fmt.Sprintf("8-%d", 300+len(yt.Attachments[id])),
			"name": p.name, "size": len(p.data), "mimeType": p.mimeType,
			"content": string(p.data), "requestContentLength": r.ContentLength, "$type": "IssueAttachment",
		}
		yt.Attachments[id] = append(yt.Attachments[id], a)
		created = append(created, a)
	}
	writeProjected(w, r, 200, created)
}

// comment returns the stored comment with that ID on the issue, or writes a
// 404 and reports false.
func (yt *FakeYouTrack) comment(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	is, ok := yt.issue(r.PathValue("id"))
	if !ok {
		notFound(w, r.PathValue("id"))
		return nil, false
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	for _, c := range yt.Comments[is["idReadable"].(string)] {
		if c["id"] == r.PathValue("comment") {
			return c, true
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not Found", "error_description": "Entity with id " + r.PathValue("comment") + " not found"})
	return nil, false
}

func (yt *FakeYouTrack) getComment(w http.ResponseWriter, r *http.Request) {
	if c, ok := yt.comment(w, r); ok {
		yt.mu.Lock()
		defer yt.mu.Unlock()
		writeProjected(w, r, 200, c)
	}
}

// attachToComment stores the files of a multipart request as the comment's
// attachments, dropping those named in DropCommentAttachments.
func (yt *FakeYouTrack) attachToComment(w http.ResponseWriter, r *http.Request) {
	c, ok := yt.comment(w, r)
	if !ok {
		return
	}
	parts, ok := readParts(w, r)
	if !ok {
		return
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	stored, _ := c["attachments"].([]any)
	created := []map[string]any{}
	for _, p := range parts {
		if slices.Contains(yt.DropCommentAttachments, p.name) {
			continue
		}
		a := map[string]any{
			"id":   fmt.Sprintf("8-%d", 200+len(stored)),
			"name": p.name, "size": len(p.data), "mimeType": p.mimeType,
			"content": string(p.data), "$type": "IssueAttachment",
		}
		stored = append(stored, a)
		created = append(created, a)
	}
	c["attachments"] = stored
	writeProjected(w, r, 200, created)
}

type filePart struct {
	name, mimeType string
	data           []byte
}

// readParts reads the files of a multipart upload. Like a server that needs
// the size up front, it refuses a chunked upload with no Content-Length.
func readParts(w http.ResponseWriter, r *http.Request) ([]filePart, bool) {
	if r.ContentLength < 0 {
		writeJSON(w, http.StatusLengthRequired, map[string]any{"error": "length_required", "error_description": "an upload needs a Content-Length"})
		return nil, false
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": err.Error()})
		return nil, false
	}
	var out []filePart
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return out, true
		}
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": err.Error()})
			return nil, false
		}
		data, _ := io.ReadAll(p)
		out = append(out, filePart{name: p.FileName(), mimeType: p.Header.Get("Content-Type"), data: data})
	}
}
