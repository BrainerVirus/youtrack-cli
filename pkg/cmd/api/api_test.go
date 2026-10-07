package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/api"
)

func loggedIn(t *testing.T, prefix string) (*cmdtest.Env, *cmdtest.FakeYouTrack) {
	t.Helper()
	env := cmdtest.New(t)
	yt := cmdtest.NewFakeYouTrack(t, prefix)
	env.Login(yt)
	return env, yt
}

func TestNormalizePath(t *testing.T) {
	const base = "https://tools.acme.com/youtrack"
	tests := []struct {
		in, wantPath, wantQuery string
	}{
		{"/issues", "/api/issues", ""},
		{"issues", "/api/issues", ""},
		{"/api/issues", "/api/issues", ""},
		{"api/issues/APP-1", "/api/issues/APP-1", ""},
		{"/api", "/api", ""},
		{"/", "/api", ""},
		{"/issues/", "/api/issues", ""},
		{"/apiary", "/api/apiary", ""},
		{"/issues?fields=id,summary,customFields(name,value(name))", "/api/issues", "fields=id,summary,customFields(name,value(name))"},
		{base + "/api/issues?$top=5", "/api/issues", "$top=5"},
	}
	for _, tt := range tests {
		t.Run(tt.in+" becomes "+tt.wantPath, func(t *testing.T) {
			p, q, err := api.NormalizePath(tt.in, base)
			if err != nil || p != tt.wantPath || q != tt.wantQuery {
				t.Errorf("NormalizePath(%q) = %q, %q, %v; want %q, %q", tt.in, p, q, err, tt.wantPath, tt.wantQuery)
			}
		})
	}

	t.Run("an absolute URL on another host is rejected so the token is not sent there", func(t *testing.T) {
		for _, in := range []string{"https://evil.example/api/issues", base + "evil/api/issues"} {
			if _, _, err := api.NormalizePath(in, base); err == nil {
				t.Errorf("NormalizePath(%q) succeeded", in)
			}
		}
	})
}

func TestAPIRequests(t *testing.T) {
	t.Run("given /issues, issues or /api/issues, it requests /api/issues", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		for _, p := range []string{"/issues", "issues", "/api/issues"} {
			if code := env.Run("api", p); code != 0 {
				t.Fatalf("api %s: exit %d: %s", p, code, env.Stderr)
			}
			if got := yt.LastRequest(t).Path; got != "/api/issues" {
				t.Errorf("api %s requested %s", p, got)
			}
		}
	})

	t.Run("given a server install under a path prefix, it requests under the prefix", func(t *testing.T) {
		env, yt := loggedIn(t, "/youtrack")
		if code := env.Run("api", "/users/me"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got := yt.LastRequest(t).Path; got != "/youtrack/api/users/me" {
			t.Errorf("path = %s", got)
		}
	})

	t.Run("given fields= in the path, it sends the query exactly as written", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		q := "fields=idReadable,summary,customFields(name,value(name))&query=project:%20APP"
		if code := env.Run("api", "/issues?"+q); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got := yt.LastRequest(t).RawQuery; got != q {
			t.Errorf("query = %q, want %q", got, q)
		}
	})

	t.Run("given --fields, it sets the fields query parameter", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("api", "/users/me", "--fields", "login,profiles(general(locale))"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got := yt.LastRequest(t).RawQuery; got != "fields=login,profiles(general(locale))" {
			t.Errorf("query = %q", got)
		}
	})

	t.Run("given --fields and fields= in the path, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/issues?fields=id", "--fields", "summary"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})

	t.Run("given -f on a GET, it adds query parameters", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("api", "/issues", "-X", "GET", "-f", "query=#Unresolved for: me", "-F", "$top=5"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		req := yt.LastRequest(t)
		if req.Method != http.MethodGet || req.RawQuery != "$top=5&query=%23Unresolved+for%3A+me" {
			t.Errorf("got %s ?%s", req.Method, req.RawQuery)
		}
	})

	t.Run("given -F and -f without -X, it POSTs a typed nested JSON body", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		code := env.Run("api", "/issues", "-F", "project[id]=0-0", "-f", "summary=Hello", "-F", "usesMarkdown=true", "-F", "votes=3", "-f", "raw=3")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		req := yt.LastRequest(t)
		if req.Method != http.MethodPost || !strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("got %s with Content-Type %q", req.Method, req.Header.Get("Content-Type"))
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
			t.Fatalf("body %q: %v", req.Body, err)
		}
		project, _ := body["project"].(map[string]any)
		if project["id"] != "0-0" || body["summary"] != "Hello" || body["usesMarkdown"] != true || body["votes"] != float64(3) || body["raw"] != "3" {
			t.Errorf("body = %v", body)
		}
	})

	t.Run("given --input, it sends the file as the body", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		file := filepath.Join(t.TempDir(), "issue.json")
		_ = os.WriteFile(file, []byte(`{"summary":"from file"}`), 0o600)
		if code := env.Run("api", "/issues", "--input", file, "-H", "Content-Type: application/json"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		req := yt.LastRequest(t)
		if req.Method != http.MethodPost || req.Body != `{"summary":"from file"}` {
			t.Errorf("got %s %q", req.Method, req.Body)
		}
		if env.Stdout.String() != `{"summary":"from file"}` {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given -H, it sends the header", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		if code := env.Run("api", "/users/me", "-H", "Accept-Language: de"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got := yt.LastRequest(t).Header.Get("Accept-Language"); got != "de" {
			t.Errorf("Accept-Language = %q", got)
		}
	})

	t.Run("given a header without a colon, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/users/me", "-H", "bogus"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})
}

