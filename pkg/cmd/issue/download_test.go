package issue_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func withAttachments(t *testing.T, prefix string) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
	t.Helper()
	env, yt := loggedIn(t, prefix)
	yt.SeedSampleAttachments()
	env.Reset()
	return env, yt
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIssueAttachments(t *testing.T) {
	t.Run("given an issue with a file on it and one on a comment, it lists both with name, id, size, type, author and comment", func(t *testing.T) {
		env, _ := withAttachments(t, "")
		mustRun(t, env, "issue", "attachments", "NSR-40")
		want := "trace.log\t8-1\t14\ttext/plain\t2026-10-06T15:20:00Z\tjroe\t\n" +
			"shot.png\t8-2\t11\timage/png\t2026-10-07T05:13:20Z\tjdoe\t4-2\n"
		if env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
	})

	t.Run("given --json, it gives the same fields", func(t *testing.T) {
		env, _ := withAttachments(t, "")
		mustRun(t, env, "issue", "attachments", "NSR-40", "--json", "name,id,size,mimeType,created,author,commentId")
		got := decodeJSON[[]map[string]any](t, env.Stdout.Bytes())
		if len(got) != 2 {
			t.Fatalf("got %d attachments: %s", len(got), env.Stdout)
		}
		if got[0]["commentId"] != nil || got[1]["commentId"] != "4-2" {
			t.Errorf("commentId: %v, %v", got[0]["commentId"], got[1]["commentId"])
		}
		author, _ := got[1]["author"].(map[string]any)
		if got[1]["name"] != "shot.png" || got[1]["id"] != "8-2" || got[1]["size"] != float64(11) ||
			got[1]["mimeType"] != "image/png" || got[1]["created"] != "2026-10-07T05:13:20Z" || author["login"] != "jdoe" {
			t.Errorf("second attachment: %v", got[1])
		}
	})

	t.Run("given an issue with no attachments, it says so on stderr and prints nothing", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		mustRun(t, env, "issue", "attachments", "NSR-41")
		if env.Stdout.Len() != 0 || env.Stderr.String() != "NSR-41 has no attachments\n" {
			t.Errorf("stdout %q, stderr %q", env.Stdout, env.Stderr)
		}
	})
}

