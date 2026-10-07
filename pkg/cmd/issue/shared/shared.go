// Package shared holds what the issue commands have in common: issue
// arguments, the --json field contract and issue URLs.
package shared

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// IssueFields are the --json fields of an issue.
var IssueFields = []string{
	"id", "idReadable", "summary", "description", "project", "state", "priority",
	"assignee", "reporter", "created", "updated", "resolved", "tags", "url", "customFields",
}

// IssueFieldsHelp documents IssueFields for command help.
const IssueFieldsHelp = `  id            database ID, e.g. 2-1234
  idReadable    readable ID, e.g. APP-123
  summary       title
  description   description source text
  project       {shortName, name}
  state         value of the State field
  priority      value of the Priority field
  assignee      {login, fullName} from the Assignee field, or null
  reporter      {login, fullName}
  created       creation time (RFC 3339, UTC)
  updated       last update time (RFC 3339, UTC)
  resolved      resolution time, or null when unresolved
  tags          tag names
  url           issue web URL
  customFields  [{name, kind, value}]: kind is enum, state, user, group,
                version, build, owned, period, date, text, simple or unknown;
                value is null, a value, or an array for multi-value fields`

var idPattern = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9_]*-[0-9]+|[0-9]+-[0-9]+)$`)

// Ref is an issue named on the command line.
type Ref struct {
	ID string
	// ServiceURL is set when the issue was given as a URL.
	ServiceURL string
}

// ParseRef accepts a readable ID (APP-123), a database ID (2-1234) or an
// issue URL such as https://acme.youtrack.cloud/issue/APP-123/some-slug.
func ParseRef(arg string) (Ref, error) {
	if !strings.Contains(arg, "://") {
		if !idPattern.MatchString(arg) {
			return Ref{}, clierr.FlagErrorf("invalid issue %q: expected an ID such as APP-123 or an issue URL", arg)
		}
		return Ref{ID: arg}, nil
	}
	u, err := url.Parse(arg)
	if err != nil || u.Host == "" {
		return Ref{}, clierr.FlagErrorf("invalid issue URL %q", arg)
	}
	prefix, rest, ok := strings.Cut(u.Path, "/issue/")
	id, _, _ := strings.Cut(rest, "/")
	if !ok || !idPattern.MatchString(id) {
		return Ref{}, clierr.FlagErrorf("invalid issue URL %q: expected .../issue/APP-123", arg)
	}
	return Ref{ID: id, ServiceURL: u.Scheme + "://" + u.Host + prefix}, nil
}

// UseRefHost points f at the host of an issue URL. An explicit --host for a
// different host is a usage error.
func UseRefHost(f *cmdutil.Factory, ref Ref) error {
	if ref.ServiceURL == "" {
		return nil
	}
	want, err := hosts.Parse(ref.ServiceURL)
	if err != nil {
		return clierr.FlagErrorWrap(err)
	}
	if f.HostFlag != "" {
		have, err := hosts.Parse(f.HostFlag)
		if err == nil && have.Key != want.Key {
			return clierr.FlagErrorf("the issue URL is on %s but --host is %s", want.Key, have.Key)
		}
	}
	f.HostFlag = ref.ServiceURL
	return nil
}

// IssueURL is the web URL of an issue.
func IssueURL(host hosts.Host, id string) string {
	return host.URL + "/issue/" + url.PathEscape(id)
}

// CommentURL is the web URL of a comment.
func CommentURL(host hosts.Host, issueID, commentID string) string {
	return IssueURL(host, issueID) + "#focus=Comments-" + commentID + ".0-0"
}

// NotFound turns a 404 for issue id into a clear message.
func NotFound(err error, id string, host hosts.Host) error {
	if apiErr, ok := errors.AsType[*transport.APIError](err); ok && apiErr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("issue %s not found on %s (or you cannot see it)", id, host.Key)
	}
	return err
}

// Attrs returns the adapter attributes needed for the requested --json
// fields plus extra.
func Attrs(jsonFields []string, extra ...string) []string {
	out := slices.Clone(extra)
	for _, f := range jsonFields {
		if slices.Contains(adapter.IssueFields, f) && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// Issue exports an adapter.Issue under the --json field names.
type Issue struct {
	adapter.Issue
	Host     hosts.Host
	Comments []adapter.Comment
}

func (i Issue) ExportData(fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = i.field(f)
	}
	return out
}

func (i Issue) field(name string) any {
	switch name {
	case "id":
		return i.ID
	case "idReadable":
		return i.IDReadable
	case "summary":
		return i.Summary
	case "description":
		return i.Description
	case "project":
		if i.Project == nil {
			return nil
		}
		return map[string]any{"shortName": i.Project.ShortName, "name": i.Project.Name}
	case "state":
		return i.State()
	case "priority":
		return i.Priority()
	case "assignee":
		return exportUser(i.Assignee())
	case "reporter":
		return exportUser(i.Reporter)
	case "created":
		return exportTime(i.Created)
	case "updated":
		return exportTime(i.Updated)
	case "resolved":
		if i.Resolved == nil {
			return nil
		}
		return exportTime(*i.Resolved)
	case "tags":
		return nonNil(i.Tags)
	case "url":
		return IssueURL(i.Host, i.IDReadable)
	case "customFields":
		cfs := make([]map[string]any, len(i.CustomFields))
		for n, cf := range i.CustomFields {
			cfs[n] = map[string]any{"name": cf.Name, "kind": cf.Kind, "value": cf.Export()}
		}
		return cfs
	case "comments":
		cs := make([]map[string]any, len(i.Comments))
		for n, c := range i.Comments {
			cs[n] = map[string]any{
				"id": c.ID, "text": c.Text, "author": exportUser(c.Author),
				"created": exportTime(c.Created), "updated": exportTime(c.Updated),
				"url": CommentURL(i.Host, i.IDReadable, c.ID),
			}
		}
		return cs
	}
	return nil
}

func exportUser(u *adapter.User) any {
	if u == nil {
		return nil
	}
	return u.Export()
}

func exportTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