func TestAPIOutput(t *testing.T) {
	t.Run("given a JSON response and a pipe, it prints the body verbatim to stdout", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/users/me"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		var me map[string]any
		if err := json.Unmarshal(env.Stdout.Bytes(), &me); err != nil || me["login"] != "jdoe" {
			t.Errorf("stdout = %q", env.Stdout)
		}
		if strings.Contains(env.Stdout.String(), "\x1b[") {
			t.Error("color codes in piped output")
		}
	})

	t.Run("given --jq, it prints the filtered values", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedIssues(3)
		if code := env.Run("api", "/issues", "--jq", ".[].idReadable"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != "APP-1\nAPP-2\nAPP-3\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given an invalid --jq expression, it exits 1", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/users/me", "--jq", ".["); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		if !strings.Contains(env.Stderr.String(), "jq") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given --template, it renders the response", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedIssues(2)
		tmpl := `{{range .}}{{.idReadable}}: {{.summary}}{{"\n"}}{{end}}`
		if code := env.Run("api", "/issues", "--template", tmpl); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != "APP-1: Issue 1\nAPP-2: Issue 2\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given --jq and --template together, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/users/me", "--jq", ".", "--template", "x"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})

	t.Run("given --silent, it prints nothing", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/users/me", "--silent"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.Len() != 0 {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given --include, it prints the status line and headers before the body", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/users/me", "--include"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		out := env.Stdout.String()
		if !strings.HasPrefix(out, "HTTP/1.1 200 OK\n") || !strings.Contains(out, "Content-Type: application/json") || !strings.Contains(out, `"login":"jdoe"`) {
			t.Errorf("stdout = %q", out)
		}
	})

	t.Run("given a non-JSON response, it copies the body", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/plain"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != "plain text" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})
}

