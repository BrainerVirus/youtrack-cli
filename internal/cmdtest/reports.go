package cmdtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
)

// reportRoutes serve the cross-issue reads:
//
//	GET  /api/workItems             every issue's WorkItems, filtered by author
//	                                (a login), startDate/endDate (YYYY-MM-DD,
//	                                inclusive) and a "project: X" query; paged
//	GET  /api/issues/{id}/activities  Activities[idReadable] in the requested
//	                                categories (400 without categories), paged
//	GET  /api/savedQueries          SavedQueries, paged
//	POST /api/issuesGetter/count    {"count"} of the issues {"query"} matches
//	                                (see queryIssues); -1 for the first
//	                                CountPending calls, as while YouTrack counts
//	POST /api/search/assist         SearchSuggestions for the query's last word
func (yt *FakeYouTrack) reportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workItems", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var from, to time.Time
		for _, d := range []struct {
			name string
			out  *time.Time
		}{{"startDate", &from}, {"endDate", &to}} {
			if v := q.Get(d.name); v != "" {
				t, err := time.Parse(time.DateOnly, v)
				if err != nil {
					badRequest(w, d.name+" must be YYYY-MM-DD")
					return
				}
				*d.out = t
			}
		}
		project := ""
		if m := workItemProject.FindStringSubmatch(q.Get("query")); m != nil {
			project = m[1]
		}
		yt.mu.Lock()
		var all []map[string]any
		for _, is := range yt.Issues {
			id := fmt.Sprint(is["idReadable"])
			if project != "" && !strings.HasPrefix(id, project+"-") {
				continue
			}
			for _, item := range yt.WorkItems[id] {
				author, _ := item["author"].(map[string]any)
				if a := q.Get("author"); a != "" && (author == nil || author["login"] != a) {
					continue
				}
				ms, _ := item["date"].(float64)
				day := time.UnixMilli(int64(ms)).UTC()
				if (!from.IsZero() && day.Before(from)) || (!to.IsZero() && day.After(to)) {
					continue
				}
				all = append(all, item)
			}
		}
		yt.mu.Unlock()
		writeProjected(w, r, 200, page(r, all))
	})
	mux.HandleFunc("GET /api/issues/{id}/activities", func(w http.ResponseWriter, r *http.Request) {
		is, ok := yt.issue(r.PathValue("id"))
		if !ok {
			notFound(w, r.PathValue("id"))
			return
		}
		cats := strings.Split(r.URL.Query().Get("categories"), ",")
		if slices.Equal(cats, []string{""}) {
			badRequest(w, "categories are required")
			return
		}
		yt.mu.Lock()
		var out []map[string]any
		for _, a := range yt.Activities[fmt.Sprint(is["idReadable"])] {
			c, _ := a["category"].(map[string]any)
			if c != nil && slices.Contains(cats, fmt.Sprint(c["id"])) {
				out = append(out, a)
			}
		}
		yt.mu.Unlock()
		writeProjected(w, r, 200, page(r, out))
	})
	mux.HandleFunc("GET /api/savedQueries", func(w http.ResponseWriter, r *http.Request) {
		yt.mu.Lock()
		qs := slices.Clone(yt.SavedQueries)
		yt.mu.Unlock()
		writeProjected(w, r, 200, page(r, qs))
	})
	mux.HandleFunc("POST /api/issuesGetter/count", func(w http.ResponseWriter, r *http.Request) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		query, _ := body["query"].(string)
		yt.mu.Lock()
		count := len(yt.queryIssues(query))
		if yt.CountPending > 0 {
			yt.CountPending--
			count = -1
		}
		yt.mu.Unlock()
		writeProjected(w, r, 200, map[string]any{"$type": "IssueCountResponse", "count": count})
	})
	mux.HandleFunc("POST /api/search/assist", func(w http.ResponseWriter, r *http.Request) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		query, _ := body["query"].(string)
		caret, _ := body["caret"].(float64)
		writeProjected(w, r, 200, map[string]any{
			"$type": "SearchSuggestions", "query": query, "caret": caret,
			"suggestions": suggest(query, int(caret), searchWords),
		})
	})
}

var workItemProject = regexp.MustCompile(`(?:^|\s)project: \{?([^}\s]+)\}?`)

// searchWords are what the fake's search assist completes.
var searchWords = []string{"#Unresolved", "#Resolved", "project:", "Assignee:", "State:", "tag:"}

