package issue_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func rfc3339(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) }

// commentIDs lists the IDs of an issue's comments as the fake holds them.
func commentIDs(yt *cmdtest.FakeYouTrack, issue string) []string {
	ids := []string{}
	for _, c := range yt.Comments[issue] {
		ids = append(ids, c["id"].(string))
	}
	return ids
}

func hasIssue(yt *cmdtest.FakeYouTrack, id string) bool {
	for _, is := range yt.Issues {
		if is["idReadable"] == id {
			return true
		}
	}
	return false
}

func TestIssueDelete(t *testing.T) {
	t.Run("not in a terminal and without --yes, it is a usage error and sends nothing", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		before := len(yt.Requests())
		if code := env.Run("issue", "delete", "NSR-40"); !env.IsUsageError(code) || !strings.Contains(env.Stderr.String(), "`--yes` is required to delete an issue") {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if len(yt.Requests()) != before || !hasIssue(yt, "NSR-40") {
			t.Error("a request was sent or the issue is gone")
		}
	})

	t.Run("given --yes, it deletes the issue without asking or reading it", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "delete", "NSR-40", "--yes")
		if len(requests(yt, "DELETE", "/api/issues/NSR-40")) != 1 || len(requests(yt, "GET", "/api/issues/NSR-40")) != 0 {
			t.Errorf("requests = %+v", yt.Requests())
		}
		if hasIssue(yt, "NSR-40") || !hasIssue(yt, "NSR-41") {
			t.Errorf("issues = %v", yt.Issues)
		}
		if env.Stdout.Len() != 0 {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("in a terminal, it shows the issue and deletes it on yes", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Interactive()
		env.Stdin.WriteString("y\n")
		mustRun(t, env, "issue", "delete", "NSR-40")
		if want := `Delete issue NSR-40, "Login redirect drops the return URL", with its comments, attachments and work items? This cannot be undone. (y/N)`; !strings.Contains(env.Stderr.String(), want) {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "Deleted issue NSR-40") || hasIssue(yt, "NSR-40") {
			t.Errorf("not deleted: %s", env.Stderr)
		}
	})

	t.Run("in a terminal, answering no cancels with exit 2 and keeps the issue", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Interactive()
		env.Stdin.WriteString("n\n")
		if code := env.Run("issue", "delete", "NSR-40"); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		if len(requests(yt, "DELETE", "")) != 0 || !hasIssue(yt, "NSR-40") {
			t.Error("deleted")
		}
	})

	t.Run("given a database ID in a terminal, it names and deletes the readable issue", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Interactive()
		env.Stdin.WriteString("y\n")
		mustRun(t, env, "issue", "delete", "2-4041")
		if !strings.Contains(env.Stderr.String(), "Delete issue NSR-41, \"Dark mode toggle\"") || hasIssue(yt, "NSR-41") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given an unknown issue, it says so", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("issue", "delete", "NSR-99", "--yes"); code != 1 || !strings.Contains(env.Stderr.String(), "issue NSR-99 not found on "+yt.Key()) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})
}

