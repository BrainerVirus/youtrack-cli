package cmdtest

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
)

// sampleProjects are the projects as /api/admin/projects returns them.
const sampleProjects = `[
  {"$type": "Project", "id": "0-12", "shortName": "NSR", "name": "Nightshift", "archived": false, "leader": {"$type": "User", "login": "jroe"}},
  {"$type": "Project", "id": "0-13", "shortName": "OPS", "name": "Operations", "archived": false, "leader": {"$type": "User", "login": "jdoe"}},
  {"$type": "Project", "id": "0-9", "shortName": "OLD", "name": "Legacy", "archived": true, "leader": null}
]`

// sampleProjectFields are the Nightshift project's custom fields as
// /api/admin/projects/0-12/customFields returns them. "issueType" is not a
// YouTrack attribute: it is the IssueCustomField $type the fake requires
// when the field is written, independently of ytrack's own mapping.
const sampleProjectFields = `[
  {"$type": "EnumProjectCustomField", "id": "93-10", "canBeEmpty": false, "issueType": "SingleEnumIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-1", "name": "Priority", "fieldType": {"$type": "FieldType", "id": "enum[1]", "isMultiValue": false}},
   "bundle": {"$type": "EnumBundle", "id": "68-1", "values": [
     {"$type": "EnumBundleElement", "id": "67-0", "name": "Show-stopper", "archived": true},
     {"$type": "EnumBundleElement", "id": "67-1", "name": "Major", "archived": false},
     {"$type": "EnumBundleElement", "id": "67-2", "name": "Critical", "archived": false},
     {"$type": "EnumBundleElement", "id": "67-3", "name": "Normal", "archived": false},
     {"$type": "EnumBundleElement", "id": "67-4", "name": "Minor", "archived": false}]}},
  {"$type": "EnumProjectCustomField", "id": "93-11", "canBeEmpty": false, "issueType": "SingleEnumIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-2", "name": "Type", "fieldType": {"$type": "FieldType", "id": "enum[1]"}},
   "bundle": {"$type": "EnumBundle", "id": "68-2", "values": [
     {"$type": "EnumBundleElement", "id": "67-9", "name": "Bug", "archived": false},
     {"$type": "EnumBundleElement", "id": "67-10", "name": "Feature", "archived": false},
     {"$type": "EnumBundleElement", "id": "67-11", "name": "Task", "archived": false}]}},
  {"$type": "StateProjectCustomField", "id": "93-12", "canBeEmpty": false, "issueType": "StateIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-3", "name": "State", "fieldType": {"$type": "FieldType", "id": "state[1]"}},
   "bundle": {"$type": "StateBundle", "id": "68-3", "values": [
     {"$type": "StateBundleElement", "id": "69-1", "name": "Open", "archived": false, "isResolved": false},
     {"$type": "StateBundleElement", "id": "69-2", "name": "In Progress", "archived": false, "isResolved": false},
     {"$type": "StateBundleElement", "id": "69-4", "name": "Fixed", "archived": false, "isResolved": true}]}},
  {"$type": "UserProjectCustomField", "id": "93-13", "canBeEmpty": true, "issueType": "SingleUserIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-4", "name": "Assignee", "fieldType": {"$type": "FieldType", "id": "user[1]"}},
   "bundle": {"$type": "UserBundle", "id": "68-4", "aggregatedUsers": [
     {"$type": "User", "id": "1-3", "login": "jdoe", "fullName": "John Doe"},
     {"$type": "User", "id": "1-7", "login": "jroe", "fullName": "Jane Roe"}]}},
  {"$type": "OwnedProjectCustomField", "id": "93-14", "canBeEmpty": true, "issueType": "MultiOwnedIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-5", "name": "Subsystem", "fieldType": {"$type": "FieldType", "id": "ownedField[*]"}},
   "bundle": {"$type": "OwnedBundle", "id": "68-5", "values": [
     {"$type": "OwnedBundleElement", "id": "70-1", "name": "Auth", "archived": false},
     {"$type": "OwnedBundleElement", "id": "70-2", "name": "Web", "archived": false},
     {"$type": "OwnedBundleElement", "id": "70-3", "name": "API", "archived": false}]}},
  {"$type": "VersionProjectCustomField", "id": "93-15", "canBeEmpty": true, "issueType": "MultiVersionIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-6", "name": "Fix versions", "fieldType": {"$type": "FieldType", "id": "version[*]"}},
   "bundle": {"$type": "VersionBundle", "id": "68-6", "values": [
     {"$type": "VersionBundleElement", "id": "71-1", "name": "2026.3", "archived": false},
     {"$type": "VersionBundleElement", "id": "71-2", "name": "2026.4", "archived": false}]}},
  {"$type": "PeriodProjectCustomField", "id": "93-1", "canBeEmpty": true, "issueType": "PeriodIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-7", "name": "Estimation", "fieldType": {"$type": "FieldType", "id": "period"}}},
  {"$type": "SimpleProjectCustomField", "id": "93-16", "canBeEmpty": true, "issueType": "DateIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-8", "name": "Due Date", "fieldType": {"$type": "FieldType", "id": "date"}}},
  {"$type": "SimpleProjectCustomField", "id": "93-17", "canBeEmpty": true, "issueType": "SimpleIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-9", "name": "Story points", "fieldType": {"$type": "FieldType", "id": "integer"}}},
  {"$type": "SimpleProjectCustomField", "id": "93-18", "canBeEmpty": true, "issueType": "SimpleIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-10", "name": "Deployed", "fieldType": {"$type": "FieldType", "id": "date and time"}}},
  {"$type": "GroupProjectCustomField", "id": "93-20", "canBeEmpty": true, "issueType": "SingleGroupIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-12", "name": "Team", "fieldType": {"$type": "FieldType", "id": "group[1]"}},
   "bundle": {"$type": "UserBundle", "id": "68-7", "groups": [
     {"$type": "UserGroup", "id": "3-1", "name": "Developers"},
     {"$type": "UserGroup", "id": "3-2", "name": "QA"}]}},
  {"$type": "TextProjectCustomField", "id": "93-19", "canBeEmpty": true, "issueType": "TextIssueCustomField",
   "field": {"$type": "CustomField", "id": "58-11", "name": "Root cause", "fieldType": {"$type": "FieldType", "id": "text"}}}
]`

