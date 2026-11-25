package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// suite drives the ytrack binary through every command path like a user,
// on entities it creates and deletes itself.
type suite struct {
	cfg   itConfig
	runID string
	// env is the binary's environment: the caller's, minus every YTRACK_*
	// variable that could redirect it, plus the target host and token.
	env  []string
	logf func(format string, args ...any)

	issues  []string // issue IDs this run created, in order
	deleted map[string]bool
	tags    []tag // tags this run created, in order
}

// tag is one tag the run created.
type tag struct{ ID, Name string }

// assignToMe is the command the suite applies. It names the field rather
// than using the "for" keyword, which YouTrack localizes (a Spanish
// instance answers with its localized unknown-command error for `for`).
const assignToMe = "Assignee me"

// stepTimeout bounds one ytrack invocation.
const stepTimeout = 2 * time.Minute

// newSuite prepares a run against cfg. baseEnv is the environment the
// binary inherits (os.Environ() for a real run).
func newSuite(cfg itConfig, baseEnv []string, logf func(string, ...any)) *suite {
	env := []string{}
	for _, kv := range baseEnv {
		name, _, _ := strings.Cut(kv, "=")
		// Keep YTRACK_CONFIG_DIR so the binary finds the session the
		// suite resolved the host from.
		if strings.HasPrefix(name, "YTRACK_") && name != "YTRACK_CONFIG_DIR" {
			continue
		}
		env = append(env, kv)
	}
	// YTRACK_NO_UPDATE_NOTIFIER keeps a release binary from checking for,
	// or installing, a newer release in the background while it is tested.
	env = append(env, "YTRACK_HOST="+cfg.Host.URL, "NO_COLOR=1", "YTRACK_NO_UPDATE_NOTIFIER=1")
	if cfg.Token != "" {
		env = append(env, "YTRACK_TOKEN="+cfg.Token)
	}
	return &suite{cfg: cfg, runID: newRunID(), env: env, logf: logf, deleted: map[string]bool{}}
}

func newRunID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// prefix starts the summary of every issue the run creates.
func (s *suite) prefix() string { return "ytrack-it " + s.runID }

// tagPrefix starts the name of every tag the run creates.
func (s *suite) tagPrefix() string { return "ytrack-it-" + s.runID }

// run executes every step, then deletes what the run created, whether the
// steps passed or not. It returns the first step error joined with any
// cleanup error.
func (s *suite) run() (err error) {
	// Checked again here so no path through the suite can reach a refused
	// host, whatever built cfg.
	if err := checkHost(s.cfg.Host, s.cfg.AllowHost); err != nil {
		return err
	}
	s.logf("run %s against %s, project %s", s.runID, s.cfg.Host.Key, s.cfg.Project)
	defer func() { err = errors.Join(err, s.cleanup()) }()
	return s.steps()
}

// steps runs the suite. Issue create and edit come first, as every later
// step works on that issue; the families after them are independent, so
// all of them run and their errors are joined: one failing family does not
// hide the others.
func (s *suite) steps() error {
	// auth status: --host limits the check to the target, so other
	// logged-in hosts are never contacted.
	var status []struct{ Host, State, Login, Error string }
	if err := s.ytrackJSON(&status, "auth", "status", "--json", "host,state,login,error"); err != nil {
		return fmt.Errorf("auth status: %w", err)
	}
	if len(status) != 1 || status[0].State != "ok" {
		return fmt.Errorf("auth status: want one host in state ok, got %+v", status)
	}
	login := status[0].Login

	tagA, tagB, err := s.tagSteps()
	if err != nil {
		return err
	}
	id, err := s.createSteps(tagA)
	if err != nil {
		return err
	}
	if err := s.editSteps(id, tagA, tagB); err != nil {
		return err
	}
	return errors.Join(
		s.commentSteps(id),
		s.commandSteps(id, login),
		s.attachmentSteps(id),
		s.readSteps(id),
		s.voteWatchSteps(id, login),
		s.workItemSteps(id),
		s.deleteSteps(id),
		s.toolSteps(),
	)
}

