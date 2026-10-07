package adapter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// WorkItem is a time entry on an issue.
type WorkItem struct {
	ID string
	// Date is the day the work was done, at midnight UTC.
	Date     time.Time
	Minutes  int
	Duration string // YouTrack's presentation, e.g. "1h 30m"
	Author   *User
	// Type is the work item type's name, "" when it has none.
	Type            string
	Text            string
	IssueIDReadable string
}

// WorkItemType is a work item type a project allows.
type WorkItemType struct {
	ID   string
	Name string
}

const workItemFields = "id,date,duration(minutes,presentation),author(login,fullName),type(id,name),text,issue(idReadable)"

type wireWorkItem struct {
	ID       string `json:"id"`
	Date     *int64 `json:"date"`
	Duration *struct {
		Minutes      int    `json:"minutes"`
		Presentation string `json:"presentation"`
	} `json:"duration"`
	Author *wireUser `json:"author"`
	Type   *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"type"`
	Text  string `json:"text"`
	Issue *struct {
		IDReadable string `json:"idReadable"`
	} `json:"issue"`
}

func (w wireWorkItem) domain() WorkItem {
	wi := WorkItem{ID: w.ID, Date: millis(w.Date), Author: w.Author.domain(), Text: w.Text}
	if w.Duration != nil {
		wi.Minutes, wi.Duration = w.Duration.Minutes, w.Duration.Presentation
	}
	if w.Type != nil {
		wi.Type = w.Type.Name
	}
	if w.Issue != nil {
		wi.IssueIDReadable = w.Issue.IDReadable
	}
	return wi
}

func workItemsPath(issueID string) string {
	return "/api/issues/" + url.PathEscape(issueID) + "/timeTracking/workItems"
}

// ListWorkItems returns all of an issue's work items, in YouTrack's order.
func ListWorkItems(ctx context.Context, c *transport.Client, issueID string) ([]WorkItem, error) {
	wire, err := transport.Paginate(0, PageSize, 0, func(skip, top int) ([]wireWorkItem, error) {
		var page []wireWorkItem
		err := c.GetJSON(ctx, fmt.Sprintf("%s?fields=%s&$skip=%d&$top=%d", workItemsPath(issueID), workItemFields, skip, top), &page)
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

// GetWorkItem returns one work item of an issue.
func GetWorkItem(ctx context.Context, c *transport.Client, issueID, itemID string) (WorkItem, error) {
	var w wireWorkItem
	err := c.GetJSON(ctx, workItemsPath(issueID)+"/"+url.PathEscape(itemID)+"?fields="+workItemFields, &w)
	return w.domain(), err
}

// WorkItemChange is the content of a new work item, or the attributes to
// change on an existing one (nil or empty means unchanged).
type WorkItemChange struct {
	Minutes int
	Date    *time.Time
	Text    *string
	TypeID  string
}

func (ch WorkItemChange) body() map[string]any {
	b := map[string]any{}
	if ch.Minutes > 0 {
		b["duration"] = map[string]any{"minutes": ch.Minutes}
	}
	if ch.Date != nil {
		b["date"] = ch.Date.UnixMilli()
	}
	if ch.Text != nil {
		b["text"] = *ch.Text
	}
	if ch.TypeID != "" {
		b["type"] = map[string]any{"id": ch.TypeID}
	}
	return b
}

// AddWorkItem creates a work item on the issue.
func AddWorkItem(ctx context.Context, c *transport.Client, issueID string, ch WorkItemChange) (WorkItem, error) {
	var w wireWorkItem
	err := c.SendJSON(ctx, http.MethodPost, workItemsPath(issueID)+"?fields="+workItemFields, ch.body(), &w)
	return w.domain(), err
}

// UpdateWorkItem changes the given attributes of a work item.
func UpdateWorkItem(ctx context.Context, c *transport.Client, issueID, itemID string, ch WorkItemChange) (WorkItem, error) {
	var w wireWorkItem
	err := c.SendJSON(ctx, http.MethodPost, workItemsPath(issueID)+"/"+url.PathEscape(itemID)+"?fields="+workItemFields, ch.body(), &w)
	return w.domain(), err
}

// DeleteWorkItem deletes a work item.
func DeleteWorkItem(ctx context.Context, c *transport.Client, issueID, itemID string) error {
	return c.Delete(ctx, workItemsPath(issueID)+"/"+url.PathEscape(itemID))
}

// IssueProject is the project an issue belongs to, with its database ID.
type IssueProject struct {
	IssueIDReadable string
	ProjectID       string
	ShortName       string
}

// GetIssueProject returns the project of an issue.
func GetIssueProject(ctx context.Context, c *transport.Client, issueID string) (IssueProject, error) {
	var w struct {
		IDReadable string `json:"idReadable"`
		Project    *struct {
			ID        string `json:"id"`
			ShortName string `json:"shortName"`
		} `json:"project"`
	}
	if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"?fields=idReadable,project(id,shortName)", &w); err != nil {
		return IssueProject{}, err
	}
	p := IssueProject{IssueIDReadable: w.IDReadable}
	if w.Project != nil {
		p.ProjectID, p.ShortName = w.Project.ID, w.Project.ShortName
	}
	return p, nil
}

// TimeTrackingSettings are a project's time tracking settings.
type TimeTrackingSettings struct {
	Enabled bool
	Types   []WorkItemType
}

// GetTimeTrackingSettings returns whether time tracking is enabled in the
// project and the work item types it allows.
func GetTimeTrackingSettings(ctx context.Context, c *transport.Client, projectID string) (TimeTrackingSettings, error) {
	var w struct {
		Enabled       bool `json:"enabled"`
		WorkItemTypes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workItemTypes"`
	}
	path := "/api/admin/projects/" + url.PathEscape(projectID) + "/timeTrackingSettings?fields=enabled,workItemTypes(id,name)"
	if err := c.GetJSON(ctx, path, &w); err != nil {
		return TimeTrackingSettings{}, err
	}
	s := TimeTrackingSettings{Enabled: w.Enabled}
	for _, t := range w.WorkItemTypes {
		s.Types = append(s.Types, WorkItemType{ID: t.ID, Name: t.Name})
	}
	return s, nil
}