// sampleTags are the tags the user can see.
const sampleTags = `[
  {"$type": "Tag", "id": "6-1", "name": "backend", "owner": {"$type": "User", "login": "jdoe"}},
  {"$type": "Tag", "id": "6-2", "name": "sso", "owner": {"$type": "User", "login": "jdoe"}},
  {"$type": "Tag", "id": "6-3", "name": "regression", "owner": {"$type": "User", "login": "jroe"}}
]`

// SeedSampleProjects adds the NSR, OPS and (archived) OLD projects, NSR's
// custom field definitions and three tags. With Projects set, POST
// /api/issues creates issues instead of echoing the body.
func (yt *FakeYouTrack) SeedSampleProjects() {
	var projects, fields, tags []map[string]any
	for s, v := range map[string]*[]map[string]any{sampleProjects: &projects, sampleProjectFields: &fields, sampleTags: &tags} {
		if err := json.Unmarshal([]byte(s), v); err != nil {
			panic(err)
		}
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	yt.Projects = projects
	yt.ProjectFields = map[string][]map[string]any{"0-12": fields, "0-13": {}}
	yt.Tags = tags
}

func (yt *FakeYouTrack) issueWriteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/projects", func(w http.ResponseWriter, r *http.Request) {
		yt.mu.Lock()
		ps := slices.Clone(yt.Projects)
		yt.mu.Unlock()
		writeProjected(w, r, 200, page(r, ps))
	})
	mux.HandleFunc("GET /api/admin/projects/{id}/customFields", func(w http.ResponseWriter, r *http.Request) {
		yt.mu.Lock()
		fs, ok := yt.ProjectFields[r.PathValue("id")]
		fs = slices.Clone(fs)
		yt.mu.Unlock()
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		writeProjected(w, r, 200, page(r, fs))
	})
	mux.HandleFunc("GET /api/admin/projects/{id}/customFields/{field}", func(w http.ResponseWriter, r *http.Request) {
		yt.mu.Lock()
		fs := slices.Clone(yt.ProjectFields[r.PathValue("id")])
		yt.mu.Unlock()
		for _, f := range fs {
			if f["id"] == r.PathValue("field") {
				writeProjected(w, r, 200, f)
				return
			}
		}
		notFound(w, r.PathValue("field"))
	})
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		yt.mu.Lock()
		ts := slices.Clone(yt.Tags)
		yt.mu.Unlock()
		writeProjected(w, r, 200, page(r, ts))
	})
	mux.HandleFunc("POST /api/issues", yt.createIssue)
	mux.HandleFunc("POST /api/issues/{id}", func(w http.ResponseWriter, r *http.Request) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		yt.mu.Lock()
		defer yt.mu.Unlock()
		is := yt.liveIssue(r.PathValue("id"))
		if is == nil {
			notFound(w, r.PathValue("id"))
			return
		}
		for _, k := range []string{"project", "tags", "idReadable", "id"} {
			if _, ok := body[k]; ok {
				badRequest(w, k+" cannot be changed here")
				return
			}
		}
		if msg := yt.applyIssue(is, body); msg != "" {
			badRequest(w, msg)
			return
		}
		writeProjected(w, r, 200, is)
	})
	mux.HandleFunc("POST /api/issues/{id}/tags", func(w http.ResponseWriter, r *http.Request) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		yt.mu.Lock()
		defer yt.mu.Unlock()
		is := yt.liveIssue(r.PathValue("id"))
		if is == nil {
			notFound(w, r.PathValue("id"))
			return
		}
		tag := yt.tag(fmt.Sprint(body["id"]))
		if tag == nil {
			notFound(w, fmt.Sprint(body["id"]))
			return
		}
		tags, _ := is["tags"].([]any)
		is["tags"] = append(tags, tag)
		writeProjected(w, r, 200, tag)
	})
	mux.HandleFunc("DELETE /api/issues/{id}/tags/{tag}", func(w http.ResponseWriter, r *http.Request) {
		yt.mu.Lock()
		defer yt.mu.Unlock()
		is := yt.liveIssue(r.PathValue("id"))
		if is == nil {
			notFound(w, r.PathValue("id"))
			return
		}
		tags, _ := is["tags"].([]any)
		kept := slices.DeleteFunc(slices.Clone(tags), func(t any) bool {
			m, _ := t.(map[string]any)
			return m["id"] == r.PathValue("tag")
		})
		if len(kept) == len(tags) {
			notFound(w, r.PathValue("tag"))
			return
		}
		is["tags"] = kept
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/commands", func(w http.ResponseWriter, r *http.Request) { yt.command(w, r, false) })
	mux.HandleFunc("POST /api/commands/assist", func(w http.ResponseWriter, r *http.Request) { yt.command(w, r, true) })
}

// createIssue creates an issue as YouTrack does, checking the project,
// summary, custom field $types and values, and tags. Without Projects it
// echoes the body (for the api command tests).
func (yt *FakeYouTrack) createIssue(w http.ResponseWriter, r *http.Request) {
	yt.mu.Lock()
	seeded := yt.Projects != nil
	yt.mu.Unlock()
	if !seeded {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, r.Body)
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	pref, _ := body["project"].(map[string]any)
	var project map[string]any
	for _, p := range yt.Projects {
		if pref != nil && p["id"] == pref["id"] {
			project = p
		}
	}
	if project == nil {
		badRequest(w, "Project is required")
		return
	}
	if s, _ := body["summary"].(string); strings.TrimSpace(s) == "" {
		badRequest(w, "Summary is required")
		return
	}
	n := 100 + len(yt.Issues)
	is := map[string]any{
		"$type": "Issue", "id": fmt.Sprintf("2-%d", 5000+n), "idReadable": fmt.Sprintf("%s-%d", project["shortName"], n),
		"project":  map[string]any{"$type": "Project", "id": project["id"], "shortName": project["shortName"], "name": project["name"]},
		"reporter": map[string]any{"$type": "User", "login": yt.Login, "fullName": yt.FullName},
		"created":  float64(todayMillis), "updated": float64(todayMillis), "resolved": nil,
		"description": nil, "tags": []any{}, "customFields": []any{},
	}
	if tags, ok := body["tags"].([]any); ok {
		for _, t := range tags {
			m, _ := t.(map[string]any)
			tag := yt.tag(fmt.Sprint(m["id"]))
			if tag == nil {
				badRequest(w, fmt.Sprintf("Tag %v not found", m["id"]))
				return
			}
			is["tags"] = append(is["tags"].([]any), tag)
		}
	}
	delete(body, "tags")
	delete(body, "project")
	if msg := yt.applyIssue(is, body); msg != "" {
		badRequest(w, msg)
		return
	}
	yt.Issues = append(yt.Issues, is)
	writeProjected(w, r, 200, is)
}

// liveIssue finds an issue for in-place changes; the caller holds mu.
func (yt *FakeYouTrack) liveIssue(id string) map[string]any {
	for _, is := range yt.Issues {
		if is["idReadable"] == id || (is["id"] != nil && is["id"] == id) {
			return is
		}
	}
	return nil
}

func (yt *FakeYouTrack) tag(id string) map[string]any {
	for _, t := range yt.Tags {
		if t["id"] == id {
			return t
		}
	}
	return nil
}

func (yt *FakeYouTrack) fieldDefs(is map[string]any) []map[string]any {
	p, _ := is["project"].(map[string]any)
	return yt.ProjectFields[fmt.Sprint(p["id"])]
}

// applyIssue sets summary, description and customFields from body on is,
// validating custom fields against the project's definitions. It returns
// YouTrack's error message, or "". The caller holds mu.
func (yt *FakeYouTrack) applyIssue(is, body map[string]any) string {
	for k := range body {
		if !slices.Contains([]string{"summary", "description", "customFields"}, k) {
			return "Unknown attribute " + k
		}
	}
	cfs, _ := body["customFields"].([]any)
	updated := map[string]any{}
	for _, raw := range cfs {
		cf, _ := raw.(map[string]any)
		name := fmt.Sprint(cf["name"])
		def := findDef(yt.fieldDefs(is), name)
		if def == nil {
			return "Unknown custom field: " + name
		}
		if cf["$type"] != def["issueType"] {
			return fmt.Sprintf("Incompatible field type: %s for %s", cf["$type"], name)
		}
		if msg := checkValue(def, cf["value"]); msg != "" {
			return msg
		}
		updated[name] = map[string]any{"$type": cf["$type"], "name": name, "value": cf["value"]}
	}
	for _, k := range []string{"summary", "description"} {
		if v, ok := body[k]; ok {
			is[k] = v
		}
	}
	for name, cf := range updated {
		setField(is, name, cf.(map[string]any))
	}
	is["updated"] = float64(todayMillis + 1000)
	return ""
}

func setField(is map[string]any, name string, cf map[string]any) {
	list, _ := is["customFields"].([]any)
	list = slices.DeleteFunc(slices.Clone(list), func(e any) bool {
		m, _ := e.(map[string]any)
		return m["name"] == name
	})
	is["customFields"] = append(list, cf)
}

func findDef(defs []map[string]any, name string) map[string]any {
	for _, d := range defs {
		if f, _ := d["field"].(map[string]any); f["name"] == name {
			return d
		}
	}
	return nil
}

// checkValue validates a written value against a field definition.
func checkValue(def, value any) string {
	d := def.(map[string]any)
	ft := d["field"].(map[string]any)["fieldType"].(map[string]any)["id"].(string)
	if value == nil {
		if d["canBeEmpty"] == false {
			return "Field cannot be empty"
		}
		return ""
	}
	items := []any{value}
	if strings.HasSuffix(ft, "[*]") {
		var ok bool
		if items, ok = value.([]any); !ok {
			return "Expected an array value"
		}
	}
	for _, it := range items {
		switch {
		case strings.HasPrefix(ft, "user"):
			m, _ := it.(map[string]any)
			if !slices.ContainsFunc(bundleMembers(d, "aggregatedUsers", "login"), func(l string) bool { return l == m["login"] }) {
				return fmt.Sprintf("User %v not found", m["login"])
			}
		case strings.HasPrefix(ft, "group"):
			m, _ := it.(map[string]any)
			if !slices.Contains(bundleMembers(d, "groups", "name"), fmt.Sprint(m["name"])) {
				return fmt.Sprintf("Group %v not found", m["name"])
			}
		case strings.Contains(ft, "["):
			m, _ := it.(map[string]any)
			if !slices.Contains(bundleMembers(d, "values", "name"), fmt.Sprint(m["name"])) {
				return fmt.Sprintf("Value %v not found in bundle", m["name"])
			}
		case ft == "date" || ft == "date and time" || ft == "integer":
			if _, ok := it.(float64); !ok {
				return "Expected a number"
			}
		case ft == "text":
			if m, ok := it.(map[string]any); !ok || m["text"] == nil {
				return "Expected a text value"
			}
		case ft == "period":
			return "Period values are not set this way in the fake"
		}
	}
	return ""
}

func bundleMembers(def map[string]any, list, key string) []string {
	b, _ := def["bundle"].(map[string]any)
	items, _ := b[list].([]any)
	var out []string
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["archived"] == true {
			continue
		}
		out = append(out, fmt.Sprint(m[key]))
	}
	return out
}

