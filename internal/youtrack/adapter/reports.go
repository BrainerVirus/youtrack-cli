package adapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// WorkItemQuery selects work items across issues (GET /api/workItems).
type WorkItemQuery struct {
	// Author is a login; "" means every author.
	Author string
	// Since and Until bound the work item date, both days inclusive; zero
	// means unbounded.
	Since, Until time.Time
	// Query is a YouTrack issue search query the items' issues match.
	Query string
}

func (q WorkItemQuery) params() string {
	v := url.Values{}
	if q.Author != "" {
		v.Set("author", q.Author)
	}
	if !q.Since.IsZero() {
		v.Set("startDate", q.Since.UTC().Format(time.DateOnly))
	}
	if !q.Until.IsZero() {
		v.Set("endDate", q.Until.UTC().Format(time.DateOnly))
	}
	if q.Query != "" {
		v.Set("query", q.Query)
	}
	return v.Encode()
}

// SearchWorkItems returns the work items q selects, across issues, in
// YouTrack's order.
func SearchWorkItems(ctx context.Context, c *transport.Client, q WorkItemQuery) ([]WorkItem, error) {
	base := "/api/workItems?fields=" + workItemFields
	if p := q.params(); p != "" {
		base += "&" + p
	}
	wire, err := transport.Paginate(0, PageSize, 0, func(skip, top int) ([]wireWorkItem, error) {
		var page []wireWorkItem
		err := c.GetJSON(ctx, fmt.Sprintf("%s&$skip=%d&$top=%d", base, skip, top), &page)
		return page, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]WorkItem, len(wire))
	for i, w := range wire {
		out[i] = w.domain()
	}
	return out, nil
}

// Watcher is a user who watches an issue.
type Watcher struct {
	User User
	// Starred is true when the user watches the issue by its star, not
	// through a tag or saved search.
	Starred bool
}

// IssueWatchers are an issue's watchers and whether the current user
// starred it.
type IssueWatchers struct {
	IssueIDReadable string
	HasStar         bool
	Watchers        []Watcher
}

// GetWatchers returns the users who watch an issue.
func GetWatchers(ctx context.Context, c *transport.Client, issueID string) (IssueWatchers, error) {
	var w struct {
		IDReadable string `json:"idReadable"`
		Watchers   *struct {
			HasStar       bool `json:"hasStar"`
			IssueWatchers []struct {
				User      *wireUser `json:"user"`
				IsStarred bool      `json:"isStarred"`
			} `json:"issueWatchers"`
		} `json:"watchers"`
	}
	path := "/api/issues/" + url.PathEscape(issueID) + "?fields=idReadable,watchers(hasStar,issueWatchers(user(login,fullName),isStarred))"
	if err := c.GetJSON(ctx, path, &w); err != nil {
		return IssueWatchers{}, err
	}
	out := IssueWatchers{IssueIDReadable: w.IDReadable}
	if w.Watchers != nil {
		out.HasStar = w.Watchers.HasStar
		for _, iw := range w.Watchers.IssueWatchers {
			if u := iw.User.domain(); u != nil {
				out.Watchers = append(out.Watchers, Watcher{User: *u, Starred: iw.IsStarred})
			}
		}
	}
	return out, nil
}

// SetStar stars an issue for the current user (on) or removes the star by
// writing its watchers' hasStar. Unlike the star command, it does not
// depend on the instance's language.
func SetStar(ctx context.Context, c *transport.Client, issueID string, on bool) error {
	body := map[string]any{"watchers": map[string]bool{"hasStar": on}}
	return c.SendJSON(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(issueID)+"?fields=id", body, nil)
}

// IssueVotes is an issue's vote count and whether the current user voted.
type IssueVotes struct {
	IssueIDReadable string
	Votes           int
	HasVote         bool
}

