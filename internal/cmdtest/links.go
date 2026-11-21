package cmdtest

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// Link is a link between two issues, by readable ID. For an undirected
// type, Source and Target are interchangeable.
type Link struct {
	TypeID, Source, Target string
}

// defaultLinkTypes are YouTrack Cloud's predefined link types, as
// GET /api/issueLinkTypes answers them.
var defaultLinkTypes = []map[string]any{
	{"$type": "IssueLinkType", "id": "173-0", "name": "Relates", "directed": false, "sourceToTarget": "relates to", "targetToSource": ""},
	{"$type": "IssueLinkType", "id": "173-1", "name": "Depend", "directed": true, "sourceToTarget": "is required for", "targetToSource": "depends on"},
	{"$type": "IssueLinkType", "id": "173-2", "name": "Duplicate", "directed": true, "sourceToTarget": "is duplicated by", "targetToSource": "duplicates"},
	{"$type": "IssueLinkType", "id": "173-3", "name": "Subtask", "directed": true, "sourceToTarget": "parent for", "targetToSource": "subtask of"},
}

// linkRoutes serves issue links:
//
//	GET    /api/issueLinkTypes                       LinkTypes (defaultLinkTypes when nil), paged
//	GET    /api/issues/{id}/links                    one link per type end, as YouTrack
//	                                                 does: <type> (BOTH) for an undirected
//	                                                 type, <type>s (OUTWARD) and <type>t
//	                                                 (INWARD) for a directed one
//	GET    /api/issues/{id}/links/{link}/issues      the issues on that end, paged
//	POST   /api/issues/{id}/links/{link}/issues      adds {"idReadable"} or {"id"} (no
//	                                                 duplicates); 400 (localized) for an unknown target
//	DELETE /api/issues/{id}/links/{link}/issues/{issue}  removes the issue with that
//	                                                 database ID; a readable ID or an issue
//	                                                 not on that end is a 404, as on YouTrack
func (yt *FakeYouTrack) linkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/issueLinkTypes", func(w http.ResponseWriter, r *http.Request) {
		writeProjected(w, r, 200, page(r, yt.linkTypes()))
	})
	mux.HandleFunc("GET /api/issues/{id}/links", func(w http.ResponseWriter, r *http.Request) {
		is, ok := yt.issue(r.PathValue("id"))
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		var out []map[string]any
		for _, t := range yt.linkTypes() {
			for _, end := range linkEnds(t) {
				out = append(out, map[string]any{
					"$type": "IssueLink", "id": end.id, "direction": end.direction, "linkType": t,
					"issues": yt.linkedIssues(is["idReadable"].(string), end),
				})
			}
		}
		writeProjected(w, r, 200, page(r, out))
	})
	mux.HandleFunc("GET /api/issues/{id}/links/{link}/issues", func(w http.ResponseWriter, r *http.Request) {
		is, end, ok := yt.linkEnd(w, r)
		if ok {
			writeProjected(w, r, 200, page(r, yt.linkedIssues(is, end)))
		}
	})
	mux.HandleFunc("POST /api/issues/{id}/links/{link}/issues", func(w http.ResponseWriter, r *http.Request) {
		is, end, ok := yt.linkEnd(w, r)
		if !ok {
			return
		}
		var body struct {
			ID         string `json:"id"`
			IDReadable string `json:"idReadable"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": err.Error()})
			return
		}
		ref := body.IDReadable
		if ref == "" {
			ref = body.ID
		}
		target, ok := yt.issue(ref)
		if !ok {
			// YouTrack answers an unknown target with 400 in the instance's
			// language (measured on Cloud 2026.2, Spanish), not 404.
			writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": "YouTrack no puede localizar una entidad de tipo Issue con el ID especificado"})
			return
		}
		l := end.link(is, target["idReadable"].(string))
		yt.mu.Lock()
		if !slices.ContainsFunc(yt.Links, end.matches(l)) {
			yt.Links = append(yt.Links, l)
		}
		yt.mu.Unlock()
		writeProjected(w, r, 200, target)
	})
	mux.HandleFunc("DELETE /api/issues/{id}/links/{link}/issues/{issue}", func(w http.ResponseWriter, r *http.Request) {
		is, end, ok := yt.linkEnd(w, r)
		if !ok {
			return
		}
		dbID := r.PathValue("issue")
		target, ok := yt.issue(dbID)
		if !ok || target["id"] != dbID {
			notFound(w, dbID)
			return
		}
		l := end.link(is, target["idReadable"].(string))
		yt.mu.Lock()
		n := len(yt.Links)
		yt.Links = slices.DeleteFunc(yt.Links, end.matches(l))
		removed := len(yt.Links) < n
		yt.mu.Unlock()
		if !removed {
			notFound(w, dbID)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (yt *FakeYouTrack) linkTypes() []map[string]any {
	yt.mu.Lock()
	defer yt.mu.Unlock()
	if yt.LinkTypes == nil {
		return defaultLinkTypes
	}
	return slices.Clone(yt.LinkTypes)
}

type fakeLinkEnd struct {
	id, typeID, direction string
}

func linkEnds(t map[string]any) []fakeLinkEnd {
	id := t["id"].(string)
	if t["directed"] != true {
		return []fakeLinkEnd{{id, id, "BOTH"}}
	}
	return []fakeLinkEnd{{id + "s", id, "OUTWARD"}, {id + "t", id, "INWARD"}}
}

// link is the link that adds other to issue's end e.
func (e fakeLinkEnd) link(issue, other string) Link {
	if e.direction == "INWARD" {
		return Link{TypeID: e.typeID, Source: other, Target: issue}
	}
	return Link{TypeID: e.typeID, Source: issue, Target: other}
}

// matches returns a test for links that are the same as l, a link on end e:
// an undirected link has no source or target.
func (e fakeLinkEnd) matches(l Link) func(Link) bool {
	return func(o Link) bool {
		return l == o || (e.direction == "BOTH" && l.TypeID == o.TypeID && l.Source == o.Target && l.Target == o.Source)
	}
}

// linkEnd finds the issue and the link end a request names, or writes a 404.
func (yt *FakeYouTrack) linkEnd(w http.ResponseWriter, r *http.Request) (string, fakeLinkEnd, bool) {
	is, ok := yt.issue(r.PathValue("id"))
	if !ok {
		notFound(w, r.PathValue("id"))
		return "", fakeLinkEnd{}, false
	}
	for _, t := range yt.linkTypes() {
		for _, end := range linkEnds(t) {
			if end.id == r.PathValue("link") {
				return is["idReadable"].(string), end, true
			}
		}
	}
	notFound(w, r.PathValue("link"))
	return "", fakeLinkEnd{}, false
}

// linkedIssues returns the issues on issue's end e, as {id, idReadable}.
func (yt *FakeYouTrack) linkedIssues(issue string, e fakeLinkEnd) []map[string]any {
	yt.mu.Lock()
	links := slices.Clone(yt.Links)
	yt.mu.Unlock()
	out := []map[string]any{}
	for _, l := range links {
		if l.TypeID != e.typeID {
			continue
		}
		var other string
		switch {
		case e.direction != "INWARD" && strings.EqualFold(l.Source, issue):
			other = l.Target
		case e.direction != "OUTWARD" && strings.EqualFold(l.Target, issue):
			other = l.Source
		default:
			continue
		}
		if is, ok := yt.issue(other); ok {
			out = append(out, map[string]any{"$type": "Issue", "id": is["id"], "idReadable": is["idReadable"]})
		}
	}
	return out
}
