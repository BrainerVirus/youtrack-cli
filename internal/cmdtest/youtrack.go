package cmdtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Request is a request the fake server received.
type Request struct {
	Method   string
	Path     string
	RawQuery string
	Header   http.Header
	Body     string
}

// FakeYouTrack is a minimal YouTrack REST API:
//
//	GET  /api/users/me          the token's user
//	GET  /api/issues                Issues, paged with $skip/$top (default $top 42)
//	                                and restricted by customFields= when given
//	POST /api/issues                creates an issue (see SeedSampleProjects); echoes
//	                                the JSON body back when no projects are seeded
//	POST /api/issues/{id}           updates summary, description, customFields
//	                                and watchers.hasStar (the token user's star)
//	POST|DELETE /api/issues/{id}/tags[/{tag}]  tags and untags an issue
//	GET  /api/admin/projects        Projects, paged
//	GET  /api/admin/projects/{id}/customFields  ProjectFields[project id], paged
//	                                (empty when HideFieldSettings)
//	GET  /api/admin/projects/{id}/customFields/{field}  one of them
//	GET  /api/tags                  Tags, paged
//	POST /api/tags                  appends {"name"} to Tags, owned by jdoe
//	POST /api/commands[/assist]     applies or previews a small subset of the
//	                                YouTrack command language (see command);
//	                                the preview also suggests completions
//	GET  /api/issues/{id}           the issue in Issues with that idReadable or id,
//	                                with commentsCount; 404 with a YouTrack body if none
//	GET  /api/issues/{id}/comments  Comments[idReadable], paged with $skip/$top
//	POST /api/issues/{id}/comments  appends {"text"} to Comments and returns it;
//	                                like YouTrack, it ignores an "attachments" field
//	GET  /api/issues/{id}/comments/{comment}  one of Comments, with a
//	                                textPreview that resolves ![](name) and
//	                                [label](name) against the issue's Attachments
//	POST|DELETE /api/issues/{id}/comments/{comment}  changes a comment's text
//	                                (setting updated) or deletes it
//	DELETE /api/issues/{id}         deletes the issue with its comments,
//	                                attachments and work items
//	POST /api/issues/{id}/comments/{comment}/attachments  stores multipart files
//	                                as the comment's attachments (also listed in
//	                                Attachments) and returns them; see
//	                                CommentUploadsToIssue, RejectCommentUploads
//	POST /api/issues/{id}/attachments  stores multipart files in Attachments and
//	                                returns them (neither upload keeps DropUploads)
//	GET  /api/issues/{id}/attachments  Attachments[idReadable], paged, each with a
//	                                signed url ({Prefix}/api/files/{id}?sign=...)
//	GET  /api/files/{id}            an attachment's content at its signed URL; 404
//	                                for an unknown file or a wrong sign
//	GET  /api/issues/{id}/timeTracking/workItems         WorkItems[idReadable], paged
//	POST /api/issues/{id}/timeTracking/workItems         creates a work item from
//	                                {duration{minutes}, date, text, type{id}}; 400 for
//	                                a missing duration or a type the project lacks
//	GET|POST|DELETE /api/issues/{id}/timeTracking/workItems/{item}
//	                                reads, updates (merging the body) or deletes one,
//	                                found by ID across all issues (any {id})
//	GET  /api/admin/projects/{id}/timeTrackingSettings   TimeTracking[project id]
//	GET  /api/admin/timeTrackingSettings/workTimeSettings  the work schedule
//	                                (MinutesADay, DaysAWeek); 403 with ForbidWorkSchedule
//	GET  /api/issueLinkTypes, GET /api/issues/{id}/links, and GET|POST|DELETE
//	                                /api/issues/{id}/links/{link}/issues[/{issue}]
//	                                read and change Links (see linkRoutes)
//	GET  /api/workItems, GET /api/issues/{id}/activities, GET /api/savedQueries,
//	POST /api/issuesGetter/count and POST /api/search/assist serve reports
//	                                and search help (see reportRoutes)
//	GET  /api/plain                 a text/plain body
//	GET  /users/me                  the web token page (no auth; 404 if HubTokenPage)
//
// Like YouTrack, a request with a body that is not JSON (or multipart for
// attachments) is a 415.
//
// Like YouTrack, JSON responses keep only the attributes named in fields=
// (plus $type), when fields= is given. Every /api route requires
// "Authorization: Bearer <Token>".
type FakeYouTrack struct {
	Server   *httptest.Server
	Prefix   string
	Token    string
	Login    string
	FullName string
	Issues   []map[string]any
	// Comments holds each issue's comments by readable ID, oldest first.
	Comments map[string][]map[string]any
	// Attachments holds each issue's issue-level attachments by readable ID.
	Attachments map[string][]map[string]any
	// DropUploads names files that an upload, to the issue or a comment,
	// accepts but does not keep, as a server that loses them would.
	DropUploads []string
	// CommentUploadsToIssue makes an upload to a comment keep its files on
	// the issue only, as a server that ignores the comment would.
	CommentUploadsToIssue bool
	// RejectCommentUploads answers an upload to a comment with a 404, as a
	// server without that endpoint would.
	RejectCommentUploads bool
	// MissingFiles names attachment IDs that are listed but whose file is
	// gone: downloading one is a 404.
	MissingFiles []string
	// WorkItems holds each issue's work items by readable ID.
	WorkItems map[string][]map[string]any
	// LinkTypes are the issue link types; nil means YouTrack Cloud's
	// predefined ones (Relates, Depend, Duplicate, Subtask).
	LinkTypes []map[string]any
	// Links are the links between Issues.
	Links []Link
	// TimeTracking holds project time tracking settings by project ID.
	TimeTracking map[string]map[string]any
	// Projects, ProjectFields (by project ID) and Tags back issue writes.
	Projects      []map[string]any
	ProjectFields map[string][]map[string]any
	Tags          []map[string]any
	// HideFieldSettings answers the project settings' custom field list
	// with an empty list and a single field with a 403, as YouTrack does
	// for a user who may read the project's issues but not its settings.
	// The definitions stay readable through an issue's
	// customFields(projectCustomField(...)).
	HideFieldSettings bool
	// IgnoreFieldWrites names custom fields whose REST writes YouTrack
	// accepts (200) but leaves unchanged, as a workflow can.
	IgnoreFieldWrites []string
	// IgnoreStarWrites makes a watchers.hasStar write answer 200 without
	// changing the star.
	IgnoreStarWrites bool
	// ForbidSilentCommands answers a command with "silent": true with a
	// 403, as for a user without Apply Commands Silently.
	ForbidSilentCommands bool
	// Localized makes the search keyword "for:" match nothing, as on a
	// non-English instance, where keywords are translated but field names
	// such as Assignee are not.
	Localized bool
	// KnownUsers, when set, are the only logins an "Assignee:" search term
	// may name besides Login; any other is a 400 invalid_query, as YouTrack
	// answers a search for a user it does not have.
	KnownUsers []string
	// MaxTop caps $top like a server limit would (0 means no cap).
	MaxTop int
	// HubTokenPage makes the instance's token page 404, as on a Server
	// install with an external Hub.
	HubTokenPage bool
	// MinutesADay and DaysAWeek are the instance's work schedule, which
	// period commands use and workTimeSettings answers; 0 means YouTrack's
	// default of 480 and 5.
	MinutesADay, DaysAWeek int
	// ForbidWorkSchedule answers workTimeSettings with a 403, as YouTrack
	// does for a user who is not an administrator.
	ForbidWorkSchedule bool
	// CommandHTML makes command previews describe each part in HTML, as
	// YouTrack does: values HTML-escaped inside <span class="bold">.
	CommandHTML bool
	// CommandDescriptions, when set, replaces the description of each parsed
	// command part, in order, exactly as given (for hostile previews).
	CommandDescriptions []string
	// Activities holds each issue's history by readable ID, oldest first.
	Activities map[string][]map[string]any
	// SavedQueries are the saved searches the user sees.
	SavedQueries []map[string]any
	// CountPending is the number of issue counts answered with -1, as
	// YouTrack does while it is still counting, before the real count.
	CountPending int

	mu       sync.Mutex
	requests []Request
}

