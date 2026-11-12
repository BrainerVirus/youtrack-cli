package issue_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type sentComment struct {
	Text        string `json:"text"`
	Attachments []struct {
		Name          string `json:"name"`
		Base64Content string `json:"base64Content"`
	} `json:"attachments"`
}

func attachmentCalls(yt *cmdtest.FakeYouTrack) int {
	n := 0
	for _, r := range yt.Requests() {
		if strings.HasSuffix(r.Path, "/attachments") {
			n++
		}
	}
	return n
}

func TestCommentAttach(t *testing.T) {
	posted := func(yt *cmdtest.FakeYouTrack) []cmdtest.Request {
		return requests(yt, "POST", "/api/issues/NSR-40/comments")
	}

	t.Run("given a body with ![](shot.png) and two --attach files, it posts one comment carrying both", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"shot.png": "\x89PNG-bytes", "notes.html": "<p>notes</p>"})
		mustRun(t, env, "issue", "comment", "NSR-40", "--body", "Before: ![](shot.png)",
			"--attach", filepath.Join(dir, "shot.png"), "--attach", filepath.Join(dir, "notes.html"))
		reqs := posted(yt)
		if len(reqs) != 1 || attachmentCalls(yt) != 0 {
			t.Fatalf("comment posts %d, attachment calls %d", len(reqs), attachmentCalls(yt))
		}
		var got sentComment
		if err := json.Unmarshal([]byte(reqs[0].Body), &got); err != nil {
			t.Fatal(err)
		}
		if got.Text != "Before: ![](shot.png)" || len(got.Attachments) != 2 {
			t.Fatalf("sent %+v", got)
		}
		for i, want := range []struct{ name, uri string }{
			{"shot.png", "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG-bytes"))},
			{"notes.html", "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte("<p>notes</p>"))},
		} {
			if a := got.Attachments[i]; a.Name != want.name || a.Base64Content != want.uri {
				t.Errorf("attachment %d = %s %.40s..., want %s %.40s...", i, a.Name, a.Base64Content, want.name, want.uri)
			}
		}
		if c := yt.Comments["NSR-40"]; len(c[len(c)-1]["attachments"].([]any)) != 2 {
			t.Errorf("the stored comment has %v", c[len(c)-1]["attachments"])
		}
		if want := yt.URL() + "/issue/NSR-40#focus=Comments-4-103.0-0\n"; env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("given a reference to the path given to --attach, it is rewritten to the attachment name", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"shots/a.png": "png"})
		path := filepath.Join(dir, "shots", "a.png")
		mustRun(t, env, "issue", "comment", "NSR-40", "--body", "See ![]("+path+") and [it]("+path+")", "--attach", path)
		var got sentComment
		_ = json.Unmarshal([]byte(posted(yt)[0].Body), &got)
		if got.Text != "See ![](a.png) and [it](a.png)" {
			t.Errorf("text = %q", got.Text)
		}
	})

	t.Run("given --attach missing.png, it exits 1 naming the file and posts nothing", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"ok.txt": "ok"})
		before := len(yt.Requests())
		code := env.Run("issue", "comment", "NSR-40", "--body", "x", "--attach", filepath.Join(dir, "ok.txt"), "--attach", "missing.png")
		if code != 1 || !strings.Contains(env.Stderr.String(), "missing.png") {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if len(yt.Requests()) != before {
			t.Errorf("it sent %d requests", len(yt.Requests())-before)
		}
	})

	t.Run("given two files with the same name, it exits 1 and posts nothing", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"a/log.txt": "1", "b/log.txt": "2"})
		before := len(yt.Requests())
		if code := env.Run("issue", "comment", "NSR-40", "--body", "x", "--attach", filepath.Join(dir, "a", "log.txt"), "--attach", filepath.Join(dir, "b", "log.txt")); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if !strings.Contains(env.Stderr.String(), "both are named log.txt") || len(yt.Requests()) != before {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})
}

func TestIssueAttach(t *testing.T) {
	t.Run("given two files, both land as issue attachments and each prints as name<TAB>id", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"a.sql": "select 1;", "b.sh": "#!/bin/sh\necho hi\n"})
		mustRun(t, env, "issue", "attach", "NSR-40", filepath.Join(dir, "a.sql"), filepath.Join(dir, "b.sh"))
		if env.Stdout.String() != "a.sql\t8-300\nb.sh\t8-301\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		stored := yt.Attachments["NSR-40"]
		if len(stored) != 2 || stored[0]["name"] != "a.sql" || stored[0]["content"] != "select 1;" || stored[1]["name"] != "b.sh" || stored[1]["content"] != "#!/bin/sh\necho hi\n" {
			t.Errorf("stored %v", stored)
		}
		if reqs := requests(yt, "POST", "/api/issues/NSR-40/attachments"); len(reqs) != 1 || !strings.HasPrefix(reqs[0].Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("requests = %d", len(reqs))
		}
	})

	t.Run("given --json, it prints name, id, size and mimeType", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"page.html": "<p>x</p>"})
		mustRun(t, env, "issue", "attach", "NSR-40", filepath.Join(dir, "page.html"), "--json", "name,id,size,mimeType")
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		if len(got) != 1 || got[0]["name"] != "page.html" || got[0]["id"] != "8-300" || got[0]["size"] != float64(8) || got[0]["mimeType"] != "text/html" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("given a missing file, it exits 1 naming it and uploads nothing", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"a.sql": "x"})
		if code := env.Run("issue", "attach", "NSR-40", filepath.Join(dir, "a.sql"), "nope.sh"); code != 1 || !strings.Contains(env.Stderr.String(), "nope.sh") {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if attachmentCalls(yt) != 0 {
			t.Error("it uploaded")
		}
	})

	t.Run("given an unknown issue, it exits 1 saying the issue was not found", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"a.sql": "x"})
		if code := env.Run("issue", "attach", "NOPE-1", filepath.Join(dir, "a.sql")); code != 1 || !strings.Contains(env.Stderr.String(), "issue NOPE-1 not found on "+yt.Key()) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})

	t.Run("given no file, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("issue", "attach", "NSR-40"); !env.IsUsageError(code) {
			t.Errorf("exit %d: %s", code, env.Stderr)
		}
	})
}

func TestUploadKeepsTheTokenSecret(t *testing.T) {
	t.Run("given --debug, neither an issue upload nor a comment attachment shows the token", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		dir := writeFiles(t, map[string]string{"a.png": "png"})
		path := filepath.Join(dir, "a.png")
		mustRun(t, env, "--debug", "issue", "attach", "NSR-40", path)
		mustRun(t, env, "--debug", "issue", "comment", "NSR-40", "--body", "![](a.png)", "--attach", path)
		t.Setenv("YTRACK_TOKEN", "perm-wrong.s3cr3tW0rd")
		_ = env.Run("--debug", "issue", "attach", "NSR-40", path)
		for _, out := range []string{env.Stdout.String(), env.Stderr.String()} {
			for _, secret := range []string{yt.Token, "s3cr3tT0k3nValue", "s3cr3tW0rd"} {
				if strings.Contains(out, secret) {
					t.Errorf("output shows %q:\n%s", secret, out)
				}
			}
		}
		if !strings.Contains(env.Stderr.String(), "> POST "+yt.URL()+"/api/issues/NSR-40/attachments") || !strings.Contains(env.Stderr.String(), "Authorization: Bearer [REDACTED]") {
			t.Errorf("no debug trace of the upload:\n%s", env.Stderr)
		}
	})
}