// GetVotes returns an issue's votes.
func GetVotes(ctx context.Context, c *transport.Client, issueID string) (IssueVotes, error) {
	var w struct {
		IDReadable string `json:"idReadable"`
		Votes      int    `json:"votes"`
		Voters     *struct {
			HasVote bool `json:"hasVote"`
		} `json:"voters"`
	}
	if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"?fields=idReadable,votes,voters(hasVote)", &w); err != nil {
		return IssueVotes{}, err
	}
	v := IssueVotes{IssueIDReadable: w.IDReadable, Votes: w.Votes}
	if w.Voters != nil {
		v.HasVote = w.Voters.HasVote
	}
	return v, nil
}

// SavedQuery is a saved search.
type SavedQuery struct {
	ID    string
	Name  string
	Query string
	// Owner is the owner's login; "" when unknown.
	Owner string
}

// ListSavedQueries returns the saved searches the user can see.
func ListSavedQueries(ctx context.Context, c *transport.Client) ([]SavedQuery, error) {
	type wire struct {
		ID    string    `json:"id"`
		Name  string    `json:"name"`
		Query string    `json:"query"`
		Owner *wireUser `json:"owner"`
	}
	ws, err := transport.Paginate(0, PageSize, 0, func(skip, top int) ([]wire, error) {
		var page []wire
		err := c.GetJSON(ctx, fmt.Sprintf("/api/savedQueries?fields=id,name,query,owner(login)&$skip=%d&$top=%d", skip, top), &page)
		return page, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]SavedQuery, len(ws))
	for i, w := range ws {
		out[i] = SavedQuery{ID: w.ID, Name: w.Name, Query: w.Query}
		if w.Owner != nil {
			out[i].Owner = w.Owner.Login
		}
	}
	return out, nil
}

// Activity is one change in an issue's history.
type Activity struct {
	ID        string
	Timestamp time.Time
	Author    *User
	// Category is YouTrack's category ID, e.g. CustomFieldCategory.
	Category string
	// Field names what changed: a custom field, "summary", "comments"...
	Field string
	// Added and Removed are the values as text, each entity by its name,
	// login, readable ID, presentation or text.
	Added, Removed []string
}

// activityValueFields name the attributes that identify an activity value,
// whatever entity it is: a bundle element or tag (name), a user (login), an
// issue (idReadable), a period (presentation), a comment or text (text).
const activityValueFields = "name,login,idReadable,presentation,text"

const activityFields = "id,timestamp,author(login,fullName),category(id),field(name)," +
	"added(" + activityValueFields + "),removed(" + activityValueFields + ")"

type wireActivity struct {
	ID        string    `json:"id"`
	Timestamp *int64    `json:"timestamp"`
	Author    *wireUser `json:"author"`
	Category  *struct {
		ID string `json:"id"`
	} `json:"category"`
	Field *struct {
		Name string `json:"name"`
	} `json:"field"`
	Added   any `json:"added"`
	Removed any `json:"removed"`
}

// ActivityQuery selects an issue's activities.
type ActivityQuery struct {
	// Categories are YouTrack category IDs; at least one is required.
	Categories []string
	// Limit caps the number of activities; 0 fetches all.
	Limit int
}

