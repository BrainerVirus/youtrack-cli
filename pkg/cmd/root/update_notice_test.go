package root_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
)

// toServer sends every request, whatever its host, to a fake GitHub.
type toServer struct{ srv *upgradetest.Server }

func (r toServer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "https"
	req.URL.Host = r.srv.Listener.Addr().String()
	return r.srv.Client().Transport.RoundTrip(req)
}

// noticeEnv is ytrack 1.0.0 in an interactive terminal, outside CI, with a
// fake GitHub whose latest release is 1.1.0.
func noticeEnv(t *testing.T) (*cmdtest.Env, *upgradetest.Server) {
	t.Helper()
	for _, k := range []string{"CI", "BUILD_NUMBER", "RUN_ID", "YTRACK_NO_UPDATE_NOTIFIER"} {
		t.Setenv(k, "")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := upgradetest.NewServer(t, upgradetest.NewSigstore(t), &upgradetest.Release{Version: "1.0.0"}, &upgradetest.Release{Version: "1.1.0"})
	env := cmdtest.New(t)
	env.Factory.AppVersion = "1.0.0"
	env.Factory.RoundTripper = toServer{srv}
	env.IO.SetStderrTTY(true)
	return env, srv
}

const noticeText = "A new release of ytrack is available: 1.0.0 → 1.1.0"

func TestUpdateNotice(t *testing.T) {
	t.Run("given a newer release and a terminal, it prints one line once per 24h", func(t *testing.T) {
		env, srv := noticeEnv(t)
		if code := env.Run("version"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if got := strings.Count(env.Stderr.String(), noticeText); got != 1 {
			t.Fatalf("stderr = %q, want the notice once", env.Stderr)
		}
		if !strings.Contains(env.Stderr.String(), "run `ytrack upgrade`") {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if strings.Contains(env.Stdout.String(), "new release") {
			t.Errorf("the notice went to stdout: %q", env.Stdout)
		}
		env.Reset()
		if code := env.Run("version"); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if strings.Contains(env.Stderr.String(), noticeText) {
			t.Errorf("second run printed the notice again: %q", env.Stderr)
		}
		if got := len(srv.Requests()); got != 1 {
			t.Errorf("GitHub requests = %d, want 1", got)
		}
		if _, err := os.Stat(filepath.Join(os.Getenv("XDG_STATE_HOME"), "ytrack", "state.yml")); err != nil {
			t.Errorf("state file: %v", err)
		}
	})

	silent := map[string]struct {
		setup func(t *testing.T, env *cmdtest.Env)
		args  []string
	}{
		"CI=1":                            {setup: func(t *testing.T, _ *cmdtest.Env) { t.Setenv("CI", "1") }},
		"a stderr that is not a terminal": {setup: func(_ *testing.T, env *cmdtest.Env) { env.IO.SetStderrTTY(false) }},
		"--json": {
			setup: func(*testing.T, *cmdtest.Env) {},
			args:  []string{"issue", "list", "--json", "idReadable"},
		},
		"YTRACK_NO_UPDATE_NOTIFIER=1": {setup: func(t *testing.T, _ *cmdtest.Env) { t.Setenv("YTRACK_NO_UPDATE_NOTIFIER", "1") }},
		"check_update: false in config.yml": {setup: func(t *testing.T, env *cmdtest.Env) {
			if err := os.WriteFile(filepath.Join(env.ConfigDir, "config.yml"), []byte("check_update: false\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		"a development build": {setup: func(_ *testing.T, env *cmdtest.Env) { env.Factory.AppVersion = "dev" }},
		"the upgrade command": {
			setup: func(*testing.T, *cmdtest.Env) {},
			args:  []string{"upgrade", "--help"},
		},
	}
	for name, tc := range silent {
		t.Run("given "+name+", it prints nothing and makes no network call", func(t *testing.T) {
			env, srv := noticeEnv(t)
			tc.setup(t, env)
			args := tc.args
			if args == nil {
				args = []string{"version"}
			}
			env.Run(args...)
			if strings.Contains(env.Stderr.String(), "new release") {
				t.Errorf("stderr = %q", env.Stderr)
			}
			if r := srv.Requests(); len(r) != 0 {
				t.Errorf("GitHub requests = %v, want none", r)
			}
		})
	}
}
