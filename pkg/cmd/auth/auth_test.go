package auth_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/BrainerVirus/youtrack-cli/internal/auth"
	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
)

func keyringToken(t *testing.T, hostKey string) string {
	t.Helper()
	v, err := keyring.Get(auth.KeyringService(hostKey), "token")
	if errors.Is(err, keyring.ErrNotFound) {
		return ""
	}
	if err != nil {
		t.Fatalf("keyring.Get: %v", err)
	}
	return v
}

func TestLoginWithToken(t *testing.T) {
	t.Run("given a valid token on stdin, it verifies it and saves it to the keyring", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Stdin.WriteString(yt.Token + "\n")

		code := env.Run("auth", "login", "--host", yt.URL(), "--with-token")

		if code != 0 {
			t.Fatalf("exit %d, stderr: %s", code, env.Stderr)
		}
		req := yt.LastRequest(t)
		if req.Path != "/api/users/me" || req.RawQuery != "fields=login,fullName" {
			t.Errorf("verification request = %s?%s, want /api/users/me?fields=login,fullName", req.Path, req.RawQuery)
		}
		if got := keyringToken(t, yt.Key()); got != yt.Token {
			t.Errorf("keyring holds %q, want the token", got)
		}
		cfg := env.Config()
		if cfg.DefaultHost() != yt.Key() {
			t.Errorf("default host = %q, want %q", cfg.DefaultHost(), yt.Key())
		}
		if h := cfg.Host(yt.Key()); h == nil || h.User != "jdoe" || h.URL != yt.URL() || h.Storage != "keyring" {
			t.Errorf("host entry = %+v", h)
		}
		hostsYML, _ := os.ReadFile(filepath.Join(env.ConfigDir, "hosts.yml"))
		if strings.Contains(string(hostsYML), yt.Token) {
			t.Error("hosts.yml contains the token")
		}
		if !strings.Contains(env.Stderr.String(), "Logged in to "+yt.Key()+" as jdoe (John Doe)") {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if strings.Contains(env.Stdout.String()+env.Stderr.String(), yt.Token) {
			t.Error("the token was printed")
		}
	})

	t.Run("given a server install under a path prefix, it verifies against the prefixed API", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "/youtrack")
		env.Stdin.WriteString(yt.Token)

		if code := env.Run("auth", "login", "--host", yt.URL()+"/", "--with-token"); code != 0 {
			t.Fatalf("exit %d, stderr: %s", code, env.Stderr)
		}
		if p := yt.LastRequest(t).Path; p != "/youtrack/api/users/me" {
			t.Errorf("path = %q", p)
		}
		if got := keyringToken(t, yt.Key()); got != yt.Token {
			t.Errorf("token not stored under ytrack:%s", yt.Key())
		}
	})

	t.Run("given a token the server rejects, it exits 4 and saves nothing", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Stdin.WriteString("perm-wrong")

		code := env.Run("auth", "login", "--host", yt.URL(), "--with-token")

		if code != 4 {
			t.Fatalf("exit %d, want 4; stderr: %s", code, env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "rejected the token") || !strings.Contains(env.Stderr.String(), "You are not logged in.") {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if keyringToken(t, yt.Key()) != "" {
			t.Error("a rejected token was stored")
		}
		if _, err := os.Stat(filepath.Join(env.ConfigDir, "hosts.yml")); !os.IsNotExist(err) {
			t.Error("hosts.yml was written for a failed login")
		}
	})

	t.Run("given an empty stdin, it is a usage error", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		if code := env.Run("auth", "login", "--host", yt.URL(), "--with-token"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
		if len(yt.Requests()) != 0 {
			t.Error("contacted the server without a token")
		}
	})

	t.Run("given no keyring, it fails and points to --insecure-storage without writing a file", func(t *testing.T) {
		env := cmdtest.New(t)
		keyring.MockInitWithError(errors.New("org.freedesktop.secrets not provided"))
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Stdin.WriteString(yt.Token)

		code := env.Run("auth", "login", "--host", yt.URL(), "--with-token")

		if code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		for _, want := range []string{"no usable OS keyring", "--insecure-storage", "YTRACK_TOKEN"} {
			if !strings.Contains(env.Stderr.String(), want) {
				t.Errorf("stderr lacks %q: %s", want, env.Stderr)
			}
		}
		entries, _ := os.ReadDir(env.ConfigDir)
		if len(entries) != 0 {
			t.Errorf("config dir has files after a failed login: %v", entries)
		}
	})

	t.Run("given --insecure-storage, it writes the token to an owner-only file", func(t *testing.T) {
		env := cmdtest.New(t)
		keyring.MockInitWithError(errors.New("no keyring"))
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Stdin.WriteString(yt.Token)

		if code := env.Run("auth", "login", "--host", yt.URL(), "--with-token", "--insecure-storage"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		path := filepath.Join(env.ConfigDir, "credentials.yml")
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), yt.Token) {
			t.Fatalf("credentials.yml = %q, %v", b, err)
		}
		if runtime.GOOS != "windows" {
			if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
				t.Errorf("credentials.yml mode = %v, want 0600", fi.Mode().Perm())
			}
		}
		if !strings.Contains(env.Stderr.String(), "plain text") {
			t.Errorf("no plain-text warning: %s", env.Stderr)
		}
	})

	t.Run("given --web, it explains browser login is not supported yet", func(t *testing.T) {
		env := cmdtest.New(t)
		if code := env.Run("auth", "login", "--web"); code != 1 {
			t.Fatalf("exit %d, want 1", code)
		}
		if !strings.Contains(env.Stderr.String(), "not supported yet") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given no terminal and no --with-token, it refuses to prompt", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		if code := env.Run("auth", "login", "--host", yt.URL()); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})

	t.Run("given YTRACK_TOKEN, login does not save it", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		t.Setenv("YTRACK_TOKEN", yt.Token)
		if code := env.Run("auth", "login", "--host", yt.URL()); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error; stderr: %s", code, env.Stderr)
		}
		if keyringToken(t, yt.Key()) != "" {
			t.Error("YTRACK_TOKEN was saved")
		}
	})
}

