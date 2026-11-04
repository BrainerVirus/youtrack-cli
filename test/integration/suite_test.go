package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
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
}

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
	env = append(env, "YTRACK_HOST="+cfg.Host.URL, "NO_COLOR=1")
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

	// issue create
	var created struct{ IDReadable, Summary string }
	if err := s.ytrackJSON(&created, "issue", "create", "--project", s.cfg.Project,
		"--summary", s.prefix()+" create", "--description", "Created by ytrack's real-instance suite.",
		"--json", "idReadable,summary"); err != nil {
		return fmt.Errorf("issue create: %w", err)
	}
	if created.IDReadable == "" {
		return errors.New("issue create: no idReadable in the output")
	}
	id := created.IDReadable
	s.issues = append(s.issues, id)

	// issue view
	var viewed struct{ IDReadable, Summary, Description string }
	if err := s.ytrackJSON(&viewed, "issue", "view", id, "--json", "idReadable,summary,description"); err != nil {
		return fmt.Errorf("issue view: %w", err)
	}
	if viewed.Summary != s.prefix()+" create" {
		return fmt.Errorf("issue view %s: summary %q, want %q", id, viewed.Summary, s.prefix()+" create")
	}

	// issue list
	var listed []struct{ IDReadable string }
	if err := s.ytrackJSON(&listed, "issue", "list", "--project", s.cfg.Project, "--all",
		"--sort", "created desc", "--limit", "50", "--json", "idReadable"); err != nil {
		return fmt.Errorf("issue list: %w", err)
	}
	if !slices.ContainsFunc(listed, func(i struct{ IDReadable string }) bool { return i.IDReadable == id }) {
		return fmt.Errorf("issue list: %s missing from the 50 newest issues of %s", id, s.cfg.Project)
	}

	// issue edit
	edited := s.prefix() + " edited"
	if _, err := s.ytrack("issue", "edit", id, "--summary", edited, "--description", "Edited by ytrack's real-instance suite."); err != nil {
		return fmt.Errorf("issue edit: %w", err)
	}
	if err := s.ytrackJSON(&viewed, "issue", "view", id, "--json", "summary,description"); err != nil {
		return fmt.Errorf("issue view after edit: %w", err)
	}
	if viewed.Summary != edited || viewed.Description != "Edited by ytrack's real-instance suite." {
		return fmt.Errorf("issue edit %s: got summary %q, description %q", id, viewed.Summary, viewed.Description)
	}

	// issue comment
	commentText := s.prefix() + " comment"
	var comment struct{ ID string }
	if err := s.ytrackJSON(&comment, "issue", "comment", id, "--body", commentText, "--json", "id"); err != nil {
		return fmt.Errorf("issue comment: %w", err)
	}
	if comment.ID == "" {
		return errors.New("issue comment: no comment id in the output")
	}
	var withComments struct{ Comments []struct{ Text string } }
	if err := s.ytrackJSON(&withComments, "issue", "view", id, "--json", "comments"); err != nil {
		return fmt.Errorf("issue view comments: %w", err)
	}
	if !slices.ContainsFunc(withComments.Comments, func(c struct{ Text string }) bool { return c.Text == commentText }) {
		return fmt.Errorf("issue comment: %q not among the comments of %s", commentText, id)
	}

	// issue command: preview, then apply. Command keywords such as "for"
	// are localized with the instance's language; a field name is not.
	if _, err := s.ytrack("issue", "command", id, assignToMe, "--dry-run"); err != nil {
		return fmt.Errorf("issue command --dry-run: %w", err)
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

	return s.workItemSteps(id)
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
		return fmt.Errorf("cleanup (delete leftovers by hand; their summaries start with %q): %w", s.prefix(), err)
	}
	return nil
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
