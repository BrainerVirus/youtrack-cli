package issue_test

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
)

// personalAndWork logs in to personal (the default host) and work, both
// serving the sample issues.
func personalAndWork(t *testing.T) (env *cmdtest.Env, personal, work *cmdtest.FakeYouTrack) {
	t.Helper()
	env, personal, work = cmdtest.TwoHosts(t)
	personal.SeedSampleIssues()
	work.SeedSampleIssues()
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

// wrongFolder sets up NSR-40 on work only, with a folder rule that picks
// personal (which has only the OWN project) for the working directory.
func wrongFolder(t *testing.T) (env *cmdtest.Env, personal, work *cmdtest.FakeYouTrack) {
	t.Helper()
	env, personal, work = cmdtest.TwoHosts(t)
	personal.AddProject("OWN")
	work.SeedSampleIssues()
	work.SeedSampleProjects()
	mustRun(t, env, "auth", "paths", "--host", personal.URL(), "--add", "/p/**")
	env.Factory.WorkDir = func() (string, error) { return "/p/repo", nil }
	env.Reset()
	return env, personal, work
}

func TestHostByIssueKey(t *testing.T) {
	t.Run("given the folder rule picks a host without the key's project, view uses the one host that has it and says so", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable,url")
		if want := `{"idReadable":"NSR-40","url":"` + work.URL() + `/issue/NSR-40"}` + "\n"; env.Stdout.String() != want {
			t.Errorf("stdout = %q, want %q", env.Stdout, want)
		}
		if want := "using " + work.Key() + " for NSR-* (the folder rule picked " + personal.Key() + ")\n"; env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})

	t.Run("given a cached answer, a second view goes straight to the owner with no project lookup", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable")
		lookups, personalReads := personal.ProjectLookups()+work.ProjectLookups(), issueGets(personal)
		env.Reset()
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable")
		if n := personal.ProjectLookups() + work.ProjectLookups() - lookups; n != 0 {
			t.Errorf("second run made %d project lookups", n)
		}
		if issueGets(personal) != personalReads {
			t.Errorf("second run asked personal for the issue")
		}
		if want := "using " + work.Key() + " for NSR-* (the folder rule picked " + personal.Key() + ")\n"; env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})

	t.Run("given no folder rule matches, the default host is the one named", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		env.Factory.WorkDir = func() (string, error) { return "/elsewhere", nil }
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable")
		if want := "using " + work.Key() + " for NSR-* (the default host is " + personal.Key() + ")\n"; env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})

	for _, explicit := range []struct{ name, why string }{{"--host", "by --host"}, {"YTRACK_HOST", "by YTRACK_HOST"}} {
		t.Run("given "+explicit.name+" names a host without the key, no other host is tried and the error says where the host came from", func(t *testing.T) {
			env, personal, work := wrongFolder(t)
			before := len(work.Requests())
			args := []string{"issue", "view", "NSR-40"}
			if explicit.name == "--host" {
				args = append(args, "--host", personal.URL())
			} else {
				t.Setenv("YTRACK_HOST", personal.URL())
			}
			if code := env.Run(args...); code != 1 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			want := "ytrack: issue NSR-40 not found on " + personal.Key() + " (or you cannot see it); " + personal.Key() + " chosen " + explicit.why + "\n"
			if env.Stderr.String() != want {
				t.Errorf("stderr = %q, want %q", env.Stderr, want)
			}
			if len(work.Requests()) != before || personal.ProjectLookups() != 0 {
				t.Errorf("work got %d requests, personal %d project lookups", len(work.Requests())-before, personal.ProjectLookups())
			}
		})
	}

	t.Run("given the key's project is on no host, the error names the host and why, and suggests none", func(t *testing.T) {
		env, personal, _ := wrongFolder(t)
		if code := env.Run("issue", "view", "ZZZ-1"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		want := "ytrack: issue ZZZ-1 not found on " + personal.Key() + " (or you cannot see it); " + personal.Key() + " chosen by the folder rule /p/**\n"
		if env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q", env.Stderr, want)
		}
	})

	t.Run("given the picked host has the project, a missing issue is not looked for elsewhere", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		personal.AddProject("NSR")
		if code := env.Run("issue", "view", "NSR-40"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if issueGets(work) != 0 || strings.Contains(env.Stderr.String(), "using ") {
			t.Errorf("stderr = %q, work got %d issue reads", env.Stderr, issueGets(work))
		}
	})

	t.Run("given two other hosts have the project, none is picked and the error suggests both", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		third := cmdtest.NewFakeYouTrack(t, "")
		third.Token = "perm-dGhpcmQ=.dGhpcmQ=.th1rdT0k3nValue"
		third.AddProject("NSR")
		env.Login(third)
		if code := env.Run("issue", "view", "NSR-40"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		tries := []string{"--host " + work.Key(), "--host " + third.Key()}
		slices.Sort(tries)
		want := "ytrack: issue NSR-40 not found on " + personal.Key() + " (or you cannot see it); " + personal.Key() +
			" chosen by the folder rule /p/**; try " + strings.Join(tries, " or ") + "\n"
		if issueGets(work) != 0 || env.Stderr.String() != want {
			t.Errorf("stderr = %q, want %q; work got %d issue reads", env.Stderr, want, issueGets(work))
		}
	})

	t.Run("given an owner whose auth paths exclude this folder, the error never names it", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		third := cmdtest.NewFakeYouTrack(t, "")
		third.Token = "perm-dGhpcmQ=.dGhpcmQ=.th1rdT0k3nValue"
		third.AddProject("NSR")
		env.Login(third)
		mustRun(t, env, "auth", "paths", "--host", work.URL(), "--add", "/work/**")
		env.Reset()
		if code := env.Run("issue", "view", "NSR-40"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		got := env.Stderr.String()
		if strings.Contains(got, work.Key()) || !strings.HasSuffix(got, "; try --host "+third.Key()+"\n") || !strings.Contains(got, "not found on "+personal.Key()) {
			t.Errorf("stderr = %q, want only %s suggested", got, third.Key())
		}
	})

	t.Run("given the picked host has the project, a missing work item suggests no host", func(t *testing.T) {
		env, personal, _ := wrongFolder(t)
		personal.AddProject("NSR")
		personal.SeedSampleIssues()
		if code := env.Run("work-item", "delete", "NSR-40", "999-999", "--yes"); code != 1 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got := env.Stderr.String(); strings.Contains(got, "try --host") {
			t.Errorf("stderr = %q, want no host suggestion", got)
		}
	})

	t.Run("given YTRACK_TOKEN, no other host is tried and the token goes nowhere else", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		t.Setenv("YTRACK_TOKEN", personal.Token)
		before := len(work.Requests())
		_ = env.Run("issue", "view", "NSR-40")
		if n := len(work.Requests()) - before; n != 0 {
			t.Errorf("work got %d requests", n)
		}
	})

	t.Run("given one logged-in host, a not-found makes no project lookup", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("issue", "view", "NOPE-1"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		if want := "ytrack: issue NOPE-1 not found on " + yt.Key() + " (or you cannot see it)\n"; env.Stderr.String() != want || yt.ProjectLookups() != 0 {
			t.Errorf("stderr = %q, %d project lookups", env.Stderr, yt.ProjectLookups())
		}
	})

	t.Run("given two hosts, an API error names the host that answered and why", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		if code := env.Run("api", "/issues/NSR-40"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		want := "HTTP 404 from " + personal.Key() + " (chosen by the folder rule /p/**): "
		if !strings.Contains(env.Stderr.String(), want) || strings.Contains(env.Stderr.String(), "try --host") || strings.Contains(env.Stderr.String(), work.Key()) {
			t.Errorf("stderr = %q, want it to contain %q", env.Stderr, want)
		}
	})
}