// NewFakeYouTrack starts a fake server mounted at prefix ("" or e.g. "/youtrack").
func NewFakeYouTrack(t *testing.T, prefix string) *FakeYouTrack {
	t.Helper()
	yt := &FakeYouTrack{Prefix: prefix, Token: "perm-am9obg==.dGVzdA==.s3cr3tT0k3nValue", Login: "jdoe", FullName: "John Doe"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/me", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"login": yt.Login, "fullName": yt.FullName, "$type": "Me"})
	})
	mux.HandleFunc("GET /api/issues", func(w http.ResponseWriter, r *http.Request) {
		if login, ok := yt.unknownAssignee(r.URL.Query().Get("query")); ok {
			writeJSON(w, 400, map[string]any{"error": "invalid_query", "error_description": "Can't parse search query: unknown user " + login})
			return
		}
		items := []map[string]any{}
		for _, is := range page(r, yt.queryIssues(r.URL.Query().Get("query"))) {
			items = append(items, yt.withFieldSettings(r, onlyCustomFields(is, r.URL.Query()["customFields"])))
		}
		writeProjected(w, r, 200, items)
	})
	mux.HandleFunc("GET /api/issues/{id}", func(w http.ResponseWriter, r *http.Request) {
		is, ok := yt.issue(r.PathValue("id"))
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		writeProjected(w, r, 200, yt.withFieldSettings(r, is))
	})
	mux.HandleFunc("GET /api/issues/{id}/comments", func(w http.ResponseWriter, r *http.Request) {
		is, ok := yt.issue(r.PathValue("id"))
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		yt.mu.Lock()
		comments := make([]map[string]any, len(yt.Comments[is["idReadable"].(string)]))
		for i, c := range yt.Comments[is["idReadable"].(string)] {
			comments[i] = maps.Clone(c)
		}
		yt.mu.Unlock()
		for _, c := range comments {
			yt.withCommentIssue(r, c)
		}
		writeProjected(w, r, 200, page(r, comments))
	})
	mux.HandleFunc("POST /api/issues/{id}/comments", func(w http.ResponseWriter, r *http.Request) {
		is, ok := yt.issue(r.PathValue("id"))
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]any{"error": "bad_request", "error_description": err.Error()})
			return
		}
		id := is["idReadable"].(string)
		yt.mu.Lock()
		if yt.Comments == nil {
			yt.Comments = map[string][]map[string]any{}
		}
		c := map[string]any{
			"id": fmt.Sprintf("4-%d", 100+len(yt.Comments[id])), "text": body.Text, "$type": "IssueComment",
			"author":  map[string]any{"login": yt.Login, "fullName": yt.FullName, "$type": "User"},
			"created": 1791374400000, "updated": nil,
			"issue":       map[string]any{"id": is["id"], "idReadable": id, "$type": "Issue"},
			"attachments": []any{},
		}
		yt.Comments[id] = append(yt.Comments[id], c)
		yt.mu.Unlock()
		writeProjected(w, r, 200, c)
	})
	mux.HandleFunc("POST /api/issues/{id}/attachments", yt.attachToIssue)
	mux.HandleFunc("GET /api/issues/{id}/attachments", yt.listAttachments)
	mux.HandleFunc("GET /api/files/{file}", yt.downloadFile)
	mux.HandleFunc("GET /api/issues/{id}/comments/{comment}", yt.getComment)
	mux.HandleFunc("POST /api/issues/{id}/comments/{comment}/attachments", yt.attachToComment)
	yt.workItemRoutes(mux)
	yt.issueWriteRoutes(mux)
	yt.linkRoutes(mux)
	yt.commentRoutes(mux)
	yt.reportRoutes(mux)
	mux.HandleFunc("GET /api/plain", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "plain text")
	})

	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me" { // the web UI token page, no API auth
			if yt.HubTokenPage {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, "<html>Account security</html>")
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+yt.Token {
			writeJSON(w, 401, map[string]any{"error": "Unauthorized", "error_description": "You are not logged in."})
			return
		}
		ct := r.Header.Get("Content-Type")
		multipartOK := strings.HasPrefix(ct, "multipart/form-data") && strings.HasSuffix(r.URL.Path, "/attachments")
		if r.ContentLength != 0 && r.Method != http.MethodGet && !strings.Contains(ct, "json") && !multipartOK {
			writeJSON(w, 415, map[string]any{"error": "Unsupported Media Type", "error_description": "HTTP 415 Unsupported Media Type"})
			return
		}
		mux.ServeHTTP(w, r)
	})
	inner := http.NewServeMux()
	if prefix != "" {
		inner.Handle(prefix+"/", http.StripPrefix(prefix, authed))
	} else {
		inner.Handle("/", authed)
	}
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), fakeKey{}, yt))
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		yt.mu.Lock()
		yt.requests = append(yt.requests, Request{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Header: r.Header.Clone(), Body: string(body)})
		yt.mu.Unlock()
		inner.ServeHTTP(w, r)
	})
	yt.Server = httptest.NewServer(root)
	t.Cleanup(yt.Server.Close)
	return yt
}

