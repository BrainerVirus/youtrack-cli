package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The custom field the suite sets with --field, and its values on create
// and on edit. Every new YouTrack project has Priority with these values,
// whatever the instance's language (the values' names are not localized).
const (
	itField       = "Priority"
	itFieldCreate = "Critical"
	itFieldEdit   = "Minor"
)

// fieldsAndTags is what issue view --json customFields,tags prints.
type fieldsAndTags struct {
	Summary      string
	CustomFields []struct {
		Name  string
		Value any
	}
	Tags []string
}

// field returns the value of the custom field name, or "" without it.
func (v fieldsAndTags) field(name string) string {
	for _, f := range v.CustomFields {
		if f.Name == name && f.Value != nil {
			return fmt.Sprint(f.Value)
		}
	}
	return ""
}

// tagSteps creates two tags named after the run and finds both with tag
// list. Cleanup deletes them.
func (s *suite) tagSteps() (string, string, error) {
	var names []string
	for _, suffix := range []string{"a", "b"} {
		name := s.tagPrefix() + "-" + suffix
		var created tag
		if err := s.ytrackJSON(&created, "tag", "create", name, "--json", "id,name"); err != nil {
			return "", "", fmt.Errorf("tag create: %w", err)
		}
		if created.ID == "" || created.Name != name {
			return "", "", fmt.Errorf("tag create %s: got %+v", name, created)
		}
		s.tags = append(s.tags, created)
		names = append(names, name)
	}
	listed, err := s.listTags()
	if err != nil {
		return "", "", err
	}
	for _, name := range names {
		if !slices.ContainsFunc(listed, func(t tag) bool { return t.Name == name }) {
			return "", "", fmt.Errorf("tag list: %s missing from %+v", name, listed)
		}
	}
	return names[0], names[1], nil
}

func (s *suite) listTags() ([]tag, error) {
	var tags []tag
	if err := s.ytrackJSON(&tags, "tag", "list", "--json", "id,name"); err != nil {
		return nil, fmt.Errorf("tag list: %w", err)
	}
	return tags, nil
}

type comment struct{ ID, Text string }

// commentSteps adds a comment and finds it with issue view, lists, edits
// and deletes it, reading each change back with comment list, then posts a
// comment with an attachment.
func (s *suite) commentSteps(id string) error {
	text := s.prefix() + " comment"
	var posted comment
	if err := s.ytrackJSON(&posted, "issue", "comment", id, "--body", text, "--json", "id"); err != nil {
		return fmt.Errorf("issue comment: %w", err)
	}
	if posted.ID == "" {
		return errors.New("issue comment: no comment id in the output")
	}
	var withComments struct{ Comments []struct{ Text string } }
	if err := s.ytrackJSON(&withComments, "issue", "view", id, "--json", "comments"); err != nil {
		return fmt.Errorf("issue view comments: %w", err)
	}
	if !slices.ContainsFunc(withComments.Comments, func(c struct{ Text string }) bool { return c.Text == text }) {
		return fmt.Errorf("issue comment: %q not among the comments of %s", text, id)
	}

	listed, err := s.comments(id)
	if err != nil {
		return err
	}
	if !slices.Contains(listed, comment{posted.ID, text}) {
		return fmt.Errorf("issue comment list: %+v missing from %+v", comment{posted.ID, text}, listed)
	}

	edited := s.prefix() + " comment edited"
	var after comment
	if err := s.ytrackJSON(&after, "issue", "comment", "edit", id, posted.ID, "--body", edited, "--json", "id,text"); err != nil {
		return fmt.Errorf("issue comment edit: %w", err)
	}
	if listed, err = s.comments(id); err != nil {
		return err
	}
	if after != (comment{posted.ID, edited}) || !slices.Contains(listed, comment{posted.ID, edited}) {
		return fmt.Errorf("issue comment edit: printed %+v, listed %+v, want %s with %q", after, listed, posted.ID, edited)
	}

	if _, err := s.ytrack("issue", "comment", "delete", id, posted.ID, "--yes"); err != nil {
		return fmt.Errorf("issue comment delete: %w", err)
	}
	if listed, err = s.comments(id); err != nil {
		return err
	}
	if slices.ContainsFunc(listed, func(c comment) bool { return c.ID == posted.ID }) {
		return fmt.Errorf("issue comment delete: %s still listed on %s", posted.ID, id)
	}
	return s.commentAttachStep(id)
}

func (s *suite) comments(id string) ([]comment, error) {
	var out []comment
	if err := s.ytrackJSON(&out, "issue", "comment", "list", id, "--json", "id,text"); err != nil {
		return nil, fmt.Errorf("issue comment list: %w", err)
	}
	return out, nil
}