// createSteps creates the run's issue with a custom field and a tag, and
// finds it with issue view and issue list.
func (s *suite) createSteps(tagName string) (string, error) {
	var created struct{ IDReadable, Summary string }
	if err := s.ytrackJSON(&created, "issue", "create", "--project", s.cfg.Project,
		"--summary", s.prefix()+" create", "--description", "Created by ytrack's real-instance suite.",
		"--field", itField+"="+itFieldCreate, "--tag", tagName,
		"--json", "idReadable,summary"); err != nil {
		return "", fmt.Errorf("issue create: %w", err)
	}
	if created.IDReadable == "" {
		return "", errors.New("issue create: no idReadable in the output")
	}
	id := created.IDReadable
	s.issues = append(s.issues, id)

	var viewed fieldsAndTags
	if err := s.ytrackJSON(&viewed, "issue", "view", id, "--json", "idReadable,summary,customFields,tags"); err != nil {
		return id, fmt.Errorf("issue view: %w", err)
	}
	if viewed.Summary != s.prefix()+" create" {
		return id, fmt.Errorf("issue view %s: summary %q, want %q", id, viewed.Summary, s.prefix()+" create")
	}
	if got := viewed.field(itField); got != itFieldCreate || !slices.Equal(viewed.Tags, []string{tagName}) {
		return id, fmt.Errorf("issue create --field --tag: %s is %q and tags %v, want %q and [%s]", itField, got, viewed.Tags, itFieldCreate, tagName)
	}

	var listed []struct{ IDReadable string }
	if err := s.ytrackJSON(&listed, "issue", "list", "--project", s.cfg.Project, "--all",
		"--sort", "created desc", "--limit", "50", "--json", "idReadable"); err != nil {
		return id, fmt.Errorf("issue list: %w", err)
	}
	if !slices.ContainsFunc(listed, func(i struct{ IDReadable string }) bool { return i.IDReadable == id }) {
		return id, fmt.Errorf("issue list: %s missing from the 50 newest issues of %s", id, s.cfg.Project)
	}
	return id, nil
}

// editSteps changes summary and description, then a custom field and the
// tags (add one, remove the other), and reads each change back.
func (s *suite) editSteps(id, tagA, tagB string) error {
	edited := s.prefix() + " edited"
	if _, err := s.ytrack("issue", "edit", id, "--summary", edited, "--description", "Edited by ytrack's real-instance suite."); err != nil {
		return fmt.Errorf("issue edit: %w", err)
	}
	var viewed struct{ Summary, Description string }
	if err := s.ytrackJSON(&viewed, "issue", "view", id, "--json", "summary,description"); err != nil {
		return fmt.Errorf("issue view after edit: %w", err)
	}
	if viewed.Summary != edited || viewed.Description != "Edited by ytrack's real-instance suite." {
		return fmt.Errorf("issue edit %s: got summary %q, description %q", id, viewed.Summary, viewed.Description)
	}

	if _, err := s.ytrack("issue", "edit", id, "--field", itField+"="+itFieldEdit,
		"--add-tag", tagB, "--remove-tag", tagA); err != nil {
		return fmt.Errorf("issue edit --field --add-tag --remove-tag: %w", err)
	}
	var after fieldsAndTags
	if err := s.ytrackJSON(&after, "issue", "view", id, "--json", "customFields,tags"); err != nil {
		return fmt.Errorf("issue view after edit --field: %w", err)
	}
	if got := after.field(itField); got != itFieldEdit || !slices.Equal(after.Tags, []string{tagB}) {
		return fmt.Errorf("issue edit --field --add-tag --remove-tag: %s is %q and tags %v, want %q and [%s]", itField, got, after.Tags, itFieldEdit, tagB)
	}
	return nil
}

