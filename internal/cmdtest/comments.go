package cmdtest

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
)

// commentRoutes edits and deletes comments and deletes issues.
func (yt *FakeYouTrack) commentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/issues/{id}/comments/{comment}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text *string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": err.Error()})
			return
		}
		c, ok := yt.comment(w, r)
		if !ok {
			return
		}
		yt.mu.Lock()
		if body.Text != nil {
			c["text"] = *body.Text
			c["updated"] = 1791460000000
		}
		out := maps.Clone(c)
		yt.mu.Unlock()
		yt.withCommentIssue(r, out)
		writeProjected(w, r, 200, out)
	})
	mux.HandleFunc("DELETE /api/issues/{id}/comments/{comment}", func(w http.ResponseWriter, r *http.Request) {
		c, ok := yt.comment(w, r)
		if !ok {
			return
		}
		is, _ := yt.issue(r.PathValue("id"))
		id := is["idReadable"].(string)
		yt.mu.Lock()
		yt.Comments[id] = slices.DeleteFunc(yt.Comments[id], func(m map[string]any) bool { return m["id"] == c["id"] })
		yt.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/issues/{id}", func(w http.ResponseWriter, r *http.Request) {
		is, ok := yt.issue(r.PathValue("id"))
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		id := is["idReadable"].(string)
		yt.mu.Lock()
		yt.Issues = slices.DeleteFunc(yt.Issues, func(m map[string]any) bool { return m["idReadable"] == id })
		delete(yt.Comments, id)
		delete(yt.Attachments, id)
		delete(yt.WorkItems, id)
		yt.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

// withCommentIssue sets the issue of a comment copy that was seeded
// without one, as YouTrack always has it.
func (yt *FakeYouTrack) withCommentIssue(r *http.Request, c map[string]any) {
	if c["issue"] != nil {
		return
	}
	if is, ok := yt.issue(r.PathValue("id")); ok {
		c["issue"] = map[string]any{"id": is["id"], "idReadable": is["idReadable"], "$type": "Issue"}
	}
}
