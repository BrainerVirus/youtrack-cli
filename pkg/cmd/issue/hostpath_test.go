package issue_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

// personalAndWork logs in to personal (the default host) and work, both
// serving the sample issues.
func personalAndWork(t *testing.T) (env *cmdtest.Env, personal, work *cmdtest.FakeYouTrack) {
	t.Helper()
	env = cmdtest.New(t)
	personal = cmdtest.NewFakeYouTrack(t, "")
	work = cmdtest.NewFakeYouTrack(t, "")
	work.Token = "perm-d29yaw==.d29yaw==.w0rkT0k3nValue"
	personal.SeedSampleIssues()
	work.SeedSampleIssues()
	env.Login(personal)
	env.Login(work)
	return env, personal, work
}

func issueGets(yt *cmdtest.FakeYouTrack) int { return len(requests(yt, "GET", "/api/issues/NSR-40")) }

func TestHostByFolder(t *testing.T) {
	t.Run("given a folder matching work's path and personal as default host, a command talks to work", func(t *testing.T) {
		env, personal, work := personalAndWork(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		env.Factory.WorkDir = func() (string, error) { return "/w/repo", nil }
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable")
		if issueGets(work) != 1 || issueGets(personal) != 0 {
			t.Errorf("work got %d, personal got %d", issueGets(work), issueGets(personal))
		}
	})

	t.Run("given a matching folder and YTRACK_HOST, YTRACK_HOST wins", func(t *testing.T) {
		env, personal, work := personalAndWork(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		env.Factory.WorkDir = func() (string, error) { return "/w/repo", nil }
		t.Setenv("YTRACK_HOST", personal.URL())
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable")
		if issueGets(personal) != 1 || issueGets(work) != 0 {
			t.Errorf("personal got %d, work got %d", issueGets(personal), issueGets(work))
		}
	})

	t.Run("given a matching folder and --host, --host wins", func(t *testing.T) {
		env, personal, work := personalAndWork(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		env.Factory.WorkDir = func() (string, error) { return "/w/repo", nil }
		mustRun(t, env, "issue", "view", "NSR-40", "--host", personal.URL(), "--json", "idReadable")
		if issueGets(personal) != 1 || issueGets(work) != 0 {
			t.Errorf("personal got %d, work got %d", issueGets(personal), issueGets(work))
		}
	})

	t.Run("given a folder outside every path, the default host is used", func(t *testing.T) {
		env, personal, work := personalAndWork(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		env.Factory.WorkDir = func() (string, error) { return "/elsewhere", nil }
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable")
		if issueGets(personal) != 1 || issueGets(work) != 0 {
			t.Errorf("personal got %d, work got %d", issueGets(personal), issueGets(work))
		}
	})
}

func TestIssueURLForms(t *testing.T) {
	for _, form := range []string{
		"/projects/NSR/issues/NSR-40",
		"/projects/NSR/issues/NSR-40/login-fails?tab=comments#focus=Comments-4-1.0-0",
		"/issue/NSR-40/login-fails-on-safari",
		"/issue/NSR-40?u=1#comment",
	} {
		t.Run("given an issue URL like "+form+", issue view resolves it on the URL's host", func(t *testing.T) {
			for _, prefix := range []string{"", "/youtrack"} {
				env := cmdtest.New(t)
				other := cmdtest.NewFakeYouTrack(t, prefix)
				yt := cmdtest.NewFakeYouTrack(t, prefix)
				other.SeedSampleIssues()
				yt.SeedSampleIssues()
				env.Login(other)
				env.Login(yt)
				mustRun(t, env, "issue", "view", yt.URL()+form, "--json", "idReadable,url")
				want := `{"idReadable":"NSR-40","url":"` + yt.URL() + `/issue/NSR-40"}` + "\n"
				if env.Stdout.String() != want || issueGets(other) != 0 {
					t.Errorf("prefix %q: stdout = %q, want %q; default host got %d", prefix, env.Stdout, want, issueGets(other))
				}
			}
		})
	}

	t.Run("given a URL that names no issue, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		for _, u := range []string{"https://acme.youtrack.cloud/projects/NSR/issues", "https://acme.youtrack.cloud/projects/NSR", "https://acme.youtrack.cloud/issue/"} {
			env.Reset()
			if code := env.Run("issue", "view", u); !env.IsUsageError(code) {
				t.Errorf("%s: exit %d: %s", u, code, env.Stderr)
			}
		}
	})
}

func TestNotFoundOnDefaultHost(t *testing.T) {
	t.Run("given an issue only on another logged-in host, view names that host and the --host rerun", func(t *testing.T) {
		env, personal, work := personalAndWork(t)
		personal.Issues = nil
		if code := env.Run("issue", "view", "NSR-40"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		want := "issue NSR-40 not found on " + personal.Key() + " (or you cannot see it); it is on " + work.Key() + ": rerun with --host " + work.Key()
		if !strings.Contains(env.Stderr.String(), want) {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})

	t.Run("given --host, a not-found does not look at other hosts", func(t *testing.T) {
		env, personal, work := personalAndWork(t)
		personal.Issues = nil
		before := issueGets(work)
		if code := env.Run("issue", "view", "NSR-40", "--host", personal.URL()); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if issueGets(work) != before || strings.Contains(env.Stderr.String(), "rerun with") {
			t.Errorf("stderr = %q, work got %d", env.Stderr, issueGets(work)-before)
		}
	})

	t.Run("given a folder path picked the host, a not-found does not look at other hosts", func(t *testing.T) {
		env, personal, work := personalAndWork(t)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", "/w/**")
		env.Factory.WorkDir = func() (string, error) { return "/w/repo", nil }
		work.Issues = nil
		if code := env.Run("issue", "view", "NSR-40"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if issueGets(personal) != 0 || strings.Contains(env.Stderr.String(), "rerun with") {
			t.Errorf("stderr = %q, personal got %d", env.Stderr, issueGets(personal))
		}
	})

	t.Run("given YTRACK_TOKEN, the probe of other hosts never sends it", func(t *testing.T) {
		env, personal, work := personalAndWork(t)
		personal.Issues = nil
		t.Setenv("YTRACK_TOKEN", personal.Token)
		_ = env.Run("issue", "view", "NSR-40")
		for _, r := range work.Requests() {
			if strings.Contains(r.Header.Get("Authorization"), personal.Token) {
				t.Errorf("%s %s carried YTRACK_TOKEN", r.Method, r.Path)
			}
		}
	})
}

// writeCases are the issue writes that name another host on a not-found.
func writeCases(t *testing.T) map[string][]string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "log.txt")
	if err := os.WriteFile(file, []byte("trace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string][]string{
		"comment":        {"issue", "comment", "NSR-40", "-b", "x"},
		"attach":         {"issue", "attach", "NSR-40", file},
		"edit --summary": {"issue", "edit", "NSR-40", "-s", "New summary"},
		"edit --field":   {"issue", "edit", "NSR-40", "-f", "Priority=Major"},
		"edit --add-tag": {"issue", "edit", "NSR-40", "--add-tag", "regression"},
	}
}

// writes counts the requests to yt that are not reads.
func writes(yt *cmdtest.FakeYouTrack) int {
	n := 0
	for _, r := range yt.Requests() {
		if r.Method != http.MethodGet {
			n++
		}
	}
	return n
}

func TestNotFoundOnDefaultHostWrites(t *testing.T) {
	for name, args := range writeCases(t) {
		t.Run("given an issue only on another logged-in host, "+name+" names that host and writes nowhere else", func(t *testing.T) {
			env, personal, work := personalAndWork(t)
			personal.SeedSampleProjects()
			personal.Issues = nil
			if code := env.Run(args...); code != 1 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			want := "issue NSR-40 not found on " + personal.Key() + " (or you cannot see it); it is on " + work.Key() + ": rerun with --host " + work.Key()
			if !strings.Contains(env.Stderr.String(), want) {
				t.Errorf("stderr = %q, want %q", env.Stderr, want)
			}
			if writes(work) != 0 || issueGets(work) != 1 {
				t.Errorf("work got %d writes and %d issue reads, want 0 and 1", writes(work), issueGets(work))
			}
			if strings.Contains(env.Stderr.String()+env.Stdout.String(), work.Token) {
				t.Errorf("output carries work's token: %s", env.Stderr)
			}
		})

		t.Run("given --host, "+name+" does not look at other hosts", func(t *testing.T) {
			env, personal, work := personalAndWork(t)
			personal.SeedSampleProjects()
			personal.Issues = nil
			before := len(work.Requests())
			if code := env.Run(append(args, "--host", personal.URL())...); code != 1 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			if len(work.Requests()) != before || strings.Contains(env.Stderr.String(), "rerun with") {
				t.Errorf("stderr = %q, work got %d requests", env.Stderr, len(work.Requests())-before)
			}
		})
	}
}