// commandSteps previews and applies a command, previews it with --assist,
// then lists the issue as yours and checks a not-found.
func (s *suite) commandSteps(id, login string) error {
	// Command keywords such as "for" are localized with the instance's
	// language; a field name is not.
	if _, err := s.ytrack("issue", "command", id, assignToMe, "--dry-run"); err != nil {
		return fmt.Errorf("issue command --dry-run: %w", err)
	}
	var assist struct {
		Commands []struct {
			Description string
			Error       bool
		}
		Suggestions []struct{ Option string }
	}
	if err := s.ytrackJSON(&assist, "issue", "command", id, assignToMe, "--assist", "--json", "commands,suggestions"); err != nil {
		return fmt.Errorf("issue command --assist: %w", err)
	}
	failed := len(assist.Commands) == 0
	for _, c := range assist.Commands {
		failed = failed || c.Error
	}
	if failed {
		return fmt.Errorf("issue command %q --assist: commands %+v, want parsed with no error", assignToMe, assist.Commands)
	}
	if _, err := s.ytrack("issue", "command", id, assignToMe, "--silent"); err != nil {
		return fmt.Errorf("issue command: %w", err)
	}
	var assigned struct{ Assignee *struct{ Login string } }
	if err := s.ytrackJSON(&assigned, "issue", "view", id, "--json", "assignee"); err != nil {
		return fmt.Errorf("issue view assignee: %w", err)
	}
	if assigned.Assignee == nil || assigned.Assignee.Login != login {
		return fmt.Errorf("issue command %q: assignee %+v, want %s", assignToMe, assigned.Assignee, login)
	}
	if err := s.listMineStep(id); err != nil {
		return err
	}
	return s.notFoundStep()
}

// listMineStep lists the project's issues assigned to you: --assignee me
// must find id on every locale, so ytrack cannot send the "for" keyword,
// which a non-English instance does not know.
func (s *suite) listMineStep(id string) error {
	var mine []struct{ IDReadable string }
	if err := s.ytrackJSON(&mine, "issue", "list", "--project", s.cfg.Project, "--assignee", "me",
		"--limit", "0", "--json", "idReadable"); err != nil {
		return fmt.Errorf("issue list --assignee me: %w", err)
	}
	if !slices.ContainsFunc(mine, func(i struct{ IDReadable string }) bool { return i.IDReadable == id }) {
		return fmt.Errorf("issue list --assignee me: %s, assigned to you, missing from %+v", id, mine)
	}
	return nil
}

// notFoundStep views a key that does not exist in the project with --host:
// the error names the target host and suggests no other one.
func (s *suite) notFoundStep() error {
	missing := s.cfg.Project + "-999999999"
	out, err := s.ytrack("issue", "view", missing)
	if err == nil {
		return fmt.Errorf("issue view %s: exit 0, want not found\n%s", missing, out)
	}
	msg := err.Error()
	if !strings.Contains(msg, "issue "+missing+" not found on "+s.cfg.Host.Key) || strings.Contains(msg, "try --host") {
		return fmt.Errorf("issue view %s: want a not-found naming %s and no other host: %w", missing, s.cfg.Host.Key, err)
	}
	return nil
}

// itImage is a 1x1 PNG the comment --attach step uploads.
var itImage = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89" +
	"\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

// commentAttachStep posts a comment with --attach and reads the comment's
// attachments(id,name) back: the file must be on the comment itself, not
// only on the issue.
func (s *suite) commentAttachStep(id string) (err error) {
	dir, err := os.MkdirTemp("", "ytrack-it-attach-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	const name = "ytrack-it.png"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, itImage, 0o600); err != nil {
		return err
	}
	var posted struct {
		ID          string
		Attachments []struct{ Name, ID, State string }
	}
	if err := s.ytrackJSON(&posted, "issue", "comment", id, "--body", s.prefix()+" attach ![]("+name+")",
		"--attach", path, "--json", "id,attachments"); err != nil {
		return fmt.Errorf("issue comment --attach: %w", err)
	}
	if len(posted.Attachments) != 1 || posted.Attachments[0].State != "comment" {
		return fmt.Errorf("issue comment --attach: attachments %+v, want %s on the comment", posted.Attachments, name)
	}
	// issue attachments links the file to the comment it was posted with.
	listed, err := s.attachments(id)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(listed, func(a attachment) bool {
		return a.Name == name && a.CommentID != nil && *a.CommentID == posted.ID
	}) {
		return fmt.Errorf("issue attachments: %s not linked to comment %s in %+v", name, posted.ID, listed)
	}
	var comment struct{ Attachments []struct{ ID, Name string } }
	if err := s.ytrackJSON(&comment, "api", "/issues/"+id+"/comments/"+posted.ID, "-X", "GET", "--fields", "attachments(id,name)"); err != nil {
		return fmt.Errorf("reading the comment's attachments: %w", err)
	}
	if len(comment.Attachments) != 1 || comment.Attachments[0].Name != name {
		return fmt.Errorf("issue comment --attach: comment %s has attachments %+v, want %s", posted.ID, comment.Attachments, name)
	}
	return nil
}

