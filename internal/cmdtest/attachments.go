package cmdtest

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"maps"
	"net/http"
	"regexp"
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
	yt.mu.Lock()
	defer yt.mu.Unlock()
	writeProjected(w, r, 200, yt.storeParts(is["idReadable"].(string), r, parts, nil))
}

// attachToComment stores the files of a multipart request as the comment's
// attachments. With CommentUploadsToIssue it keeps them on the issue only;
// with RejectCommentUploads it answers as a server without the endpoint.
func (yt *FakeYouTrack) attachToComment(w http.ResponseWriter, r *http.Request) {
	if yt.RejectCommentUploads {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not Found", "error_description": "No subresource for path attachments"})
		return
	}
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
	issueID := c["issue"].(map[string]any)["idReadable"].(string)
	if yt.CommentUploadsToIssue {
		c = nil
	}
	writeProjected(w, r, 200, yt.storeParts(issueID, r, parts, c))
}

// storeParts adds parts to the issue's attachments, and to comment c unless
// it is nil, and returns those it created. A file named in DropUploads is
// accepted but not kept. The caller holds yt.mu.
func (yt *FakeYouTrack) storeParts(issueID string, r *http.Request, parts []filePart, c map[string]any) []map[string]any {
	if yt.Attachments == nil {
		yt.Attachments = map[string][]map[string]any{}
	}
	created := []map[string]any{}
	for _, p := range parts {
		if slices.Contains(yt.DropUploads, p.name) {
			continue
		}
		a := map[string]any{
			"id":   fmt.Sprintf("8-%d", 300+len(yt.Attachments[issueID])),
			"name": p.name, "size": len(p.data), "mimeType": p.mimeType,
			"content": string(p.data), "requestContentLength": r.ContentLength, "$type": "IssueAttachment",
		}
		if c != nil {
			a["comment"] = map[string]any{"id": c["id"], "$type": "IssueComment"}
			c["attachments"] = append(c["attachments"].([]any), a)
		}
		yt.Attachments[issueID] = append(yt.Attachments[issueID], a)
		created = append(created, a)
	}
	return created
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

// getComment answers a comment with its textPreview rendered as YouTrack
// does it for attachment references.
func (yt *FakeYouTrack) getComment(w http.ResponseWriter, r *http.Request) {
	if c, ok := yt.comment(w, r); ok {
		yt.mu.Lock()
		c = maps.Clone(c)
		yt.mu.Unlock()
		yt.withCommentIssue(r, c)
		yt.mu.Lock()
		defer yt.mu.Unlock()
		c["textPreview"] = yt.textPreview(c["issue"].(map[string]any)["idReadable"].(string), fmt.Sprint(c["text"]))
		writeProjected(w, r, 200, c)
	}
}

var attachmentRef = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)\s]+)\)`)

// textPreview renders the references in a comment's text the way YouTrack
// does: ![](name) and [label](name) resolve against the issue's attachments,
// those of its comments included, by name (the newest one wins). A resolved
// image carries data-attachment-id; a resolved link only the file URL; an
// unresolved image keeps the bare name. The caller holds yt.mu.
func (yt *FakeYouTrack) textPreview(issueID, text string) string {
	body := attachmentRef.ReplaceAllStringFunc(text, func(ref string) string {
		m := attachmentRef.FindStringSubmatch(ref)
		image, label, name := m[1] == "!", html.EscapeString(m[2]), m[3]
		var found map[string]any
		for _, a := range yt.Attachments[issueID] {
			if a["name"] == name {
				found = a
			}
		}
		switch {
		case found == nil && image:
			return `<img src="` + html.EscapeString(name) + `" alt="` + label + `">`
		case found == nil:
			return `<a href="` + html.EscapeString(name) + `">` + label + `</a>`
		}
		src := yt.Prefix + "/api/files/" + fmt.Sprint(found["id"]) + "?sign=" + fileSign(found) + "&amp;updated=1791374400000"
		if image {
			return `<img src="` + src + `" alt="` + label + `" data-attachment-id="` + fmt.Sprint(found["id"]) + `">`
		}
		return `<a href="` + src + `" target="_blank" rel="noopener noreferrer">` + label + `</a>`
	})
	return `<div class="wiki text common-markdown"><p>` + body + "</p>\n</div>"
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

// listAttachments answers an issue's attachments, paged, each with a signed
// url as YouTrack gives it: a path from the server root, service path
// included.
func (yt *FakeYouTrack) listAttachments(w http.ResponseWriter, r *http.Request) {
	is, ok := yt.issue(r.PathValue("id"))
	if !ok {
		notFound(w, r.PathValue("id"))
		return
	}
	yt.mu.Lock()
	items := []map[string]any{}
	for _, a := range yt.Attachments[is["idReadable"].(string)] {
		a = maps.Clone(a)
		a["url"] = yt.Prefix + "/api/files/" + fmt.Sprint(a["id"]) + "?sign=" + fileSign(a) + "&updated=1791374400000"
		items = append(items, a)
	}
	yt.mu.Unlock()
	writeProjected(w, r, 200, page(r, items))
}

func fileSign(a map[string]any) string { return "sig-" + fmt.Sprint(a["id"]) }

// downloadFile serves an attachment's content at its signed URL; an unknown
// file or a wrong sign is YouTrack's 404.
func (yt *FakeYouTrack) downloadFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("file")
	yt.mu.Lock()
	defer yt.mu.Unlock()
	for _, list := range yt.Attachments {
		for _, a := range list {
			if a["id"] != id || r.URL.Query().Get("sign") != fileSign(a) || slices.Contains(yt.MissingFiles, id) {
				continue
			}
			if mt, _ := a["mimeType"].(string); mt != "" {
				w.Header().Set("Content-Type", mt)
			}
			_, _ = io.WriteString(w, fmt.Sprint(a["content"]))
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not Found", "error_description": "File with id " + id + " not found"})
}

// sampleAttachments are NSR-40's attachments: one on the issue and one on
// comment 4-2.
const sampleAttachments = `[
  {"$type": "IssueAttachment", "id": "8-1", "name": "trace.log", "size": 14, "mimeType": "text/plain", "content": "GET /login 302",
   "created": 1791300000000, "author": {"$type": "User", "login": "jroe", "fullName": "Jane Roe"}, "comment": null},
  {"$type": "IssueAttachment", "id": "8-2", "name": "shot.png", "size": 11, "mimeType": "image/png", "content": "\u0089PNG-bytes",
   "created": 1791350000000, "author": {"$type": "User", "login": "jdoe", "fullName": "John Doe"}, "comment": {"$type": "IssueComment", "id": "4-2"}}
]`

// SeedSampleAttachments gives NSR-40 two attachments, trace.log on the issue
// and shot.png on comment 4-2.
func (yt *FakeYouTrack) SeedSampleAttachments() {
	var list []map[string]any
	if err := json.Unmarshal([]byte(sampleAttachments), &list); err != nil {
		panic(err)
	}
	for _, a := range list {
		a["size"] = len(fmt.Sprint(a["content"]))
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	if yt.Attachments == nil {
		yt.Attachments = map[string][]map[string]any{}
	}
	yt.Attachments["NSR-40"] = list
}

// AddAttachment adds a file with that name and content to an issue's
// attachments and returns its ID.
func (yt *FakeYouTrack) AddAttachment(issue, name, content string) string {
	yt.mu.Lock()
	defer yt.mu.Unlock()
	if yt.Attachments == nil {
		yt.Attachments = map[string][]map[string]any{}
	}
	id := fmt.Sprintf("8-%d", 500+len(yt.Attachments[issue]))
	yt.Attachments[issue] = append(yt.Attachments[issue], map[string]any{
		"$type": "IssueAttachment", "id": id, "name": name, "size": len(content), "mimeType": "text/plain",
		"content": content, "created": 1791374400000, "author": map[string]any{"$type": "User", "login": yt.Login, "fullName": yt.FullName},
	})
	return id
}