func TestIssueDownload(t *testing.T) {
	t.Run("given -o out/, it writes every attachment under its name and prints name<TAB>bytes", func(t *testing.T) {
		for _, prefix := range []string{"", "/youtrack"} {
			env, _ := withAttachments(t, prefix)
			out := filepath.Join(t.TempDir(), "out")
			mustRun(t, env, "issue", "download", "NSR-40", "-o", out)
			if want := "trace.log\t14\nshot.png\t11\n"; env.Stdout.String() != want {
				t.Errorf("prefix %q: stdout = %q, want %q", prefix, env.Stdout, want)
			}
			if got := readFile(t, filepath.Join(out, "trace.log")); got != "GET /login 302" {
				t.Errorf("trace.log = %q", got)
			}
			if got := readFile(t, filepath.Join(out, "shot.png")); got != "\u0089PNG-bytes" {
				t.Errorf("shot.png = %q", got)
			}
		}
	})

	t.Run("given the files exist, a second run refuses to overwrite them and names them; --clobber overwrites", func(t *testing.T) {
		env, yt := withAttachments(t, "")
		out := t.TempDir()
		mustRun(t, env, "issue", "download", "NSR-40", "-o", out)
		if err := os.WriteFile(filepath.Join(out, "trace.log"), []byte("mine"), 0o600); err != nil {
			t.Fatal(err)
		}
		env.Reset()
		if code := env.Run("issue", "download", "NSR-40", "-o", out); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		stderr := env.Stderr.String()
		if !strings.Contains(stderr, filepath.Join(out, "trace.log")+" already exists; pass --clobber") ||
			!strings.Contains(stderr, "could not download 2 of 2 attachments: trace.log, shot.png") {
			t.Errorf("stderr = %q", stderr)
		}
		if got := readFile(t, filepath.Join(out, "trace.log")); got != "mine" {
			t.Errorf("trace.log was overwritten: %q", got)
		}
		if n := len(requests(yt, "GET", "/api/files/8-1")); n != 1 {
			t.Errorf("the refused run still downloaded: %d file reads", n)
		}
		env.Reset()
		mustRun(t, env, "issue", "download", "NSR-40", "-o", out, "--clobber")
		if got := readFile(t, filepath.Join(out, "trace.log")); got != "GET /login 302" {
			t.Errorf("--clobber left %q", got)
		}
		if entries, _ := os.ReadDir(out); len(entries) != 2 {
			t.Errorf("out holds %d entries, want the 2 files and no temporary files", len(entries))
		}
	})

	t.Run("given an attachment named ../evil.txt, it is written inside out/ under a sanitised name and stderr says so", func(t *testing.T) {
		env, yt := withAttachments(t, "")
		yt.AddAttachment("NSR-40", "../evil.txt", "boo")
		parent := t.TempDir()
		out := filepath.Join(parent, "out")
		mustRun(t, env, "issue", "download", "NSR-40", "../evil.txt", "-o", out)
		if got := readFile(t, filepath.Join(out, ".._evil.txt")); got != "boo" {
			t.Errorf("sanitised file = %q", got)
		}
		if _, err := os.Stat(filepath.Join(parent, "evil.txt")); !os.IsNotExist(err) {
			t.Errorf("a file escaped out/: %v", err)
		}
		if want := `saving "../evil.txt" as ".._evil.txt": attachment names cannot hold a path` + "\n"; env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
		if env.Stdout.String() != ".._evil.txt\t3\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given names Windows cannot save (NUL.txt, 'a. '), it saves them under safe names and stderr says so", func(t *testing.T) {
		env, yt := withAttachments(t, "")
		yt.AddAttachment("NSR-40", "NUL.txt", "device")
		yt.AddAttachment("NSR-40", "a. ", "dots")
		out := t.TempDir()
		mustRun(t, env, "issue", "download", "NSR-40", "NUL.txt", "a. ", "-o", out)
		if got := readFile(t, filepath.Join(out, "_NUL.txt")); got != "device" {
			t.Errorf("_NUL.txt = %q", got)
		}
		if got := readFile(t, filepath.Join(out, "a__")); got != "dots" {
			t.Errorf("a__ = %q", got)
		}
		if env.Stdout.String() != "_NUL.txt\t6\na__\t4\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		want := `saving "NUL.txt" as "_NUL.txt": the name is not a valid file name on every system` + "\n" +
			`saving "a. " as "a__": the name is not a valid file name on every system` + "\n"
		if env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})

	t.Run("given names and IDs, it downloads only those; one that matches nothing fails before any download", func(t *testing.T) {
		env, yt := withAttachments(t, "")
		out := t.TempDir()
		mustRun(t, env, "issue", "download", "NSR-40", "8-2", "-o", out)
		if env.Stdout.String() != "shot.png\t11\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		env.Reset()
		if code := env.Run("issue", "download", "NSR-40", "trace.log", "nope.txt", "-o", t.TempDir()); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		if !strings.Contains(env.Stderr.String(), `no attachment named or with ID "nope.txt" on NSR-40`) {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if n := len(requests(yt, "GET", "/api/files/8-1")); n != 0 {
			t.Errorf("%d downloads after a bad name", n)
		}
	})

	t.Run("given --comment, it downloads only that comment's files", func(t *testing.T) {
		env, _ := withAttachments(t, "")
		mustRun(t, env, "issue", "download", "NSR-40", "--comment", "4-2", "-o", t.TempDir())
		if env.Stdout.String() != "shot.png\t11\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		env.Reset()
		if code := env.Run("issue", "download", "NSR-40", "--comment", "4-1", "-o", t.TempDir()); code != 1 ||
			!strings.Contains(env.Stderr.String(), "comment 4-1 on NSR-40 has no attachments") {
			t.Errorf("exit %d, stderr %q", code, env.Stderr)
		}
	})

	t.Run("given two attachments with the same name, the second gets its ID added and stderr says so", func(t *testing.T) {
		env, yt := withAttachments(t, "")
		id := yt.AddAttachment("NSR-40", "trace.log", "second")
		out := t.TempDir()
		mustRun(t, env, "issue", "download", "NSR-40", "trace.log", "-o", out)
		if got := readFile(t, filepath.Join(out, "trace-"+id+".log")); got != "second" {
			t.Errorf("second trace.log = %q", got)
		}
		if !strings.Contains(env.Stderr.String(), `as "trace-`+id+`.log": another attachment has the same name`) {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given one download fails, the others are still written and it exits 1 naming the failed one", func(t *testing.T) {
		env, yt := withAttachments(t, "")
		yt.MissingFiles = []string{"8-1"}
		out := t.TempDir()
		if code := env.Run("issue", "download", "NSR-40", "-o", out); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		if env.Stdout.String() != "shot.png\t11\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		if !strings.Contains(env.Stderr.String(), "could not download 1 of 2 attachments: trace.log") {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if _, err := os.Stat(filepath.Join(out, "trace.log")); !os.IsNotExist(err) {
			t.Errorf("a failed download left a file: %v", err)
		}
	})

	t.Run("given a key whose project is on the other logged-in host, it downloads from that host", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		work.SeedSampleAttachments()
		out := t.TempDir()
		mustRun(t, env, "issue", "download", "NSR-40", "-o", out)
		if env.Stdout.String() != "trace.log\t14\nshot.png\t11\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		if want := "using " + work.Key() + " for NSR-* (the folder rule picked " + personal.Key() + ")\n"; env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
		if n := len(requests(personal, "GET", "/api/files/8-1")); n != 0 {
			t.Errorf("personal got %d file reads", n)
		}
		if n := len(requests(work, "GET", "/api/files/8-1")); n != 1 {
			t.Errorf("work got %d file reads", n)
		}
	})

	t.Run("given an issue URL on a host, it downloads from that host", func(t *testing.T) {
		env, personal, work := cmdtest.TwoHosts(t)
		work.SeedSampleIssues()
		work.SeedSampleAttachments()
		mustRun(t, env, "issue", "download", work.URL()+"/issue/NSR-40", "8-1", "-o", t.TempDir())
		if env.Stdout.String() != "trace.log\t14\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		if n := len(requests(personal, "GET", "/attachments")) + len(requests(personal, "GET", "/api/files/8-1")); n != 0 {
			t.Errorf("personal got %d attachment requests", n)
		}
	})
}
