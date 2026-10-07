package adapter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter/customfields"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// ProjectInfo is a project ytrack can create issues in.
type ProjectInfo struct {
	ID        string
	ShortName string
	Name      string
	Archived  bool
}

// ListProjects returns the projects the user can see.
func ListProjects(ctx context.Context, c *transport.Client) ([]ProjectInfo, error) {
	type wire struct {
		ID        string `json:"id"`
		ShortName string `json:"shortName"`
		Name      string `json:"name"`
		Archived  bool   `json:"archived"`
	}
	ws, err := transport.Paginate(0, PageSize, 0, func(skip, top int) ([]wire, error) {
		var page []wire
		err := c.GetJSON(ctx, fmt.Sprintf("/api/admin/projects?fields=id,shortName,name,archived&$skip=%d&$top=%d", skip, top), &page)
		return page, err
	})
	out := make([]ProjectInfo, len(ws))
	for i, w := range ws {
		out[i] = ProjectInfo(w)
	}
	return out, err
}

// ProjectFields returns the custom field definitions of a project, by
// database ID.
func ProjectFields(ctx context.Context, c *transport.Client, projectID string) ([]customfields.Definition, error) {
	return transport.Paginate(0, PageSize, 0, func(skip, top int) ([]customfields.Definition, error) {
		var page []customfields.Definition
		err := c.GetJSON(ctx, fmt.Sprintf("/api/admin/projects/%s/customFields?fields=%s&$skip=%d&$top=%d",
			url.PathEscape(projectID), customfields.DefinitionProjection, skip, top), &page)
		return page, err
	})
}

// Tag is an issue tag.
type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListTags returns the tags the user can see.
func ListTags(ctx context.Context, c *transport.Client) ([]Tag, error) {
	return transport.Paginate(0, PageSize, 0, func(skip, top int) ([]Tag, error) {
		var page []Tag
		err := c.GetJSON(ctx, fmt.Sprintf("/api/tags?fields=id,name&$skip=%d&$top=%d", skip, top), &page)
		return page, err
	})
}

// AddIssueTag tags an issue.
func AddIssueTag(ctx context.Context, c *transport.Client, issueID, tagID string) error {
	return c.SendJSON(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(issueID)+"/tags?fields=id", map[string]string{"id": tagID}, nil)
}

// RemoveIssueTag removes a tag from an issue.
func RemoveIssueTag(ctx context.Context, c *transport.Client, issueID, tagID string) error {
	return c.Delete(ctx, "/api/issues/"+url.PathEscape(issueID)+"/tags/"+url.PathEscape(tagID))
}

// IssueChange is what CreateIssue or UpdateIssue writes. Nil and empty
// members are not sent, so an update changes only what is set.
type IssueChange struct {
	// ProjectID is the project's database ID; only CreateIssue sends it.
	ProjectID   string
	Summary     *string
	Description *string
	// CustomFields are IssueCustomField values from customfields.Encode.
	CustomFields []map[string]any
	// TagIDs tag a new issue; only CreateIssue sends them.
	TagIDs []string
}

// Empty reports whether an update would send nothing.
func (ch IssueChange) Empty() bool {
	return ch.Summary == nil && ch.Description == nil && len(ch.CustomFields) == 0
}

func (ch IssueChange) body(create bool) map[string]any {
	b := map[string]any{}
	if create {
		b["project"] = map[string]any{"id": ch.ProjectID}
		if len(ch.TagIDs) > 0 {
			tags := make([]map[string]any, len(ch.TagIDs))
			for i, id := range ch.TagIDs {
				tags[i] = map[string]any{"id": id}
			}
			b["tags"] = tags
		}
	}
	if ch.Summary != nil {
		b["summary"] = *ch.Summary
	}
	if ch.Description != nil {
		b["description"] = *ch.Description
	}
	if len(ch.CustomFields) > 0 {
		b["customFields"] = ch.CustomFields
	}
	return b
}

// CreateIssue creates an issue and returns it with attrs (IssueFields).
func CreateIssue(ctx context.Context, c *transport.Client, ch IssueChange, attrs []string) (Issue, error) {
	fields, _ := IssueProjection(attrs)
	var w wireIssue
	err := c.SendJSON(ctx, http.MethodPost, "/api/issues?fields="+fields, ch.body(true), &w)
	return w.domain(), err
}

// UpdateIssue changes only the attributes set in ch and returns the issue
// with attrs.
func UpdateIssue(ctx context.Context, c *transport.Client, id string, ch IssueChange, attrs []string) (Issue, error) {
	fields, _ := IssueProjection(attrs)
	var w wireIssue
	err := c.SendJSON(ctx, http.MethodPost, "/api/issues/"+url.PathEscape(id)+"?fields="+fields, ch.body(false), &w)
	return w.domain(), err
}

// Command is a YouTrack command applied to issues.
type Command struct {
	Query    string
	IssueIDs []string
	// Comment is added to the issues along with the command.
	Comment string
	// Silent suppresses the notifications the change would send.
	Silent bool
}

// ParsedCommand is one part of a command as YouTrack understood it.
type ParsedCommand struct {
	Description string `json:"description"`
	Error       bool   `json:"error"`
	Delete      bool   `json:"delete"`
}

const commandFields = "commands(description,error,delete)"

func (cmd Command) body(preview bool) map[string]any {
	issues := make([]map[string]string, len(cmd.IssueIDs))
	for i, id := range cmd.IssueIDs {
		if databaseID(id) {
			issues[i] = map[string]string{"id": id}
		} else {
			issues[i] = map[string]string{"idReadable": id}
		}
	}
	b := map[string]any{"query": cmd.Query, "issues": issues}
	if preview {
		b["caret"] = len(cmd.Query)
		return b
	}
	if cmd.Comment != "" {
		b["comment"] = cmd.Comment
	}
	if cmd.Silent {
		b["silent"] = true
	}
	return b
}

// databaseID reports whether id is a database ID (2-1234), not APP-123.
func databaseID(id string) bool {
	head, _, ok := strings.Cut(id, "-")
	return ok && head != "" && strings.Trim(head, "0123456789") == ""
}

// ApplyCommand runs a command on its issues and returns how YouTrack parsed it.
func ApplyCommand(ctx context.Context, c *transport.Client, cmd Command) ([]ParsedCommand, error) {
	var w struct {
		Commands []ParsedCommand `json:"commands"`
	}
	err := c.SendJSON(ctx, http.MethodPost, "/api/commands?fields="+commandFields, cmd.body(false), &w)
	return w.Commands, err
}

// PreviewCommand asks YouTrack how it would parse a command for its issues,
// without applying it (the command assist endpoint).
func PreviewCommand(ctx context.Context, c *transport.Client, cmd Command) ([]ParsedCommand, error) {
	var w struct {
		Commands []ParsedCommand `json:"commands"`
	}
	err := c.SendJSON(ctx, http.MethodPost, "/api/commands/assist?fields="+commandFields, cmd.body(true), &w)
	return w.Commands, err
}