type attachment struct {
	ID        string
	Name      string
	CommentID *string `json:"commentId"`
}

func (s *suite) attachments(id string) ([]attachment, error) {
	var out []attachment
	if err := s.ytrackJSON(&out, "issue", "attachments", id, "--json", "id,name,commentId"); err != nil {
		return nil, fmt.Errorf("issue attachments: %w", err)
	}
	return out, nil
}

// attachmentSteps attaches two files to the issue, lists them, and
// downloads one by name: only that file arrives, a second download refuses
// to overwrite it, and --clobber overwrites it.
func (s *suite) attachmentSteps(id string) (err error) {
	dir, err := os.MkdirTemp("", "ytrack-it-files-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	src, out := filepath.Join(dir, "src"), filepath.Join(dir, "out")
	if err := os.Mkdir(src, 0o700); err != nil {
		return err
	}
	names := []string{"ytrack-it-a.txt", "ytrack-it-b.txt"}
	var paths []string
	for _, name := range names {
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, []byte(name+" "+s.runID+"\n"), 0o600); err != nil {
			return err
		}
		paths = append(paths, p)
	}

	var attached []attachment
	if err := s.ytrackJSON(&attached, append(append([]string{"issue", "attach", id}, paths...), "--json", "id,name")...); err != nil {
		return fmt.Errorf("issue attach: %w", err)
	}
	if len(attached) != len(names) {
		return fmt.Errorf("issue attach: printed %+v, want %v", attached, names)
	}
	listed, err := s.attachments(id)
	if err != nil {
		return err
	}
	for _, name := range names {
		if !slices.ContainsFunc(listed, func(a attachment) bool { return a.Name == name && a.CommentID == nil }) {
			return fmt.Errorf("issue attachments: %s missing on the issue itself in %+v", name, listed)
		}
	}

	want, _ := os.ReadFile(paths[0])
	if _, err := s.ytrack("issue", "download", id, names[0], "-o", out); err != nil {
		return fmt.Errorf("issue download %s: %w", names[0], err)
	}
	if err := sameDir(out, names[0], want); err != nil {
		return fmt.Errorf("issue download %s: %w", names[0], err)
	}
	if _, err := s.ytrack("issue", "download", id, names[0], "-o", out); err == nil || !strings.Contains(err.Error(), "--clobber") {
		return fmt.Errorf("issue download %s again: want a refusal naming --clobber, got %w", names[0], err)
	}
	if err := os.WriteFile(filepath.Join(out, names[0]), []byte("stale\n"), 0o600); err != nil {
		return err
	}
	if _, err := s.ytrack("issue", "download", id, names[0], "-o", out, "--clobber"); err != nil {
		return fmt.Errorf("issue download --clobber: %w", err)
	}
	if err := sameDir(out, names[0], want); err != nil {
		return fmt.Errorf("issue download --clobber: %w", err)
	}
	return nil
}

// sameDir checks that dir holds exactly the file name, with content want.
func sameDir(dir, name string, want []byte) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, []string{name}) {
		return fmt.Errorf("%s holds %v, want only %s", dir, got, name)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	if !bytes.Equal(data, want) {
		return fmt.Errorf("%s holds %q, want %q", name, data, want)
	}
	return nil
}

