package adapter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter/customfields"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// PageSize is the $top the adapter asks for when paging a collection.
const PageSize = 100

// Project is the project an issue belongs to.
type Project struct {
	ShortName string
	Name      string
}

// Issue is a YouTrack issue. Only the attributes asked for are filled in.
type Issue struct {
	ID            string
	IDReadable    string
	Summary       string
	Description   string
	Project       *Project
	Reporter      *User
	Created       time.Time
	Updated       time.Time
	Resolved      *time.Time
	Tags          []string
	CustomFields  []customfields.Field
	CommentsCount int
}

// Names of the custom fields behind the state, priority and assignee
// attributes. They are YouTrack's defaults; projects may rename them.
const (
	StateField    = "State"
	PriorityField = "Priority"
	AssigneeField = "Assignee"
)

// CustomField returns the custom field called name.
func (i Issue) CustomField(name string) (customfields.Field, bool) {
	for _, f := range i.CustomFields {
		if f.Name == name {
			return f, true
		}
	}
	return customfields.Field{}, false
}

// State is the value of the State field, or of the first state-kind field
// when no field is called State.
func (i Issue) State() string {
	if f, ok := i.CustomField(StateField); ok {
		return f.String()
	}
	for _, f := range i.CustomFields {
		if f.Kind == customfields.KindState {
			return f.String()
		}
	}
	return ""
}

// Priority is the value of the Priority field.
func (i Issue) Priority() string {
	f, _ := i.CustomField(PriorityField)
	return f.String()
}

// Assignee is the user in the Assignee field, nil when unassigned.
func (i Issue) Assignee() *User {
	f, ok := i.CustomField(AssigneeField)
	if !ok || f.Multi {
		return nil
	}
	if u, ok := f.Value.(customfields.User); ok {
		return &u
	}
	return nil
}

// IssueFields are the attribute names an Issue can be requested with. They
// are ytrack's domain names; IssueProjection maps them to YouTrack's.
var IssueFields = []string{
	"id", "idReadable", "summary", "description", "project", "state", "priority",
	"assignee", "reporter", "created", "updated", "resolved", "tags", "url",
	"customFields", "commentsCount",
}

var issueWireFields = map[string]string{
	"id":            "id",
	"idReadable":    "idReadable",
	"url":           "idReadable",
	"summary":       "summary",
	"description":   "description",
	"project":       "project(shortName,name)",
	"reporter":      "reporter(login,fullName)",
	"created":       "created",
	"updated":       "updated",
	"resolved":      "resolved",
	"tags":          "tags(name)",
	"commentsCount": "commentsCount",
}

// customFieldAttrs maps the attributes backed by a custom field to its name.
var customFieldAttrs = map[string]string{
	"state":    StateField,
	"priority": PriorityField,
	"assignee": AssigneeField,
}

const (
	allCustomFields   = "customFields(name," + customfields.ValueProjection + ")"
	namedCustomFields = "customFields(name,value(name,login,fullName))"
)

// IssueProjection returns the YouTrack fields= projection for the given
// attribute names, and the custom field names to restrict customFields to
// (nil when every custom field is wanted or none is). Unknown names panic:
// commands validate them first.
func IssueProjection(attrs []string) (fields string, customFieldNames []string) {
	var parts []string
	add := func(p string) {
		if !slices.Contains(parts, p) {
			parts = append(parts, p)
		}
	}
	all := slices.Contains(attrs, "customFields")
	for _, name := range IssueFields {
		if !slices.Contains(attrs, name) {
			continue
		}
		if wire, ok := issueWireFields[name]; ok {
			add(wire)
		} else if cf, ok := customFieldAttrs[name]; ok && !all {
			customFieldNames = append(customFieldNames, cf)
		}
	}
	for _, a := range attrs {
		if !slices.Contains(IssueFields, a) {
			panic(fmt.Sprintf("adapter: unknown issue attribute %q", a))
		}
	}
	switch {
	case all:
		add(allCustomFields)
	case len(customFieldNames) > 0:
		add(namedCustomFields)
	}
	return strings.Join(parts, ","), customFieldNames
}

type wireUser struct {
	Login    string `json:"login"`
	FullName string `json:"fullName"`
}

func (u *wireUser) domain() *User {
	if u == nil {
		return nil
	}
	return &User{Login: u.Login, FullName: u.FullName}
}