// writeCases are the issue writes that follow the issue key's host.
func writeCases(t *testing.T) map[string][]string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "log.txt")
	if err := os.WriteFile(file, []byte("trace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string][]string{
		"comment":        {"issue", "comment", "NSR-40", "-b", "x"},
		"comment+attach": {"issue", "comment", "NSR-40", "-b", "x", "--attach", file},
		"attach":         {"issue", "attach", "NSR-40", file},
		"edit --summary": {"issue", "edit", "NSR-40", "-s", "New summary"},
		"edit --field":   {"issue", "edit", "NSR-40", "-f", "Priority=Major"},
		"edit --add-tag": {"issue", "edit", "NSR-40", "--add-tag", "regression"},
		"command":        {"issue", "command", "NSR-40", "Priority Major"},
		"work-item add":  {"work-item", "add", "NSR-40", "--duration", "1h"},
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

func TestHostByIssueKeyWrites(t *testing.T) {
	for name, args := range writeCases(t) {
		t.Run("given the wrong folder, "+name+" writes to the host that has the key and never to the picked one", func(t *testing.T) {
			env, personal, work := wrongFolder(t)
			mustRun(t, env, args...)
			if writes(personal) != 0 || writes(work) == 0 {
				t.Errorf("personal got %d writes, work %d", writes(personal), writes(work))
			}
			if !strings.Contains(env.Stderr.String(), "using "+work.Key()+" for NSR-*") {
				t.Errorf("stderr = %q", env.Stderr)
			}
			if strings.Contains(env.Stderr.String()+env.Stdout.String(), work.Token) {
				t.Errorf("output carries work's token: %s", env.Stderr)
			}
		})

		t.Run("given --host, "+name+" does not look at other hosts", func(t *testing.T) {
			env, personal, work := wrongFolder(t)
			personal.SeedSampleProjects()
			before := len(work.Requests())
			if code := env.Run(append(args, "--host", personal.URL())...); code != 1 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			if len(work.Requests()) != before || strings.Contains(env.Stderr.String(), "using ") {
				t.Errorf("stderr = %q, work got %d requests", env.Stderr, len(work.Requests())-before)
			}
		})
	}

	t.Run("given a cached 'picked host lacks the project' that is no longer true, a write re-checks it and stays on the picked host", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable") // caches: personal lacks NSR
		personal.SeedSampleIssues()
		personal.SeedSampleProjects() // NSR now exists on personal
		env.Reset()
		workWrites := writes(work)
		mustRun(t, env, "issue", "comment", "NSR-40", "-b", "stays here")
		if writes(personal) == 0 || writes(work) != workWrites || strings.Contains(env.Stderr.String(), "using ") {
			t.Errorf("personal got %d writes, work %d more; stderr = %q", writes(personal), writes(work)-workWrites, env.Stderr)
		}
		cache := config.ProjectCache{Dir: env.CacheDir}
		if has, known := cache.Lookup(personal.URL(), "NSR"); !has || !known {
			t.Errorf("cache for personal: has %v known %v, want refreshed to true", has, known)
		}
	})

	t.Run("given cached answers for another URL of the same host key, they are not used", func(t *testing.T) {
		env, personal, work := wrongFolder(t)
		personal.SeedSampleIssues()
		cache := config.ProjectCache{Dir: env.CacheDir}
		cache.Store("https://"+personal.Key(), "NSR", false) // the same keys, logged in over https before
		cache.Store("https://"+work.Key(), "NSR", true)
		mustRun(t, env, "issue", "view", "NSR-40", "--json", "idReadable")
		if issueGets(personal) != 1 || issueGets(work) != 0 || strings.Contains(env.Stderr.String(), "using ") {
			t.Errorf("personal got %d issue reads, work %d; stderr = %q", issueGets(personal), issueGets(work), env.Stderr)
		}
	})

	t.Run("given the wrong folder, comment posts the comment to the owner once", func(t *testing.T) {
		env, _, work := wrongFolder(t)
		mustRun(t, env, "issue", "comment", "NSR-40", "-b", "from the wrong folder")
		cs := work.Comments["NSR-40"]
		if len(cs) != 4 || cs[3]["text"] != "from the wrong folder" {
			t.Errorf("work's comments = %v", cs)
		}
	})
}