func TestAPIErrors(t *testing.T) {
	t.Run("given a 404, it shows YouTrack's error on stderr, keeps stdout empty and exits 1", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/issues/NOPE-1"); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		if got := env.Stderr.String(); got != "ytrack: HTTP 404: Entity with id NOPE-1 not found (Not Found)\n" {
			t.Errorf("stderr = %q", got)
		}
		if env.Stdout.Len() != 0 {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given a token the server rejects, it exits 4", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.Token = "perm-rotated"
		if code := env.Run("api", "/users/me"); code != 4 {
			t.Fatalf("exit %d, want 4", code)
		}
		if !strings.Contains(env.Stderr.String(), "You are not logged in.") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given no login, it exits 4 without a request", func(t *testing.T) {
		env := cmdtest.New(t)
		if code := env.Run("api", "/users/me"); code != 4 {
			t.Fatalf("exit %d, want 4", code)
		}
	})

	t.Run("given YTRACK_HOST and YTRACK_TOKEN, it works without a stored login", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		t.Setenv("YTRACK_HOST", yt.URL())
		t.Setenv("YTRACK_TOKEN", yt.Token)
		if code := env.Run("api", "/users/me", "--jq", ".login"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != "jdoe\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given no path, it is a usage error", func(t *testing.T) {
		env := cmdtest.New(t)
		if code := env.Run("api"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})
}

func TestAPIPaginate(t *testing.T) {
	t.Run("given 250 issues, it fetches every page and prints one array", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedIssues(250)
		if code := env.Run("api", "/issues?fields=idReadable", "--paginate"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		var items []map[string]any
		if err := json.Unmarshal(env.Stdout.Bytes(), &items); err != nil {
			t.Fatalf("stdout is not one JSON array: %v", err)
		}
		if len(items) != 250 || items[0]["idReadable"] != "APP-1" || items[249]["idReadable"] != "APP-250" {
			t.Errorf("got %d items", len(items))
		}
		var queries []string
		for _, r := range yt.Requests() {
			if r.Path == "/api/issues" {
				queries = append(queries, r.RawQuery)
			}
		}
		want := []string{
			"fields=idReadable&$skip=0&$top=100",
			"fields=idReadable&$skip=100&$top=100",
			"fields=idReadable&$skip=200&$top=100",
		}
		if strings.Join(queries, " ") != strings.Join(want, " ") {
			t.Errorf("queries = %v, want %v", queries, want)
		}
	})

	t.Run("given $top and $skip in the path, they set the page size and start", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedIssues(9)
		if code := env.Run("api", "/issues?$skip=2&$top=4", "--paginate", "--jq", "length"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != "7\n" {
			t.Errorf("stdout = %q, want 7 items", env.Stdout)
		}
		if n := len(yt.Requests()) - 1; n != 2 { // minus the login verification
			t.Errorf("made %d page requests, want 2", n)
		}
	})

	t.Run("given an exact multiple of the page size, it stops at the empty page", func(t *testing.T) {
		env, yt := loggedIn(t, "")
		yt.SeedIssues(200)
		if code := env.Run("api", "/issues", "--paginate", "--jq", "length"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != "200\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given an endpoint that returns an object, it fails clearly", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/users/me", "--paginate"); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		if !strings.Contains(env.Stderr.String(), "JSON array") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a non-GET method, it is a usage error", func(t *testing.T) {
		env, _ := loggedIn(t, "")
		if code := env.Run("api", "/issues", "-X", "POST", "--paginate"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})
}

func TestAPIDebug(t *testing.T) {
	for _, mode := range []string{"--debug", "YTRACK_DEBUG=1"} {
		t.Run("given "+mode+", it traces requests on stderr and never prints the token", func(t *testing.T) {
			env, yt := loggedIn(t, "")
			args := []string{"api", "/users/me"}
			if mode == "--debug" {
				args = append(args, "--debug")
			} else {
				t.Setenv("YTRACK_DEBUG", "1")
			}
			if code := env.Run(args...); code != 0 {
				t.Fatalf("exit %d: %s", code, env.Stderr)
			}
			stderr := env.Stderr.String()
			for _, want := range []string{"> GET " + yt.URL() + "/api/users/me", "> Authorization: Bearer [REDACTED]", "< HTTP/1.1 200 OK"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr)
				}
			}
			if strings.Contains(stderr, yt.Token) || strings.Contains(stderr, "s3cr3t") {
				t.Errorf("debug output leaked the token:\n%s", stderr)
			}
			if !strings.Contains(env.Stdout.String(), "jdoe") {
				t.Errorf("stdout = %q", env.Stdout)
			}
		})
	}
}