func TestCommentList(t *testing.T) {
	t.Run("piped, it prints id, author, time and one-line text per comment, oldest first", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", "list", "NSR-40")
		want := "4-1\tjroe\t" + rfc3339(1791200000000) + "\tReproduced on staging.\n" +
			"4-2\tjdoe\t" + rfc3339(1791300000000) + "\tThe return URL is lost in the SAML relay state.\n" +
			"4-3\tjdoe\t" + rfc3339(1791360000000) + "\tFix is up for review.\n"
		if env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("given --limit, it lists the latest comments", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", "list", "NSR-40", "--limit", "2", "--json", "id")
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		if !reflect.DeepEqual(got, []map[string]any{{"id": "4-2"}, {"id": "4-3"}}) {
			t.Errorf("got %v", got)
		}
		reqs := requests(yt, "GET", "/api/issues/NSR-40/comments")
		if len(reqs) != 1 || query(t, reqs[0]).Get("$skip") != "1" {
			t.Errorf("requests = %+v", reqs)
		}
	})

	t.Run("given --json, it reports every field, the comment's files included", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedSampleAttachments()
		mustRun(t, env, "issue", "comment", "list", "2-4040", "--limit", "0", "--json", "id,text,author,created,updated,url,attachments")
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		if len(got) != 3 {
			t.Fatalf("got %v", got)
		}
		second := got[1]
		if second["text"] != "The return URL is lost\nin the SAML relay state." || second["updated"] != rfc3339(1791350000000) ||
			!reflect.DeepEqual(second["author"], map[string]any{"login": "jdoe", "fullName": "John Doe"}) ||
			second["url"] != yt.URL()+"/issue/NSR-40#focus=Comments-4-2.0-0" {
			t.Errorf("second = %v", second)
		}
		if got[0]["updated"] != nil {
			t.Errorf("an unedited comment's updated = %v, want null", got[0]["updated"])
		}
		if _, ok := got[0]["attachments"].([]any); !ok {
			t.Errorf("attachments = %#v, want a list", got[0]["attachments"])
		}
	})

	t.Run("given an issue without comments, it says so on stderr and prints nothing", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", "list", "NSR-41")
		if env.Stdout.Len() != 0 || !strings.Contains(env.Stderr.String(), "NSR-41 has no comments") {
			t.Errorf("stdout %q, stderr %q", env.Stdout, env.Stderr)
		}
	})

	t.Run("given a negative --limit, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "comment", "list", "NSR-40", "--limit", "-1"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})
}

func TestCommentAddSubcommand(t *testing.T) {
	t.Run("given comment add, it posts like comment <id>", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", "add", "NSR-40", "--body", "via add")
		if reqs := requests(yt, "POST", "/api/issues/NSR-40/comments"); len(reqs) != 1 || reqs[0].Body != `{"text":"via add"}` {
			t.Errorf("requests = %+v", reqs)
		}
	})
}

