package cmdtest

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
)

// todayMillis is the date the fake gives a work item created without one:
// 2026-10-07 at midnight UTC.
const todayMillis = 1791331200000

func (yt *FakeYouTrack) workItemRoutes(mux *http.ServeMux) {
	const items = "/api/issues/{id}/timeTracking/workItems"
	mux.HandleFunc("GET "+items, func(w http.ResponseWriter, r *http.Request) {
		is, ok := yt.issue(r.PathValue("id"))
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		yt.mu.Lock()
		list := slices.Clone(yt.WorkItems[is["idReadable"].(string)])
		yt.mu.Unlock()
		writeProjected(w, r, 200, page(r, list))
	})
	mux.HandleFunc("POST "+items, func(w http.ResponseWriter, r *http.Request) {
		is, ok := yt.issue(r.PathValue("id"))
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		minutes := durationMinutes(body)
		if minutes <= 0 {
			badRequest(w, "duration is required")
			return
		}
		id := is["idReadable"].(string)
		yt.mu.Lock()
		defer yt.mu.Unlock()
		if yt.WorkItems == nil {
			yt.WorkItems = map[string][]map[string]any{}
		}
		item := map[string]any{
			"$type": "IssueWorkItem", "id": fmt.Sprintf("115-%d", 100+len(yt.WorkItems[id])),
			"date": float64(todayMillis), "text": "", "type": nil,
			"author":  map[string]any{"$type": "User", "login": yt.Login, "fullName": yt.FullName},
			"creator": map[string]any{"$type": "User", "login": yt.Login, "fullName": yt.FullName},
			"issue":   map[string]any{"$type": "Issue", "id": is["id"], "idReadable": id},
		}
		if msg := yt.applyWorkItem(item, body, is); msg != "" {
			badRequest(w, msg)
			return
		}
		yt.WorkItems[id] = append(yt.WorkItems[id], item)
		writeProjected(w, r, 200, item)
	})
	mux.HandleFunc("GET "+items+"/{item}", func(w http.ResponseWriter, r *http.Request) {
		_, item, _, ok := yt.workItem(w, r)
		if ok {
			writeProjected(w, r, 200, item)
		}
	})
	mux.HandleFunc("POST "+items+"/{item}", func(w http.ResponseWriter, r *http.Request) {
		is, item, _, ok := yt.workItem(w, r)
		if !ok {
			return
		}
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		if _, ok := body["duration"]; ok && durationMinutes(body) <= 0 {
			badRequest(w, "duration must be positive")
			return
		}
		yt.mu.Lock()
		defer yt.mu.Unlock()
		if msg := yt.applyWorkItem(item, body, is); msg != "" {
			badRequest(w, msg)
			return
		}
		writeProjected(w, r, 200, item)
	})
	mux.HandleFunc("DELETE "+items+"/{item}", func(w http.ResponseWriter, r *http.Request) {
		_, item, owner, ok := yt.workItem(w, r)
		if !ok {
			return
		}
		yt.mu.Lock()
		yt.WorkItems[owner] = slices.DeleteFunc(yt.WorkItems[owner], func(m map[string]any) bool { return m["id"] == item["id"] })
		yt.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /api/admin/projects/{id}/timeTrackingSettings", func(w http.ResponseWriter, r *http.Request) {
		yt.mu.Lock()
		settings, ok := yt.TimeTracking[r.PathValue("id")]
		yt.mu.Unlock()
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		writeProjected(w, r, 200, settings)
	})
}

// workItem finds the issue a request names and the work item with the
// request's item ID, writing a 404 when either is missing. Like the global
// /api/workItems/{id}, the item is found by ID across all issues, so a
// request may reach another issue's item; owner is that item's issue. The
// item is the stored map itself.
func (yt *FakeYouTrack) workItem(w http.ResponseWriter, r *http.Request) (issue, item map[string]any, owner string, ok bool) {
	is, ok := yt.issue(r.PathValue("id"))
	if !ok {
		notFound(w, r.PathValue("id"))
		return nil, nil, "", false
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	for id, list := range yt.WorkItems {
		for _, it := range list {
			if it["id"] == r.PathValue("item") {
				return is, it, id, true
			}
		}
	}
	notFound(w, r.PathValue("item"))
	return nil, nil, "", false
}

// applyWorkItem copies the attributes in body onto item, as YouTrack does,
// and returns an error message for a type the issue's project lacks. The
// caller holds yt.mu.
func (yt *FakeYouTrack) applyWorkItem(item, body, issue map[string]any) string {
	if minutes := durationMinutes(body); minutes > 0 {
		item["duration"] = map[string]any{"$type": "DurationValue", "minutes": float64(minutes), "presentation": presentation(minutes)}
	}
	if d, ok := body["date"]; ok {
		item["date"] = d
	}
	if t, ok := body["text"]; ok {
		item["text"] = t
	}
	if t, ok := body["type"].(map[string]any); ok {
		project, _ := issue["project"].(map[string]any)
		settings := yt.TimeTracking[fmt.Sprint(project["id"])]
		types, _ := settings["workItemTypes"].([]any)
		var found map[string]any
		for _, ty := range types {
			if m, ok := ty.(map[string]any); ok && m["id"] == t["id"] {
				found = maps.Clone(m)
			}
		}
		if found == nil {
			return fmt.Sprintf("work item type %v is not available in the project", t["id"])
		}
		item["type"] = found
	}
	return ""
}

func decodeBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badRequest(w, err.Error())
		return nil, false
	}
	return body, true
}

func durationMinutes(body map[string]any) int {
	d, _ := body["duration"].(map[string]any)
	m, _ := d["minutes"].(float64)
	return int(m)
}

func presentation(minutes int) string {
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": msg})
}