// suggest completes the word that ends at caret (in UTF-16 units) with
// each of words it is a prefix of, as YouTrack's assist does.
func suggest(text string, caret int, words []string) []any {
	u := utf16.Encode([]rune(text))
	caret = max(0, min(caret, len(u)))
	start := caret
	for start > 0 && u[start-1] != ' ' {
		start--
	}
	partial := string(utf16.Decode(u[start:caret]))
	out := []any{}
	for _, word := range words {
		if partial == "" || (len(word) > len(partial) && strings.HasPrefix(strings.ToLower(word), strings.ToLower(partial))) {
			out = append(out, map[string]any{
				"$type": "Suggestion", "option": word, "description": "complete " + word,
				"prefix": "", "suffix": " ", "completionStart": start, "completionEnd": caret,
				"matchingStart": start, "matchingEnd": caret, "caret": start + len(word) + 1,
			})
		}
	}
	return out
}

// sampleActivities are NSR-40's history: its creation, a State change, a
// tag and a comment.
const sampleActivities = `[
  {"$type": "IssueCreatedActivityItem", "id": "a-1", "timestamp": 1790000000000, "author": {"$type": "User", "login": "jroe", "fullName": "Jane Roe"}, "category": {"$type": "ActivityCategory", "id": "IssueCreatedCategory"}, "field": null, "target": {"$type": "Issue", "id": "2-4040"}, "added": [], "removed": []},
  {"$type": "CustomFieldActivityItem", "id": "a-2", "timestamp": 1790100000000, "author": {"$type": "User", "login": "jdoe", "fullName": "John Doe"}, "category": {"$type": "ActivityCategory", "id": "CustomFieldCategory"}, "field": {"$type": "CustomFilterField", "id": "State", "name": "State", "presentation": "State"}, "added": [{"$type": "StateBundleElement", "id": "69-2", "name": "In Progress"}], "removed": [{"$type": "StateBundleElement", "id": "69-1", "name": "Open"}]},
  {"$type": "TagsActivityItem", "id": "a-3", "timestamp": 1790200000000, "author": {"$type": "User", "login": "jdoe", "fullName": "John Doe"}, "category": {"$type": "ActivityCategory", "id": "TagsCategory"}, "field": {"$type": "PredefinedFilterField", "id": "tag", "name": "tag", "presentation": "tag"}, "added": [{"$type": "Tag", "id": "6-2", "name": "sso"}], "removed": []},
  {"$type": "CommentActivityItem", "id": "a-4", "timestamp": 1791200000000, "author": {"$type": "User", "login": "jroe", "fullName": "Jane Roe"}, "category": {"$type": "ActivityCategory", "id": "CommentsCategory"}, "field": {"$type": "PredefinedFilterField", "id": "comments", "name": "comments", "presentation": "comments"}, "added": [{"$type": "IssueComment", "id": "4-1", "text": "Reproduced on staging."}], "removed": []},
  {"$type": "SimpleValueActivityItem", "id": "a-5", "timestamp": 1791250000000, "author": {"$type": "User", "login": "jdoe", "fullName": "John Doe"}, "category": {"$type": "ActivityCategory", "id": "SummaryCategory"}, "field": {"$type": "PredefinedFilterField", "id": "summary", "name": "summary", "presentation": "summary"}, "added": "Login redirect drops the return URL", "removed": "Login drops the URL"}
]`

// sampleSavedQueries are two saved searches.
const sampleSavedQueries = `[
  {"$type": "SavedQuery", "id": "120-1", "name": "My open", "query": "for: me #Unresolved", "owner": {"$type": "User", "login": "jdoe", "fullName": "John Doe"}},
  {"$type": "SavedQuery", "id": "120-2", "name": "Nightshift bugs", "query": "project: NSR Type: Bug", "owner": {"$type": "User", "login": "jroe", "fullName": "Jane Roe"}}
]`

// SeedSampleHistory gives NSR-40 an activity history, two watchers (the
// token's user by star) and two votes, and adds two saved searches. Seed
// the issues first.
func (yt *FakeYouTrack) SeedSampleHistory() {
	var acts, queries []map[string]any
	if err := json.Unmarshal([]byte(sampleActivities), &acts); err != nil {
		panic(err)
	}
	if err := json.Unmarshal([]byte(sampleSavedQueries), &queries); err != nil {
		panic(err)
	}
	yt.mu.Lock()
	defer yt.mu.Unlock()
	yt.Activities = map[string][]map[string]any{"NSR-40": acts}
	yt.SavedQueries = queries
	is := yt.liveIssue("NSR-40")
	is["watchers"] = map[string]any{"$type": "IssueWatchers", "hasStar": true, "issueWatchers": []any{
		map[string]any{"$type": "IssueWatcher", "isStarred": true, "user": map[string]any{"$type": "User", "login": yt.Login, "fullName": yt.FullName}},
		map[string]any{"$type": "IssueWatcher", "isStarred": false, "user": map[string]any{"$type": "User", "login": "jroe", "fullName": "Jane Roe"}},
	}}
	is["votes"] = 2
	is["voters"] = map[string]any{"$type": "IssueVoters", "hasVote": false}
}