func TestCommentEdit(t *testing.T) {
	edits := func(yt *cmdtest.FakeYouTrack) []cmdtest.Request {
		return requests(yt, "POST", "/api/issues/NSR-40/comments/4-1")
	}

	t.Run("given --body, it replaces the text and prints the comment URL", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", "edit", "NSR-40", "4-1", "--body", "Reproduced on 2026.3")
		if reqs := edits(yt); len(reqs) != 1 || reqs[0].Body != `{"text":"Reproduced on 2026.3"}` {
			t.Fatalf("requests = %+v", reqs)
		}
		if yt.Comments["NSR-40"][0]["text"] != "Reproduced on 2026.3" {
			t.Errorf("stored = %v", yt.Comments["NSR-40"][0])
		}
		if want := yt.URL() + "/issue/NSR-40#focus=Comments-4-1.0-0\n"; env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("given --json and a database ID, it reports the edited comment under the readable issue", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Stdin.WriteString("from stdin")
		mustRun(t, env, "issue", "comment", "edit", "2-4040", "4-1", "-F", "-", "--json", "id,text,updated,url")
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		want := map[string]any{"id": "4-1", "text": "from stdin", "updated": rfc3339(1791460000000), "url": yt.URL() + "/issue/NSR-40#focus=Comments-4-1.0-0"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("in a terminal without a text flag, the editor opens on the current text", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Interactive()
		seen := useStubEditor(t, "Reproduced on staging and prod.\n")
		mustRun(t, env, "issue", "comment", "edit", "NSR-40", "4-1")
		if initial, err := os.ReadFile(seen); err != nil || string(initial) != "Reproduced on staging." {
			t.Errorf("editor opened on %q (%v)", initial, err)
		}
		if reqs := edits(yt); len(reqs) != 1 || reqs[0].Body != `{"text":"Reproduced on staging and prod."}` {
			t.Errorf("requests = %+v", reqs)
		}
	})

	t.Run("not in a terminal and without a text flag, it is a usage error and sends nothing", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		before := len(yt.Requests())
		if code := env.Run("issue", "comment", "edit", "NSR-40", "4-1"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if len(yt.Requests()) != before {
			t.Error("a request was sent")
		}
	})

	t.Run("given an empty text, it refuses and changes nothing", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("issue", "comment", "edit", "NSR-40", "4-1", "--body", "  "); code != 1 || !strings.Contains(env.Stderr.String(), "the new text is empty") {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if len(edits(yt)) != 0 {
			t.Error("an edit was sent")
		}
	})

	t.Run("given the same text, it sends nothing and warns", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", "edit", "NSR-40", "4-1", "--body", "Reproduced on staging.")
		if len(edits(yt)) != 0 || !strings.Contains(env.Stderr.String(), "unchanged") {
			t.Errorf("edits %d, stderr %q", len(edits(yt)), env.Stderr)
		}
	})

	t.Run("given an unknown comment, it says so before the editor opens", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Interactive()
		seen := useStubEditor(t, "lost words")
		if code := env.Run("issue", "comment", "edit", "NSR-40", "4-99"); code != 1 || !strings.Contains(env.Stderr.String(), "comment 4-99 not found on issue NSR-40 on "+yt.Key()) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if _, err := os.Stat(seen); err == nil {
			t.Error("the editor opened")
		}
	})

	t.Run("given an invalid comment ID, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "comment", "edit", "NSR-40", "latest", "--body", "x"); !env.IsUsageError(code) || !strings.Contains(env.Stderr.String(), `invalid comment ID "latest"`) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})
}

func TestCommentDelete(t *testing.T) {
	t.Run("not in a terminal and without --yes, it is a usage error and sends nothing", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		before := len(yt.Requests())
		if code := env.Run("issue", "comment", "delete", "NSR-40", "4-1"); !env.IsUsageError(code) || !strings.Contains(env.Stderr.String(), "`--yes` is required to delete a comment") {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
		if len(yt.Requests()) != before {
			t.Error("a request was sent")
		}
	})

	t.Run("given --yes, it deletes the comment without asking", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		mustRun(t, env, "issue", "comment", "delete", "NSR-40", "4-2", "--yes")
		if len(requests(yt, "DELETE", "/api/issues/NSR-40/comments/4-2")) != 1 {
			t.Errorf("requests = %+v", yt.Requests())
		}
		if got := commentIDs(yt, "NSR-40"); !reflect.DeepEqual(got, []string{"4-1", "4-3"}) {
			t.Errorf("remaining = %v", got)
		}
	})

	t.Run("in a terminal, it shows the comment and deletes it on yes", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Interactive()
		env.Stdin.WriteString("y\n")
		mustRun(t, env, "issue", "comment", "delete", "NSR-40", "4-2")
		if want := `Delete comment 4-2 on NSR-40 by jdoe, "The return URL is lost in the SAML relay state."? (y/N)`; !strings.Contains(env.Stderr.String(), want) {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if got := commentIDs(yt, "NSR-40"); !reflect.DeepEqual(got, []string{"4-1", "4-3"}) {
			t.Errorf("remaining = %v", got)
		}
	})

	t.Run("in a terminal, answering no cancels with exit 2 and keeps the comment", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		env.Interactive()
		env.Stdin.WriteString("n\n")
		if code := env.Run("issue", "comment", "delete", "NSR-40", "4-1"); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		if len(requests(yt, "DELETE", "")) != 0 || len(commentIDs(yt, "NSR-40")) != 3 {
			t.Error("deleted")
		}
	})

	t.Run("given an unknown comment, it says so", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("issue", "comment", "delete", "NSR-40", "4-99", "--yes"); code != 1 || !strings.Contains(env.Stderr.String(), "comment 4-99 not found on issue NSR-40 on "+yt.Key()) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})
}

// A long comment is cut in the delete prompt.
func TestCommentDeletePromptExcerpt(t *testing.T) {
	env, yt := loggedIn(t, "")
	yt.Comments["NSR-40"][0]["text"] = strings.Repeat("word ", 30)
	env.Interactive()
	env.Stdin.WriteString("n\n")
	_ = env.Run("issue", "comment", "delete", "NSR-40", "4-1")
	if !strings.Contains(env.Stderr.String(), `word word…"?`) || strings.Count(env.Stderr.String(), "word") > 13 {
		t.Errorf("stderr = %q", env.Stderr)
	}
}
