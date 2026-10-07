// Package shared holds what the work-item commands have in common: the
// --json field contract, work item type resolution and work dates.
package shared

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/worktime"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	issueshared "github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields are the --json fields of a work item.
var Fields = []string{"id", "date", "duration", "author", "type", "text", "issue", "url"}

// FieldsHelp documents Fields for command help.
const FieldsHelp = `  id        work item ID, e.g. 115-3
  date      day of the work, YYYY-MM-DD
  duration  {minutes, presentation}, e.g. {"minutes": 90, "presentation": "1h 30m"}
  author    {login, fullName}
  type      work item type name, or null
  text      description
  issue     readable ID of the issue
  url       issue web URL`

// DateHelp explains --date for command help.
const DateHelp = `--date auto (the default) is today in your work timezone: the timezone
setting in config.yml (an IANA zone such as Europe/Madrid) when set,
otherwise the system timezone (TZ). YouTrack stores the day only.`

var itemIDPattern = regexp.MustCompile(`^[0-9]+-[0-9]+$`)

// ParseItemID checks a work item ID argument.
func ParseItemID(arg string) (string, error) {
	if !itemIDPattern.MatchString(arg) {
		return "", clierr.FlagErrorf("invalid work item ID %q: expected an ID such as 115-3 (see `ytrack work-item list`)", arg)
	}
	return arg, nil
}

// WorkItem exports an adapter.WorkItem under the --json field names.
type WorkItem struct {
	adapter.WorkItem
	Host  hosts.Host
	Issue string
}

// New wraps item, falling back to issueID when YouTrack did not say which
// issue it belongs to.
func New(item adapter.WorkItem, host hosts.Host, issueID string) WorkItem {
	if item.IssueIDReadable != "" {
		issueID = item.IssueIDReadable
	}
	return WorkItem{WorkItem: item, Host: host, Issue: issueID}
}

func (w WorkItem) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = w.field(f)
	}
	return out
}

func (w WorkItem) field(name string) any {
	switch name {
	case "id":
		return w.ID
	case "date":
		return w.Day()
	case "duration":
		return map[string]any{"minutes": w.Minutes, "presentation": w.Presentation()}
	case "author":
		if w.Author == nil {
			return nil
		}
		return w.Author.Export()
	case "type":
		if w.Type == "" {
			return nil
		}
		return w.Type
	case "text":
		return w.Text
	case "issue":
		return w.Issue
	case "url":
		return issueshared.IssueURL(w.Host, w.Issue)
	}
	return nil
}

// Day is the work item's date as YYYY-MM-DD, or "" when it has none.
func (w WorkItem) Day() string {
	if w.Date.IsZero() {
		return ""
	}
	return w.Date.UTC().Format(worktime.DateLayout)
}

// Presentation is YouTrack's rendering of the duration, or ytrack's own when
// YouTrack sent none.
func (w WorkItem) Presentation() string {
	if w.Duration != "" {
		return w.Duration
	}
	return worktime.FormatMinutes(w.Minutes)
}

// WorkDate resolves a --date value with the configured work timezone.
func WorkDate(f *cmdutil.Factory, raw string) (time.Time, error) {
	now := f.Clock()
	if raw != "" && raw != "auto" {
		d, err := worktime.WorkDate(raw, now, time.UTC)
		if err != nil {
			return time.Time{}, clierr.FlagErrorWrap(err)
		}
		return d, nil
	}
	cfg, err := f.Config()
	if err != nil {
		return time.Time{}, err
	}
	loc, err := worktime.Location(cfg.Timezone(), now.Location())
	if err != nil {
		return time.Time{}, err
	}
	return worktime.WorkDate(raw, now, loc)
}

// IssueProject reads the named issue's readable ID and project.
func IssueProject(ctx context.Context, c *transport.Client, host hosts.Host, issueID string) (adapter.IssueProject, error) {
	p, err := adapter.GetIssueProject(ctx, c, issueID)
	if err != nil {
		return adapter.IssueProject{}, issueshared.NotFound(err, issueID, host)
	}
	return p, nil
}

// ItemOnIssue reads a work item and checks that it belongs to issue, the
// issue named on the command line as YouTrack resolved it (so a database
// ID or an old project alias still matches). Work item IDs are global, so
// edit and delete must not trust the issue in the URL to scope them.
func ItemOnIssue(ctx context.Context, c *transport.Client, host hosts.Host, issue adapter.IssueProject, named, itemID string) (adapter.WorkItem, error) {
	item, err := adapter.GetWorkItem(ctx, c, named, itemID)
	if err != nil {
		return adapter.WorkItem{}, NotFound(err, named, itemID, host)
	}
	if item.IssueIDReadable == "" || item.IssueIDReadable != issue.IssueIDReadable {
		owner := item.IssueIDReadable
		if owner == "" {
			owner = "an unknown issue"
		}
		return adapter.WorkItem{}, fmt.Errorf("work item %s belongs to %s, not %s; nothing was changed", itemID, owner, issue.IssueIDReadable)
	}
	return item, nil
}

// ResolveType finds the ID of the work item type called name (ignoring case)
// in the time tracking settings of the issue's project p.
func ResolveType(ctx context.Context, c *transport.Client, p adapter.IssueProject, name string) (string, error) {
	project := p.ShortName
	if project == "" {
		project = p.ProjectID
	}
	s, err := adapter.GetTimeTrackingSettings(ctx, c, p.ProjectID)
	if err != nil {
		if apiErr, ok := errors.AsType[*transport.APIError](err); ok && apiErr.StatusCode == http.StatusForbidden {
			return "", fmt.Errorf("cannot read the work item types of project %s: %w", project, err)
		}
		return "", err
	}
	if !s.Enabled {
		return "", fmt.Errorf("time tracking is not enabled in project %s", project)
	}
	var folded string
	names := make([]string, 0, len(s.Types))
	for _, t := range s.Types {
		if t.Name == name {
			return t.ID, nil
		}
		if folded == "" && strings.EqualFold(t.Name, name) {
			folded = t.ID
		}
		names = append(names, t.Name)
	}
	if folded != "" {
		return folded, nil
	}
	if len(names) == 0 {
		return "", clierr.FlagErrorf("unknown work item type %q: project %s has no work item types", name, project)
	}
	return "", clierr.FlagErrorf("unknown work item type %q in project %s; valid types:\n  %s", name, project, strings.Join(names, "\n  "))
}

// NotFound turns a 404 on a work item into a clear message.
func NotFound(err error, issueID, itemID string, host hosts.Host) error {
	if apiErr, ok := errors.AsType[*transport.APIError](err); ok && apiErr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("work item %s not found on issue %s on %s (or you cannot see it)", itemID, issueID, host.Key)
	}
	return err
}