// command parses a YouTrack command for the issues in the body: "for
// <login|me>", "tag <name>" and "<Field> <value>" for the project's fields.
// It applies the command unless preview is set; a command with an
// unparseable part is a 400, as in YouTrack.
func (yt *FakeYouTrack) command(w http.ResponseWriter, r *http.Request, preview bool) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	query, _ := body["query"].(string)
	refs, _ := body["issues"].([]any)
	yt.mu.Lock()
	defer yt.mu.Unlock()
	var issues []map[string]any
	for _, ref := range refs {
		m, _ := ref.(map[string]any)
		id, _ := m["idReadable"].(string)
		if id == "" {
			id, _ = m["id"].(string)
		}
		is := yt.liveIssue(id)
		if is == nil {
			notFound(w, id)
			return
		}
		issues = append(issues, is)
	}
	if len(issues) == 0 {
		badRequest(w, "No issues")
		return
	}
	parts := yt.parseCommand(query, yt.fieldDefs(issues[0]))
	commands := make([]any, len(parts))
	failed := false
	for i, p := range parts {
		commands[i] = map[string]any{"$type": "ParsedCommand", "description": p.description, "error": p.err, "delete": false}
		failed = failed || p.err
	}
	if !preview {
		if failed || len(parts) == 0 {
			badRequest(w, "Command ["+query+"] is invalid")
			return
		}
		for _, is := range issues {
			for _, p := range parts {
				p.apply(is)
			}
			if c, _ := body["comment"].(string); c != "" {
				id := is["idReadable"].(string)
				if yt.Comments == nil {
					yt.Comments = map[string][]map[string]any{}
				}
				yt.Comments[id] = append(yt.Comments[id], map[string]any{
					"$type": "IssueComment", "id": fmt.Sprintf("4-%d", 200+len(yt.Comments[id])), "text": c,
					"author": map[string]any{"$type": "User", "login": yt.Login, "fullName": yt.FullName}, "created": float64(todayMillis),
				})
			}
		}
	}
	writeProjected(w, r, 200, map[string]any{"$type": "CommandList", "query": query, "commands": commands})
}