// readSteps runs the read-only commands: project fields, count, the
// issue's history and watchers, saved searches and query completion.
func (s *suite) readSteps(id string) error {
	var errs []error
	var fields []struct{ Name string }
	if err := s.ytrackJSON(&fields, "project", "fields", s.cfg.Project, "--json", "name"); err != nil {
		errs = append(errs, fmt.Errorf("project fields: %w", err))
	} else if !slices.Contains(fields, struct{ Name string }{itField}) {
		errs = append(errs, fmt.Errorf("project fields %s: no %s in %+v", s.cfg.Project, itField, fields))
	}

	var count struct{ Count int }
	if err := s.ytrackJSON(&count, "issue", "count", "project: "+s.cfg.Project, "--json", "count"); err != nil {
		errs = append(errs, fmt.Errorf("issue count: %w", err))
	} else if count.Count < 1 {
		errs = append(errs, fmt.Errorf("issue count project: %s: %d, want at least the run's issue", s.cfg.Project, count.Count))
	}

	var acts []struct{ Category string }
	if err := s.ytrackJSON(&acts, "issue", "activities", id, "--json", "category"); err != nil {
		errs = append(errs, fmt.Errorf("issue activities: %w", err))
	} else if len(acts) == 0 {
		errs = append(errs, fmt.Errorf("issue activities %s: no history, want at least its creation", id))
	}

	var watchers []struct{ Login string }
	if err := s.ytrackJSON(&watchers, "issue", "watchers", id, "--json", "login"); err != nil {
		errs = append(errs, fmt.Errorf("issue watchers: %w", err))
	}

	var queries []struct{ Name, Query string }
	if err := s.ytrackJSON(&queries, "query", "list", "--json", "name,query"); err != nil {
		errs = append(errs, fmt.Errorf("query list: %w", err))
	} else if len(queries) == 0 {
		errs = append(errs, errors.New("query list: no saved searches, want YouTrack's built-in ones"))
	}

	var options []struct{ Option string }
	if err := s.ytrackJSON(&options, "issue", "list", "-q", "project: ", "--query-assist", "--json", "option"); err != nil {
		errs = append(errs, fmt.Errorf("issue list --query-assist: %w", err))
	} else if len(options) == 0 {
		errs = append(errs, errors.New(`issue list -q "project: " --query-assist: no completions`))
	}
	return errors.Join(errs...)
}

// voteWatchSteps votes for the issue and stars it, reading each change
// back, then undoes both.
func (s *suite) voteWatchSteps(id, login string) error {
	var errs []error
	var vote struct{ Voted bool }
	out, err := s.ytrack("issue", "vote", id, "--json", "voted")
	switch {
	case err != nil && strings.Contains(err.Error(), `rejected the "vote" command`):
		// YouTrack refuses a vote on an issue you reported, and the run
		// reported this one: the refusal is the expected outcome.
		s.logf("issue vote %s: refused as your own issue, unvote not run: %v", id, err)
	case err != nil:
		errs = append(errs, fmt.Errorf("issue vote: %w", err))
	default:
		if err := decode(out, &vote, "issue vote"); err != nil {
			errs = append(errs, fmt.Errorf("issue vote %s: %w", id, err))
		} else if !vote.Voted {
			errs = append(errs, fmt.Errorf("issue vote %s: voted false, want true", id))
		}
		if err := s.ytrackJSON(&vote, "issue", "unvote", id, "--json", "voted"); err != nil {
			errs = append(errs, fmt.Errorf("issue unvote %s: %w", id, err))
		} else if vote.Voted {
			errs = append(errs, fmt.Errorf("issue unvote %s: voted true, want false", id))
		}
	}

	var watch struct{ Watching bool }
	switch err := s.ytrackJSON(&watch, "issue", "watch", id, "--json", "watching"); {
	case err != nil:
		return errors.Join(append(errs, fmt.Errorf("issue watch %s: %w", id, err))...)
	case !watch.Watching:
		return errors.Join(append(errs, fmt.Errorf("issue watch %s: watching false, want true", id))...)
	}
	var watchers []struct{ Login string }
	if err := s.ytrackJSON(&watchers, "issue", "watchers", id, "--json", "login"); err != nil {
		errs = append(errs, fmt.Errorf("issue watchers after watch: %w", err))
	} else if !slices.Contains(watchers, struct{ Login string }{login}) {
		errs = append(errs, fmt.Errorf("issue watch %s: %s missing from the watchers %+v", id, login, watchers))
	}
	if err := s.ytrackJSON(&watch, "issue", "unwatch", id, "--json", "watching"); err != nil {
		errs = append(errs, fmt.Errorf("issue unwatch %s: %w", id, err))
	} else if watch.Watching {
		errs = append(errs, fmt.Errorf("issue unwatch %s: watching true, want false", id))
	}
	return errors.Join(errs...)
}

