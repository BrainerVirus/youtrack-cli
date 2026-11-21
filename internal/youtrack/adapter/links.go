package adapter

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// LinkType is an issue link type. A directed type has two phrases, one for
// each end (Depend: "is required for" and "depends on"); an undirected type
// has one (Relates: "relates to") and an empty TargetToSource. The phrases
// are the type's own, not the translations YouTrack shows in a localized UI.
type LinkType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SourceToTarget string `json:"sourceToTarget"`
	TargetToSource string `json:"targetToSource"`
	Directed       bool   `json:"directed"`
}

// Link directions, as YouTrack names them: an issue is the source of an
// OUTWARD link and the target of an INWARD one; an undirected link is BOTH.
const (
	LinkOutward = "OUTWARD"
	LinkInward  = "INWARD"
	LinkBoth    = "BOTH"
)

// LinkEnd is one end of a link type, as seen from an issue: the phrase that
// reads "<issue> <phrase> <other issue>", its direction, and the ID of the
// issue's link collection for it (the type ID, with an "s" suffix for
// OUTWARD and a "t" suffix for INWARD).
type LinkEnd struct {
	Type      LinkType
	Phrase    string
	Direction string
	LinkID    string
}

// Ends returns the ends of a link type: OUTWARD then INWARD for a directed
// type, a single BOTH for an undirected one.
func (t LinkType) Ends() []LinkEnd {
	if !t.Directed {
		return []LinkEnd{{Type: t, Phrase: t.SourceToTarget, Direction: LinkBoth, LinkID: t.ID}}
	}
	return []LinkEnd{
		{Type: t, Phrase: t.SourceToTarget, Direction: LinkOutward, LinkID: t.ID + "s"},
		{Type: t, Phrase: t.TargetToSource, Direction: LinkInward, LinkID: t.ID + "t"},
	}
}

// IssueLink is the issues linked to an issue through one end of a link
// type.
type IssueLink struct {
	LinkEnd
	// Issues are the readable IDs of the linked issues.
	Issues []string
}

const (
	linkTypeFields  = "id,name,sourceToTarget,targetToSource,directed"
	issueLinkFields = "id,direction,linkType(" + linkTypeFields + "),issues(idReadable)"
)

// ListLinkTypes returns the link types of the instance.
func ListLinkTypes(ctx context.Context, c *transport.Client) ([]LinkType, error) {
	return transport.Paginate(0, PageSize, 0, func(skip, top int) ([]LinkType, error) {
		var page []LinkType
		err := c.GetJSON(ctx, fmt.Sprintf("/api/issueLinkTypes?fields=%s&$skip=%d&$top=%d", linkTypeFields, skip, top), &page)
		return page, err
	})
}

// ListIssueLinks returns an issue's links, one per link type end, those
// with no issues included.
func ListIssueLinks(ctx context.Context, c *transport.Client, issueID string) ([]IssueLink, error) {
	type wire struct {
		ID        string   `json:"id"`
		Direction string   `json:"direction"`
		LinkType  LinkType `json:"linkType"`
		Issues    []struct {
			IDReadable string `json:"idReadable"`
		} `json:"issues"`
	}
	ws, err := transport.Paginate(0, PageSize, 0, func(skip, top int) ([]wire, error) {
		var page []wire
		err := c.GetJSON(ctx, fmt.Sprintf("/api/issues/%s/links?fields=%s&$skip=%d&$top=%d", url.PathEscape(issueID), issueLinkFields, skip, top), &page)
		return page, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]IssueLink, len(ws))
	for i, w := range ws {
		phrase := w.LinkType.SourceToTarget
		if w.Direction == LinkInward {
			phrase = w.LinkType.TargetToSource
		}
		l := IssueLink{LinkEnd: LinkEnd{Type: w.LinkType, Phrase: phrase, Direction: w.Direction, LinkID: w.ID}, Issues: []string{}}
		for _, is := range w.Issues {
			l.Issues = append(l.Issues, is.IDReadable)
		}
		out[i] = l
	}
	return out, nil
}

// FindLinkEnd finds the link type end that phrase names, ignoring case and
// surrounding space: a phrase of a directed type picks its direction, and an
// undirected type also answers to its name. A directed type's name matches
// nothing, because it does not say which way the link goes. ok is false
// when no end, or more than one, matches.
func FindLinkEnd(types []LinkType, phrase string) (end LinkEnd, ok bool) {
	phrase = strings.TrimSpace(phrase)
	var found []LinkEnd
	for _, t := range types {
		for _, e := range t.Ends() {
			if strings.EqualFold(e.Phrase, phrase) || (!t.Directed && strings.EqualFold(t.Name, phrase)) {
				found = append(found, e)
			}
		}
	}
	if len(found) != 1 {
		return LinkEnd{}, false
	}
	return found[0], true
}

// LinkedIssue is an issue on one end of an issue's link.
type LinkedIssue struct {
	ID         string `json:"id"`
	IDReadable string `json:"idReadable"`
}

// LinkedIssues returns the issues linked to an issue through the link with
// that ID (see LinkEnd.LinkID).
func LinkedIssues(ctx context.Context, c *transport.Client, issueID, linkID string) ([]LinkedIssue, error) {
	return transport.Paginate(0, PageSize, 0, func(skip, top int) ([]LinkedIssue, error) {
		var page []LinkedIssue
		err := c.GetJSON(ctx, fmt.Sprintf("%s?fields=id,idReadable&$skip=%d&$top=%d", linkIssuesPath(issueID, linkID), skip, top), &page)
		return page, err
	})
}

// AddLink links target, a readable or database issue ID, to an issue
// through the link with that ID. Adding a link that exists is a no-op.
func AddLink(ctx context.Context, c *transport.Client, issueID, linkID, target string) error {
	body := map[string]string{"idReadable": target}
	if databaseID(target) {
		body = map[string]string{"id": target}
	}
	return c.SendJSON(ctx, http.MethodPost, linkIssuesPath(issueID, linkID)+"?fields=id", body, nil)
}

// RemoveLink removes target, which must be a database issue ID (YouTrack
// answers 404 for a readable one), from the issue's link with that ID.
func RemoveLink(ctx context.Context, c *transport.Client, issueID, linkID, targetDatabaseID string) error {
	return c.Delete(ctx, linkIssuesPath(issueID, linkID)+"/"+url.PathEscape(targetDatabaseID))
}

func linkIssuesPath(issueID, linkID string) string {
	return "/api/issues/" + url.PathEscape(issueID) + "/links/" + url.PathEscape(linkID) + "/issues"
}