type commandPart struct {
	description string
	err         bool
	apply       func(is map[string]any)
}

var periodValue = regexp.MustCompile(`^(?i)(\d+[wdhm])+$`)

// parseCommand splits query into parts at field names and "for"/"tag".
func (yt *FakeYouTrack) parseCommand(query string, defs []map[string]any) []commandPart {
	words := strings.Fields(query)
	names := []string{}
	for _, d := range defs {
		names = append(names, d["field"].(map[string]any)["name"].(string))
	}
	// keywordAt returns the field name or keyword starting at word i.
	keywordAt := func(i int) string {
		best := ""
		for _, n := range append([]string{"for", "tag"}, names...) {
			nw := strings.Fields(n)
			if i+len(nw) <= len(words) && strings.EqualFold(strings.Join(words[i:i+len(nw)], " "), n) && len(n) > len(best) {
				best = n
			}
		}
		return best
	}
	var parts []commandPart
	for i := 0; i < len(words); {
		kw := keywordAt(i)
		if kw == "" {
			parts = append(parts, commandPart{description: "Unknown command: " + words[i], err: true})
			i++
			continue
		}
		i += len(strings.Fields(kw))
		j := i
		for j < len(words) && keywordAt(j) == "" {
			j++
		}
		value := strings.Join(words[i:j], " ")
		i = j
		parts = append(parts, yt.commandPart(kw, value, defs))
	}
	return parts
}