func TestLoginInteractive(t *testing.T) {
	t.Run("given a terminal, it asks for the URL, opens the token page and reads a pasted token", func(t *testing.T) {
		env := cmdtest.New(t)
		env.Interactive()
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Stdin.WriteString(yt.URL() + "\n" + yt.Token + "\n")

		code := env.Run("auth", "login")

		if code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		wantPage := yt.URL() + "/users/me?tab=account-security"
		if len(env.Browser.URLs) != 1 || env.Browser.URLs[0] != wantPage {
			t.Errorf("browser opened %v, want %s", env.Browser.URLs, wantPage)
		}
		for _, want := range []string{"YouTrack URL", wantPage, `Name it "ytrack"`, `scope "YouTrack"`, "Paste your token"} {
			if !strings.Contains(env.Stderr.String(), want) {
				t.Errorf("stderr lacks %q:\n%s", want, env.Stderr)
			}
		}
		if keyringToken(t, yt.Key()) != yt.Token {
			t.Error("token not stored")
		}
		if strings.Contains(env.Stdout.String()+env.Stderr.String(), yt.Token) {
			t.Error("the pasted token was echoed")
		}
	})

	t.Run("given a Server whose token page lives in an external Hub, it points to the docs", func(t *testing.T) {
		env := cmdtest.New(t)
		env.Interactive()
		yt := cmdtest.NewFakeYouTrack(t, "")
		yt.HubTokenPage = true
		env.Stdin.WriteString(yt.Token + "\n")

		if code := env.Run("auth", "login", "--host", yt.URL()); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		docs := "https://www.jetbrains.com/help/youtrack/cloud/manage-permanent-token.html"
		if !strings.Contains(env.Stderr.String(), docs) {
			t.Errorf("stderr lacks the docs link:\n%s", env.Stderr)
		}
		if len(env.Browser.URLs) != 1 || env.Browser.URLs[0] != docs {
			t.Errorf("browser opened %v, want the docs", env.Browser.URLs)
		}
	})

	t.Run("given end of input at the URL prompt, it exits as cancelled", func(t *testing.T) {
		env := cmdtest.New(t)
		env.Interactive()
		if code := env.Run("auth", "login"); code != 2 {
			t.Fatalf("exit %d, want 2 (cancelled)", code)
		}
	})
}