// ListActivities returns an issue's activities, oldest first.
func ListActivities(ctx context.Context, c *transport.Client, issueID string, q ActivityQuery) ([]Activity, error) {
	base := "/api/issues/" + url.PathEscape(issueID) + "/activities?fields=" + activityFields +
		"&categories=" + url.QueryEscape(strings.Join(q.Categories, ","))
	ws, err := transport.Paginate(0, PageSize, q.Limit, func(skip, top int) ([]wireActivity, error) {
		var page []wireActivity
		err := c.GetJSON(ctx, fmt.Sprintf("%s&$skip=%d&$top=%d", base, skip, top), &page)
		return page, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Activity, len(ws))
	for i, w := range ws {
		a := Activity{ID: w.ID, Timestamp: millis(w.Timestamp), Author: w.Author.domain(), Added: valueTexts(w.Added), Removed: valueTexts(w.Removed)}
		if w.Category != nil {
			a.Category = w.Category.ID
		}
		if w.Field != nil {
			a.Field = w.Field.Name
		}
		out[i] = a
	}
	return out, nil
}

// valueTexts renders an activity's added or removed value as text: a scalar
// as itself, an entity by the first identifying attribute it has, a list
// item by item.
func valueTexts(v any) []string {
	switch t := v.(type) {
	case nil:
		return []string{}
	case []any:
		out := []string{}
		for _, e := range t {
			out = append(out, valueTexts(e)...)
		}
		return out
	case map[string]any:
		for _, k := range strings.Split(activityValueFields, ",") {
			if s, ok := t[k].(string); ok && s != "" {
				return []string{s}
			}
		}
		if ty, ok := t["$type"].(string); ok {
			return []string{ty}
		}
		return []string{}
	case string:
		return []string{t}
	case float64:
		return []string{fmt.Sprint(int64(t))}
	default:
		return []string{fmt.Sprint(t)}
	}
}

// ErrCountPending is returned when YouTrack is still counting when
// CountIssues gives up.
var ErrCountPending = errors.New("YouTrack is still counting the issues; try again in a moment")

// CountPoll paces CountIssues: it asks again after Interval, growing by half
// each time up to MaxInterval, until Timeout has passed.
type CountPoll struct {
	Interval, MaxInterval, Timeout time.Duration
}

// CountIssues returns the number of issues that match query. YouTrack
// answers -1 while it is still counting, so it asks again as p says.
func CountIssues(ctx context.Context, c *transport.Client, query string, p CountPoll) (int, error) {
	deadline := time.Now().Add(p.Timeout)
	wait := p.Interval
	for {
		var w struct {
			Count int `json:"count"`
		}
		if err := c.SendJSON(ctx, http.MethodPost, "/api/issuesGetter/count?fields=count", map[string]any{"query": query}, &w); err != nil {
			return 0, err
		}
		if w.Count >= 0 {
			return w.Count, nil
		}
		if time.Now().Add(wait).After(deadline) {
			return 0, ErrCountPending
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait+wait/2, p.MaxInterval)
	}
}

// Suggestion is a completion YouTrack offers for a search query or a
// command.
type Suggestion struct {
	Option      string `json:"option"`
	Description string `json:"description"`
	Prefix      string `json:"prefix"`
	Suffix      string `json:"suffix"`
	// CompletionStart and CompletionEnd are the part of the text the
	// suggestion replaces, in UTF-16 code units as YouTrack counts them.
	CompletionStart int `json:"completionStart"`
	CompletionEnd   int `json:"completionEnd"`
}

// Apply returns text with the suggestion applied.
func (s Suggestion) Apply(text string) string {
	u := utf16.Encode([]rune(text))
	start, end := max(0, min(s.CompletionStart, len(u))), max(0, min(s.CompletionEnd, len(u)))
	if end < start {
		end = start
	}
	return string(utf16.Decode(u[:start])) + s.Prefix + s.Option + s.Suffix + string(utf16.Decode(u[end:]))
}

const suggestionFields = "suggestions(option,description,prefix,suffix,completionStart,completionEnd)"

// caret is the end of text as YouTrack counts positions (UTF-16 units).
func caret(text string) int { return len(utf16.Encode([]rune(text))) }

// AssistSearch returns YouTrack's completions for a search query, with the
// caret at its end.
func AssistSearch(ctx context.Context, c *transport.Client, query string) ([]Suggestion, error) {
	var w struct {
		Suggestions []Suggestion `json:"suggestions"`
	}
	err := c.SendJSON(ctx, http.MethodPost, "/api/search/assist?fields="+suggestionFields, map[string]any{"query": query, "caret": caret(query)}, &w)
	return w.Suggestions, err
}

// AssistCommand asks YouTrack how it parses a command for its issues and
// what it suggests at the end of it, without applying it.
func AssistCommand(ctx context.Context, c *transport.Client, cmd Command) ([]ParsedCommand, []Suggestion, error) {
	var w struct {
		Commands    []ParsedCommand `json:"commands"`
		Suggestions []Suggestion    `json:"suggestions"`
	}
	err := c.SendJSON(ctx, http.MethodPost, "/api/commands/assist?fields="+commandFields+","+suggestionFields, cmd.body(true), &w)
	return w.Commands, w.Suggestions, err
}