func (yt *FakeYouTrack) commandPart(kw, value string, defs []map[string]any) commandPart {
	bad := func(msg string) commandPart { return commandPart{description: msg, err: true} }
	switch kw {
	case "for":
		login := value
		if login == "me" {
			login = yt.Login
		}
		if login == "" {
			return bad("for: missing user")
		}
		return commandPart{description: "Assignee: " + login, apply: func(is map[string]any) {
			setField(is, "Assignee", map[string]any{"$type": "SingleUserIssueCustomField", "name": "Assignee", "value": map[string]any{"$type": "User", "login": login, "fullName": login}})
		}}
	case "tag":
		for _, t := range yt.Tags {
			if strings.EqualFold(fmt.Sprint(t["name"]), value) {
				return commandPart{description: "Add tag " + value, apply: func(is map[string]any) {
					tags, _ := is["tags"].([]any)
					is["tags"] = append(tags, maps.Clone(t))
				}}
			}
		}
		return bad("Unknown tag: " + value)
	}
	def := findDef(defs, kw)
	ft := def["field"].(map[string]any)["fieldType"].(map[string]any)["id"].(string)
	if ft == "period" {
		if !periodValue.MatchString(value) {
			return bad(kw + ": invalid period " + value)
		}
		return commandPart{description: kw + ": " + value, apply: func(is map[string]any) {
			setField(is, kw, map[string]any{"$type": "PeriodIssueCustomField", "name": kw, "value": map[string]any{"$type": "PeriodValue", "minutes": periodMinutes(value), "presentation": value}})
		}}
	}
	for _, v := range bundleMembers(def, "values", "name") {
		if strings.EqualFold(v, value) {
			return commandPart{description: kw + ": " + v, apply: func(is map[string]any) {
				setField(is, kw, map[string]any{"$type": def["issueType"], "name": kw, "value": map[string]any{"$type": "BundleElement", "name": v}})
			}}
		}
	}
	return bad(kw + ": unknown value " + value)
}

// periodMinutes converts a duration like 1w2d3h with YouTrack's default
// schedule: 5-day weeks of 8-hour days.
func periodMinutes(v string) int {
	units := map[byte]int{'w': 5 * 8 * 60, 'd': 8 * 60, 'h': 60, 'm': 1}
	total, n := 0, 0
	for i := 0; i < len(v); i++ {
		c := v[i] | 0x20
		if v[i] >= '0' && v[i] <= '9' {
			n = n*10 + int(v[i]-'0')
			continue
		}
		total += n * units[c]
		n = 0
	}
	return total
}