// URL is the service URL, including the prefix.
func (yt *FakeYouTrack) URL() string { return yt.Server.URL + yt.Prefix }

// Key is the host key ytrack derives from URL.
func (yt *FakeYouTrack) Key() string { return strings.TrimPrefix(yt.URL(), "http://") }

// Requests returns the requests received so far.
func (yt *FakeYouTrack) Requests() []Request {
	yt.mu.Lock()
	defer yt.mu.Unlock()
	return append([]Request(nil), yt.requests...)
}

// LastRequest returns the most recent request.
func (yt *FakeYouTrack) LastRequest(t *testing.T) Request {
	t.Helper()
	reqs := yt.Requests()
	if len(reqs) == 0 {
		t.Fatal("the fake YouTrack received no requests")
	}
	return reqs[len(reqs)-1]
}

// SeedIssues fills Issues with n issues APP-1..APP-n.
func (yt *FakeYouTrack) SeedIssues(n int) {
	yt.Issues = nil
	for i := 1; i <= n; i++ {
		yt.Issues = append(yt.Issues, map[string]any{"idReadable": fmt.Sprintf("APP-%d", i), "summary": fmt.Sprintf("Issue %d", i)})
	}
}

func (yt *FakeYouTrack) issue(id string) (map[string]any, bool) {
	yt.mu.Lock()
	defer yt.mu.Unlock()
	for _, is := range yt.Issues {
		if is["idReadable"] == id || (is["id"] != nil && is["id"] == id) {
			out := maps.Clone(is)
			out["commentsCount"] = len(yt.Comments[fmt.Sprint(is["idReadable"])])
			return out, true
		}
	}
	return nil, false
}