type statusJSON struct {
	Host        string `json:"host"`
	Login       string `json:"login"`
	Active      bool   `json:"active"`
	Token       string `json:"token"`
	TokenSource string `json:"tokenSource"`
	Valid       bool   `json:"valid"`
}

func TestStatus(t *testing.T) {
	t.Run("given no login, it exits 4", func(t *testing.T) {
		env := cmdtest.New(t)
		if code := env.Run("auth", "status"); code != 4 {
			t.Fatalf("exit %d, want 4", code)
		}
		if !strings.Contains(env.Stderr.String(), "ytrack auth login") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a valid login, it shows the account and a masked token", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Login(yt)

		if code := env.Run("auth", "status"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		out := env.Stdout.String()
		for _, want := range []string{yt.Key(), "Logged in as jdoe (John Doe)", "Active host: true", "perm-****", "keyring"} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, yt.Token) || strings.Contains(out, "s3cr3t") {
			t.Error("status printed the token")
		}
	})

	t.Run("given --json fields, it prints only those fields", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Login(yt)

		if code := env.Run("auth", "status", "--json", "host,login,valid"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		var got []map[string]any
		if err := json.Unmarshal(env.Stdout.Bytes(), &got); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, env.Stdout)
		}
		want := map[string]any{"host": yt.Key(), "login": "jdoe", "valid": true}
		if len(got) != 1 || len(got[0]) != 3 || got[0]["host"] != want["host"] || got[0]["login"] != want["login"] || got[0]["valid"] != true {
			t.Errorf("got %v, want [%v]", got, want)
		}
	})

	t.Run("given --jq, it filters the JSON", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Login(yt)
		if code := env.Run("auth", "status", "--json", "login", "--jq", ".[0].login"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != "jdoe\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given an unknown --json field, it is a usage error that lists the fields", func(t *testing.T) {
		env := cmdtest.New(t)
		if code := env.Run("auth", "status", "--json", "password"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "tokenSource") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given --jq without --json, it is a usage error", func(t *testing.T) {
		env := cmdtest.New(t)
		if code := env.Run("auth", "status", "--jq", ".[]"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})

	t.Run("given --json with no fields, it is a usage error that lists the fields", func(t *testing.T) {
		env := cmdtest.New(t)
		if code := env.Run("auth", "status", "--json"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "tokenSource") {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a stored token the server now rejects, it reports it and exits 4", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Login(yt)
		yt.Token = "perm-rotated"

		if code := env.Run("auth", "status"); code != 4 {
			t.Fatalf("exit %d, want 4", code)
		}
		if !strings.Contains(env.Stdout.String(), "Not authenticated") {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given YTRACK_HOST and YTRACK_TOKEN only, it reports the environment login", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		t.Setenv("YTRACK_HOST", yt.URL())
		t.Setenv("YTRACK_TOKEN", yt.Token)

		if code := env.Run("auth", "status", "--json", "host,active,tokenSource,valid"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		var got []statusJSON
		_ = json.Unmarshal(env.Stdout.Bytes(), &got)
		if len(got) != 1 || !got[0].Active || got[0].TokenSource != "YTRACK_TOKEN" || !got[0].Valid {
			t.Errorf("got %+v", got)
		}
	})
}

func TestLogout(t *testing.T) {
	t.Run("given a login, it removes the token and the host so status exits 4", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Login(yt)

		if code := env.Run("auth", "logout"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if keyringToken(t, yt.Key()) != "" {
			t.Error("token still in the keyring")
		}
		if env.Config().Host(yt.Key()) != nil {
			t.Error("host still in hosts.yml")
		}
		if code := env.Run("auth", "status"); code != 4 {
			t.Errorf("status after logout exit %d, want 4", code)
		}
	})

	t.Run("given two hosts, logging out of the default promotes the other", func(t *testing.T) {
		env := cmdtest.New(t)
		a, b := cmdtest.NewFakeYouTrack(t, ""), cmdtest.NewFakeYouTrack(t, "")
		env.Login(a)
		env.Login(b)

		if code := env.Run("auth", "logout", "--host", a.URL()); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if d := env.Config().DefaultHost(); d != b.Key() {
			t.Errorf("default host = %q, want %q", d, b.Key())
		}
	})
}

func TestToken(t *testing.T) {
	t.Run("given a login, it prints the token and nothing else", func(t *testing.T) {
		env := cmdtest.New(t)
		yt := cmdtest.NewFakeYouTrack(t, "")
		env.Login(yt)
		if code := env.Run("auth", "token"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if env.Stdout.String() != yt.Token+"\n" {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})

	t.Run("given no login, it exits 4", func(t *testing.T) {
		env := cmdtest.New(t)
		if code := env.Run("auth", "token"); code != 4 {
			t.Fatalf("exit %d, want 4", code)
		}
		if env.Stdout.Len() != 0 {
			t.Errorf("stdout = %q", env.Stdout)
		}
	})
}

func TestSwitch(t *testing.T) {
	setup := func(t *testing.T) (*cmdtest.Env, *cmdtest.FakeYouTrack, *cmdtest.FakeYouTrack) {
		env := cmdtest.New(t)
		a, b := cmdtest.NewFakeYouTrack(t, ""), cmdtest.NewFakeYouTrack(t, "")
		env.Login(a)
		env.Login(b)
		return env, a, b
	}

	t.Run("given --host of a known host, it becomes the default and receives requests", func(t *testing.T) {
		env, a, b := setup(t)
		if env.Config().DefaultHost() != a.Key() {
			t.Fatal("precondition: first login should be the default")
		}
		if code := env.Run("auth", "switch", "--host", b.URL()); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if d := env.Config().DefaultHost(); d != b.Key() {
			t.Errorf("default = %q, want %q", d, b.Key())
		}
		before := len(a.Requests())
		if code := env.Run("api", "/users/me"); code != 0 {
			t.Fatalf("api exit %d: %s", code, env.Stderr)
		}
		if len(a.Requests()) != before || b.LastRequest(t).Path != "/api/users/me" {
			t.Error("api did not go to the new default host")
		}
	})

	t.Run("given an unknown host, it is a usage error", func(t *testing.T) {
		env, _, _ := setup(t)
		if code := env.Run("auth", "switch", "--host", "other.youtrack.cloud"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})

	t.Run("given no --host and no terminal, it is a usage error", func(t *testing.T) {
		env, _, _ := setup(t)
		if code := env.Run("auth", "switch"); !env.IsUsageError(code) {
			t.Fatalf("exit %d, want a usage error (1); stderr: %s", code, env.Stderr)
		}
	})

	t.Run("given a terminal, it lets the user pick the host", func(t *testing.T) {
		env, a, b := setup(t)
		env.Interactive()
		keys := env.Config().HostKeys()
		pick := "1"
		if keys[1] == b.Key() {
			pick = "2"
		}
		env.Stdin.WriteString(pick + "\n")
		if code := env.Run("auth", "switch"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if d := env.Config().DefaultHost(); d != b.Key() {
			t.Errorf("default = %q, want %q (a was %q)", d, b.Key(), a.Key())
		}
	})
}