type workItem struct {
	ID       string
	Text     string
	Duration struct{ Minutes int }
}

func (s *suite) workItemSteps(id string) error {
	var added workItem
	if err := s.ytrackJSON(&added, "work-item", "add", id, "--duration", "30m",
		"--text", s.prefix()+" work", "--json", "id,duration,text"); err != nil {
		return fmt.Errorf("work-item add: %w", err)
	}
	if added.ID == "" || added.Duration.Minutes != 30 {
		return fmt.Errorf("work-item add: got %+v, want 30 minutes", added)
	}

	listed, err := s.workItems(id)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(listed, func(w workItem) bool { return w.ID == added.ID }) {
		return fmt.Errorf("work-item list: %s missing on %s", added.ID, id)
	}
	if err := s.workItemReportStep(added.ID); err != nil {
		return err
	}

	var edited workItem
	if err := s.ytrackJSON(&edited, "work-item", "edit", id, added.ID, "--duration", "45m",
		"--text", s.prefix()+" work edited", "--json", "id,duration,text"); err != nil {
		return fmt.Errorf("work-item edit: %w", err)
	}
	if edited.Duration.Minutes != 45 || edited.Text != s.prefix()+" work edited" {
		return fmt.Errorf("work-item edit: got %+v, want 45 minutes", edited)
	}

	if _, err := s.ytrack("work-item", "delete", id, added.ID, "--yes"); err != nil {
		return fmt.Errorf("work-item delete: %w", err)
	}
	listed, err = s.workItems(id)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(listed, func(w workItem) bool { return w.ID == added.ID }) {
		return fmt.Errorf("work-item delete: %s still listed on %s", added.ID, id)
	}
	return nil
}

func (s *suite) workItems(id string) ([]workItem, error) {
	var items []workItem
	if err := s.ytrackJSON(&items, "work-item", "list", id, "--json", "id,duration,text"); err != nil {
		return nil, fmt.Errorf("work-item list: %w", err)
	}
	return items, nil
}

// cleanup deletes every issue the run created (YouTrack deletes their
// comments and work items with them), then sweeps the project for issues
// whose summary carries this run's prefix, which catches an issue created
// by a step that failed before it reported the ID.
func (s *suite) cleanup() error {
	var errs []error
	for _, id := range s.issues {
		errs = append(errs, s.deleteIssue(id))
	}
	var recent []struct{ IDReadable, Summary string }
	if err := s.ytrackJSON(&recent, "issue", "list", "--project", s.cfg.Project, "--all",
		"--sort", "created desc", "--limit", "50", "--json", "idReadable,summary"); err != nil {
		errs = append(errs, fmt.Errorf("cleanup sweep: %w", err))
	}
	for _, is := range recent {
		if strings.HasPrefix(is.Summary, s.prefix()) && !s.deleted[is.IDReadable] {
			errs = append(errs, s.deleteIssue(is.IDReadable))
		}
	}
	if err := errors.Join(errs...); err != nil {
		errs = []error{fmt.Errorf("cleanup (delete leftovers by hand; their summaries start with %q): %w", s.prefix(), err)}
	}
	if err := s.cleanupTags(); err != nil {
		errs = append(errs, fmt.Errorf("cleanup (delete leftover tags by hand; their names start with %q): %w", s.tagPrefix(), err))
	}
	return errors.Join(errs...)
}

func (s *suite) deleteIssue(id string) error {
	if s.deleted[id] {
		return nil
	}
	if _, err := s.ytrack("api", "--method", "DELETE", "/issues/"+id, "--silent"); err != nil {
		return fmt.Errorf("deleting %s: %w", id, err)
	}
	s.deleted[id] = true
	s.logf("deleted %s", id)
	return nil
}

// ytrackJSON runs ytrack and decodes its standard output into v.
func (s *suite) ytrackJSON(v any, args ...string) error {
	out, err := s.ytrack(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		return fmt.Errorf("decoding the output of ytrack %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// ytrack runs the binary with args against the target host and returns its
// standard output. A non-zero exit is an error carrying standard error.
func (s *suite) ytrack(args ...string) (string, error) {
	args = append([]string{"--host", s.cfg.Host.URL}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.cfg.Bin, args...)
	cmd.Env = s.env
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	s.logf("ytrack %s", strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("ytrack %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