// deleteSteps creates a second issue, pages through the project's issues
// one per request with api --paginate --jq (both of the run's issues must
// come back, as many as issue count counts), then deletes the second issue
// with issue delete and checks that it is gone.
func (s *suite) deleteSteps(id string) error {
	var created struct{ IDReadable string }
	if err := s.ytrackJSON(&created, "issue", "create", "--project", s.cfg.Project,
		"--summary", s.prefix()+" delete", "--json", "idReadable"); err != nil {
		return fmt.Errorf("issue create (to delete): %w", err)
	}
	if created.IDReadable == "" {
		return errors.New("issue create (to delete): no idReadable in the output")
	}
	second := created.IDReadable
	s.issues = append(s.issues, second)

	query := "project: " + s.cfg.Project
	out, err := s.ytrack("api", "/issues?$top=1", "-X", "GET", "-f", "query="+query,
		"--paginate", "--fields", "idReadable", "--jq", ".[].idReadable")
	if err != nil {
		return fmt.Errorf("api --paginate --jq: %w", err)
	}
	ids := strings.Fields(out)
	var count struct{ Count int }
	if err := s.ytrackJSON(&count, "issue", "count", query, "--json", "count"); err != nil {
		return fmt.Errorf("issue count: %w", err)
	}
	if !slices.Contains(ids, id) || !slices.Contains(ids, second) || len(ids) != count.Count {
		return fmt.Errorf("api /issues?$top=1 --paginate: got %v, want %s and %s among %d issues", ids, id, second, count.Count)
	}

	if _, err := s.ytrack("issue", "delete", second, "--yes"); err != nil {
		return fmt.Errorf("issue delete: %w", err)
	}
	s.deleted[second] = true
	switch _, err := s.ytrack("issue", "view", second); {
	case err == nil:
		return fmt.Errorf("issue delete %s: issue view after it succeeded, want not found", second)
	case !strings.Contains(err.Error(), "not found"):
		return fmt.Errorf("issue delete %s: issue view after it: want not found, got %w", second, err)
	}
	return nil
}

// toolSteps smoke-tests the commands that do not talk to YouTrack: the
// agent skill, and the release check (it calls GitHub, so it is skipped
// when the run is offline).
func (s *suite) toolSteps() error {
	var errs []error
	if out, err := s.ytrack("skill", "print"); err != nil {
		errs = append(errs, fmt.Errorf("skill print: %w", err))
	} else if !strings.Contains(out, "\nname: ytrack\n") {
		errs = append(errs, fmt.Errorf("skill print: no name: ytrack front matter in\n%.300s", out))
	}
	var copies []struct{ Dir, Version, State string }
	if err := s.ytrackJSON(&copies, "skill", "list", "--json"); err != nil {
		errs = append(errs, fmt.Errorf("skill list: %w", err))
	}
	if s.cfg.Offline {
		s.logf("upgrade --check skipped: the run is offline")
		return errors.Join(errs...)
	}
	if out, err := s.ytrack("upgrade", "--check"); err != nil {
		errs = append(errs, fmt.Errorf("upgrade --check: %w", err))
	} else if strings.TrimSpace(out) == "" {
		errs = append(errs, errors.New("upgrade --check: printed nothing"))
	}
	return errors.Join(errs...)
}

// workItemReportStep sums your time on the project from yesterday to
// tomorrow (so the local day of the work item is inside whatever the time
// zone) and finds the work item itemID.
func (s *suite) workItemReportStep(itemID string) error {
	now := time.Now()
	var report struct {
		Minutes int
		Items   []struct{ ID string }
	}
	if err := s.ytrackJSON(&report, "work-item", "report", "--project", s.cfg.Project,
		"--since", now.AddDate(0, 0, -1).Format(time.DateOnly), "--until", now.AddDate(0, 0, 1).Format(time.DateOnly),
		"--json", "minutes,items"); err != nil {
		return fmt.Errorf("work-item report: %w", err)
	}
	if !slices.Contains(report.Items, struct{ ID string }{itemID}) || report.Minutes < 30 {
		return fmt.Errorf("work-item report: %s missing or under 30 minutes: %d minutes, items %+v", itemID, report.Minutes, report.Items)
	}
	return nil
}

// cleanupTags deletes the tags the run created, then sweeps tag list for
// tags named with this run's prefix.
func (s *suite) cleanupTags() error {
	var errs []error
	done := map[string]bool{}
	del := func(t tag) {
		if done[t.ID] {
			return
		}
		if _, err := s.ytrack("api", "--method", "DELETE", "/tags/"+t.ID, "--silent"); err != nil {
			errs = append(errs, fmt.Errorf("deleting tag %s: %w", t.Name, err))
			return
		}
		done[t.ID] = true
		s.logf("deleted tag %s", t.Name)
	}
	for _, t := range s.tags {
		del(t)
	}
	listed, err := s.listTags()
	if err != nil {
		errs = append(errs, fmt.Errorf("tag sweep: %w", err))
	}
	for _, t := range listed {
		if strings.HasPrefix(t.Name, s.tagPrefix()) {
			del(t)
		}
	}
	return errors.Join(errs...)
}

// decode unmarshals one ytrack output into v, naming what printed it.
func decode(out string, v any, what string) error {
	if err := json.Unmarshal([]byte(out), v); err != nil {
		return fmt.Errorf("decoding the output of %s: %w\n%s", what, err, out)
	}
	return nil
}