type wireIssue struct {
	ID          string `json:"id"`
	IDReadable  string `json:"idReadable"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Project     *struct {
		ShortName string `json:"shortName"`
		Name      string `json:"name"`
	} `json:"project"`
	Reporter *wireUser `json:"reporter"`
	Created  *int64    `json:"created"`
	Updated  *int64    `json:"updated"`
	Resolved *int64    `json:"resolved"`
	Tags     []struct {
		Name string `json:"name"`
	} `json:"tags"`
	CustomFields  []customfields.Field `json:"customFields"`
	CommentsCount int                  `json:"commentsCount"`
}

func (w wireIssue) domain() Issue {
	i := Issue{
		ID: w.ID, IDReadable: w.IDReadable, Summary: w.Summary, Description: w.Description,
		Reporter: w.Reporter.domain(), Created: millis(w.Created), Updated: millis(w.Updated),
		CustomFields: w.CustomFields, CommentsCount: w.CommentsCount,
	}
	if w.Project != nil {
		i.Project = &Project{ShortName: w.Project.ShortName, Name: w.Project.Name}
	}
	if w.Resolved != nil {
		t := millis(w.Resolved)
		i.Resolved = &t
	}
	for _, t := range w.Tags {
		i.Tags = append(i.Tags, t.Name)
	}
	return i
}

func millis(ms *int64) time.Time {
	if ms == nil {
		return time.Time{}
	}
	return time.UnixMilli(*ms).UTC()
}

// IssueListOptions select the issues ListIssues returns.
type IssueListOptions struct {
	// Query is a YouTrack search query, sent as is.
	Query string
	// Attrs are the IssueFields to fetch.
	Attrs []string
	// Limit caps the number of issues; 0 fetches all.
	Limit int
}

// ListIssues returns the issues matching opts.Query, paging with $skip/$top.
func ListIssues(ctx context.Context, c *transport.Client, opts IssueListOptions) ([]Issue, error) {
	fields, cfNames := IssueProjection(opts.Attrs)
	base := "fields=" + fields
	if opts.Query != "" {
		base += "&query=" + escape(opts.Query)
	}
	for _, n := range cfNames {
		base += "&customFields=" + escape(n)
	}
	wire, err := transport.Paginate(0, PageSize, opts.Limit, func(skip, top int) ([]wireIssue, error) {
		var page []wireIssue
		err := c.GetJSON(ctx, fmt.Sprintf("/api/issues?%s&$skip=%d&$top=%d", base, skip, top), &page)
		return page, err
	})
	if err != nil {
		return nil, err
	}
	issues := make([]Issue, len(wire))
	for i, w := range wire {
		issues[i] = w.domain()
	}
	return issues, nil
}

// GetIssue returns one issue by its readable ID (APP-123) or database ID.
func GetIssue(ctx context.Context, c *transport.Client, id string, attrs []string) (Issue, error) {
	fields, _ := IssueProjection(attrs)
	var w wireIssue
	err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(id)+"?fields="+fields, &w)
	return w.domain(), err
}

// Comment is an issue comment.
type Comment struct {
	ID      string
	Text    string
	Author  *User
	Created time.Time
	Updated time.Time
}

const commentFields = "id,text,author(login,fullName),created,updated"

type wireComment struct {
	ID      string    `json:"id"`
	Text    string    `json:"text"`
	Author  *wireUser `json:"author"`
	Created *int64    `json:"created"`
	Updated *int64    `json:"updated"`
}

// ListComments returns an issue's comments, oldest first, starting at skip;
// limit 0 returns all of them.
func ListComments(ctx context.Context, c *transport.Client, issueID string, skip, limit int) ([]Comment, error) {
	wire, err := transport.Paginate(skip, PageSize, limit, func(skip, top int) ([]wireComment, error) {
		var page []wireComment
		err := c.GetJSON(ctx, fmt.Sprintf("/api/issues/%s/comments?fields=%s&$skip=%d&$top=%d", url.PathEscape(issueID), commentFields, skip, top), &page)
		return page, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Comment, len(wire))
	for i, w := range wire {
		out[i] = Comment{ID: w.ID, Text: w.Text, Author: w.Author.domain(), Created: millis(w.Created), Updated: millis(w.Updated)}
	}
	return out, nil
}

// NewComment is a comment AddComment created.
type NewComment struct {
	ID string
	// IssueIDReadable is the readable ID of the commented issue, also when it
	// was addressed by database ID.
	IssueIDReadable string
}

// AddComment posts text as a new comment on the issue.
func AddComment(ctx context.Context, c *transport.Client, issueID, text string) (NewComment, error) {
	var w struct {
		ID    string `json:"id"`
		Issue struct {
			IDReadable string `json:"idReadable"`
		} `json:"issue"`
	}
	path := "/api/issues/" + url.PathEscape(issueID) + "/comments?fields=id,issue(idReadable)"
	err := c.SendJSON(ctx, http.MethodPost, path, map[string]string{"text": text}, &w)
	return NewComment{ID: w.ID, IssueIDReadable: w.Issue.IDReadable}, err
}

// escape encodes a query parameter value, spaces as %20.
func escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