func notFound(w http.ResponseWriter, id string) {
	writeJSON(w, 404, map[string]any{"error": "Not Found", "error_description": "Entity with id " + id + " not found"})
}

// page applies $skip and $top (default 42, capped by MaxTop) to items.
func page[T any](r *http.Request, items []T) []T {
	q := r.URL.Query()
	skip, _ := strconv.Atoi(q.Get("$skip"))
	top := 42
	if v := q.Get("$top"); v != "" {
		top, _ = strconv.Atoi(v)
	}
	if yt, ok := r.Context().Value(fakeKey{}).(*FakeYouTrack); ok && yt.MaxTop > 0 {
		top = min(top, yt.MaxTop)
	}
	out := []T{}
	for i := skip; i < len(items) && i < skip+top; i++ {
		out = append(out, items[i])
	}
	return out
}

type fakeKey struct{}

// onlyCustomFields keeps the custom fields called one of names, as
// YouTrack's customFields= parameter does; no names keeps them all.
func onlyCustomFields(issue map[string]any, names []string) map[string]any {
	cfs, ok := issue["customFields"].([]any)
	if !ok || len(names) == 0 {
		return issue
	}
	out := maps.Clone(issue)
	kept := []any{}
	for _, cf := range cfs {
		if m, ok := cf.(map[string]any); ok && slices.Contains(names, fmt.Sprint(m["name"])) {
			kept = append(kept, cf)
		}
	}
	out["customFields"] = kept
	return out
}

// writeProjected writes v keeping only the attributes in the request's
// fields= projection, as YouTrack does.
func writeProjected(w http.ResponseWriter, r *http.Request, status int, v any) {
	if fields := r.URL.Query().Get("fields"); fields != "" {
		tree, err := ParseProjection(fields)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "invalid_query", "error_description": err.Error()})
			return
		}
		v = project(normalize(v), tree)
	}
	writeJSON(w, status, v)
}

// Projection is a parsed fields= value: attribute name to its nested
// projection (nil for a leaf).
type Projection map[string]Projection

// ParseProjection parses YouTrack's fields syntax, e.g. "id,project(name)".
func ParseProjection(s string) (Projection, error) {
	p, rest, err := parseProjection(s)
	if err != nil {
		return nil, err
	}
	if rest != "" {
		return nil, fmt.Errorf("unexpected %q in fields", rest)
	}
	return p, nil
}

func parseProjection(s string) (Projection, string, error) {
	p := Projection{}
	for {
		i := strings.IndexAny(s, ",()")
		if i < 0 {
			i = len(s)
		}
		name := strings.TrimSpace(s[:i])
		if name == "" {
			return nil, s, fmt.Errorf("empty attribute name in fields near %q", s)
		}
		s = s[i:]
		var sub Projection
		if strings.HasPrefix(s, "(") {
			var err error
			sub, s, err = parseProjection(s[1:])
			if err != nil {
				return nil, s, err
			}
			if !strings.HasPrefix(s, ")") {
				return nil, s, errors.New("unbalanced parentheses in fields")
			}
			s = s[1:]
		}
		p[name] = sub
		if !strings.HasPrefix(s, ",") {
			return p, s, nil
		}
		s = s[1:]
	}
}

func normalize(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func project(v any, p Projection) any {
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = project(e, p)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		if typ, ok := t["$type"]; ok {
			out["$type"] = typ
		}
		for name, sub := range p {
			val, ok := t[name]
			if !ok {
				continue
			}
			if sub != nil {
				val = project(val, sub)
			}
			out[name] = val
		}
		return out
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
