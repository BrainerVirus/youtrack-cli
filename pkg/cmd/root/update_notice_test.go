package root_test

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/root"
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
// fake GitHub whose latest release is 1.1.0, published long ago. Background
// upgrades are recorded, never started (see spawnEnv).
func noticeEnv(t *testing.T) (*cmdtest.Env, *upgradetest.Server) {
	t.Helper()
	env, srv, _ := spawnEnv(t, "bin/ytrack")
	return env, srv
}

// spawnEnv is noticeEnv with the running binary at exeRel under a temp dir,
// and the recorder of the background upgrades it starts.
func spawnEnv(t *testing.T, exeRel string) (*cmdtest.Env, *upgradetest.Server, *upgradetest.Spawner) {
	t.Helper()
	for _, k := range []string{"CI", "BUILD_NUMBER", "RUN_ID", "YTRACK_NO_UPDATE_NOTIFIER", "YTRACK_NO_AUTO_UPGRADE"} {
		t.Setenv(k, "")
	}
	exe := filepath.Join(t.TempDir(), filepath.FromSlash(exeRel))
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("ytrack 1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	spawner := upgradetest.FakeSpawn(t, exe)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := upgradetest.NewServer(t, upgradetest.NewSigstore(t), &upgradetest.Release{Version: "1.0.0"}, &upgradetest.Release{Version: "1.1.0"})
	env := cmdtest.New(t)
	env.Factory.AppVersion = "1.0.0"
	env.Factory.RoundTripper = toServer{srv}
	env.IO.SetStderrTTY(true)
	return env, srv, spawner
}

const noticeText = "A new release of ytrack is available: 1.0.0 → 1.1.0"

// slowServer answers after delay, or gives up when the request is cancelled.
type slowServer struct {
	delay time.Duration
	calls chan struct{}
}

func (s slowServer) RoundTrip(req *http.Request) (*http.Response, error) {
	s.calls <- struct{}{}
	select {
	case <-time.After(s.delay):
		return nil, http.ErrServerClosed
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}

func statePath() string { return filepath.Join(os.Getenv("XDG_STATE_HOME"), "ytrack", "state.yml") }

func TestUpdateNoticeNeverDelaysExit(t *testing.T) {
	const grace = 300 * time.Millisecond
	// Slack for a loaded machine under -race; far below the server's 5 s.
	const slack = 1200 * time.Millisecond

	t.Run("given a release server that answers after 5 s and a stale cache, the command exits within the grace", func(t *testing.T) {
		env, _ := noticeEnv(t)
		slow := slowServer{delay: 5 * time.Second, calls: make(chan struct{}, 4)}
		env.Factory.RoundTripper = slow
		began := time.Now()
		if code := env.Run("version"); code != 0 {
			t.Fatalf("exit %d: %s", code, env.Stderr)
		}
		if took := time.Since(began); took > grace+slack {
			t.Fatalf("version took %v with a 5 s release server, want at most ~%v", took, grace)
		}
		if len(slow.calls) != 1 {
			t.Errorf("release checks = %d, want 1 (in the background)", len(slow.calls))
		}
		if strings.Contains(env.Stderr.String(), "new release") {
			t.Errorf("stderr = %q, want no notice without a cached release", env.Stderr)
		}
		if s := update.ReadState(statePath()); !s.CheckedAt.IsZero() {
			t.Errorf("an abandoned check was recorded: %+v", s)
		}
	})

	t.Run("given a cached newer release and a 5 s server, the notice prints and the exit is not delayed", func(t *testing.T) {
		env, _ := noticeEnv(t)
		env.Factory.RoundTripper = slowServer{delay: 5 * time.Second, calls: make(chan struct{}, 4)}
		stale := env.Factory.Clock().Add(-48 * time.Hour)
		if err := update.WriteState(statePath(), update.State{CheckedAt: stale, LatestVersion: "1.1.0", LatestURL: "https://example.test/v1.1.0"}); err != nil {
			t.Fatal(err)
		}
		began := time.Now()
		env.Run("version")
		if took := time.Since(began); took > grace+slack {
			t.Fatalf("version took %v, want at most ~%v", took, grace)
		}
		if got := strings.Count(env.Stderr.String(), noticeText); got != 1 {
			t.Fatalf("stderr = %q, want the cached notice once", env.Stderr)
		}
	})

	t.Run("given a fresh cache with a newer release, it prints from the cache without any network call", func(t *testing.T) {
		env, srv := noticeEnv(t)
		if err := update.WriteState(statePath(), update.State{CheckedAt: env.Factory.Clock(), LatestVersion: "1.1.0"}); err != nil {
			t.Fatal(err)
		}
		env.Run("version")
		if got := strings.Count(env.Stderr.String(), noticeText); got != 1 {
			t.Fatalf("stderr = %q, want the notice once", env.Stderr)
		}
		if r := srv.Requests(); len(r) != 0 {
			t.Errorf("GitHub requests = %v, want none", r)
		}
		env.Reset()
		env.Run("version")
		if strings.Contains(env.Stderr.String(), noticeText) {
			t.Errorf("second run within 24h printed the notice again: %q", env.Stderr)
		}
	})
}

func TestUpdateNotice(t *testing.T) {
	t.Run("given a newer release and a terminal, it prints one line once per 24h", func(t *testing.T) {
		// A fast fake server; widen the grace so a loaded machine cannot
		// abandon the first check.
		t.Cleanup(root.SetNoticeGrace(10 * time.Second))
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
		if _, err := os.Stat(statePath()); err != nil {
			t.Errorf("state file: %v", err)
		}
	})

	silent := map[string]struct {
		setup func(t *testing.T, env *cmdtest.Env)
		args  []string
	}{
		"CI=1":                        {setup: func(t *testing.T, _ *cmdtest.Env) { t.Setenv("CI", "1") }},
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
		"the skill command, which works offline": {
			setup: func(*testing.T, *cmdtest.Env) {},
			args:  []string{"skill", "print"},
		},
	}
	for name, tc := range silent {
		t.Run("given "+name+", it prints nothing and makes no network call", func(t *testing.T) {
			env, srv, spawner := spawnEnv(t, "bin/ytrack")
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
			if c := spawner.Calls(); len(c) != 0 {
				t.Errorf("started background upgrades %v, want none", c)
			}
		})
	}

	// Agents run ytrack without a terminal and with --json: the check still
	// runs for them, only the notice stays quiet.
	quiet := map[string]struct {
		setup func(env *cmdtest.Env)
		args  []string
	}{
		"a stderr that is not a terminal": {setup: func(env *cmdtest.Env) { env.IO.SetStderrTTY(false) }},
		"--json": {
			setup: func(*cmdtest.Env) {},
			args:  []string{"issue", "list", "--json", "idReadable"},
		},
	}
	for name, tc := range quiet {
		t.Run("given "+name+", it checks for a release but prints no notice", func(t *testing.T) {
			t.Cleanup(root.SetNoticeGrace(10 * time.Second))
			env, srv := noticeEnv(t)
			tc.setup(env)
			args := tc.args
			if args == nil {
				args = []string{"version"}
			}
			env.Run(args...)
			if strings.Contains(env.Stderr.String(), "new release") {
				t.Errorf("stderr = %q", env.Stderr)
			}
			if r := srv.Requests(); len(r) != 1 {
				t.Errorf("GitHub requests = %v, want the one release check", r)
			}
			if s := update.ReadState(statePath()); s.LatestVersion != "1.1.0" {
				t.Errorf("state = %+v, want 1.1.0 cached", s)
			}
		})
	}
}

func lockPath() string { return filepath.Join(filepath.Dir(statePath()), upgrade.LockFile) }

// cacheRelease records a check made now that found 1.1.0, published at
// published.
func cacheRelease(t *testing.T, published time.Time, auto update.AutoState) {
	t.Helper()
	s := update.State{CheckedAt: time.Now(), LatestVersion: "1.1.0", LatestURL: "https://example.test/v1.1.0", LatestPublishedAt: published, Auto: auto}
	if err := update.WriteState(statePath(), s); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundUpgrade(t *testing.T) {
	t.Run("given a --json run without a terminal and a stale check, it checks and starts a detached upgrade without waiting", func(t *testing.T) {
		t.Cleanup(root.SetNoticeGrace(10 * time.Second))
		env, srv, spawner := spawnEnv(t, "bin/ytrack")
		env.IO.SetStderrTTY(false)
		stale := time.Now().Add(-48 * time.Hour)
		if err := update.WriteState(statePath(), update.State{CheckedAt: stale, LatestVersion: "1.0.0"}); err != nil {
			t.Fatal(err)
		}
		env.Run("issue", "list", "--json", "idReadable")
		if r := srv.Requests(); len(r) != 1 {
			t.Fatalf("GitHub requests = %v, want the release check", r)
		}
		calls := spawner.Calls()
		if len(calls) != 1 {
			t.Fatalf("background upgrades = %v, want 1", calls)
		}
		c := calls[0]
		if strings.Join(c.Args, " ") != "upgrade --auto" || filepath.Base(c.Exe) != "ytrack" {
			t.Errorf("started %s %v", c.Exe, c.Args)
		}
		if c.LogPath != filepath.Join(filepath.Dir(statePath()), upgrade.LogFile) {
			t.Errorf("log = %s", c.LogPath)
		}
		if e := env.Stderr.String(); strings.Contains(e, "release") || strings.Contains(e, "upgrade") {
			t.Errorf("stderr = %q, want no notice without a terminal", e)
		}
		if s := update.ReadState(statePath()); s.Auto.StartedAt.IsZero() {
			t.Errorf("the start was not recorded: %+v", s.Auto)
		}

		// The next run within the hour does not start another one.
		env.Reset()
		env.Run("version")
		if got := len(spawner.Calls()); got != 1 {
			t.Errorf("background upgrades = %d after a second run, want 1", got)
		}
	})

	t.Run("given a fresh cache with a release 2 days old, a terminal run starts the upgrade too", func(t *testing.T) {
		env, srv, spawner := spawnEnv(t, "bin/ytrack")
		cacheRelease(t, time.Now().Add(-48*time.Hour), update.AutoState{})
		env.Run("version")
		if len(spawner.Calls()) != 1 || len(srv.Requests()) != 0 {
			t.Errorf("background upgrades = %v, requests = %v; want 1 and none", spawner.Calls(), srv.Requests())
		}
	})

	day := 48 * time.Hour
	noSpawn := map[string]struct {
		exe   string
		setup func(t *testing.T, env *cmdtest.Env, exe string)
	}{
		"a Homebrew install": {exe: "opt/homebrew/Caskroom/ytrack/1.0.0/ytrack"},
		"a Scoop install":    {exe: "Users/jd/scoop/apps/ytrack/current/ytrack.exe"},
		"CI=1":               {setup: func(t *testing.T, _ *cmdtest.Env, _ string) { t.Setenv("CI", "1") }},
		"a development build": {setup: func(_ *testing.T, env *cmdtest.Env, _ string) {
			env.Factory.AppVersion = "dev"
		}},
		"auto_upgrade: false in config.yml": {setup: func(t *testing.T, env *cmdtest.Env, _ string) {
			if err := os.WriteFile(filepath.Join(env.ConfigDir, "config.yml"), []byte("auto_upgrade: false\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		"check_update: false in config.yml": {setup: func(t *testing.T, env *cmdtest.Env, _ string) {
			if err := os.WriteFile(filepath.Join(env.ConfigDir, "config.yml"), []byte("check_update: false\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		"YTRACK_NO_AUTO_UPGRADE=1": {setup: func(t *testing.T, _ *cmdtest.Env, _ string) { t.Setenv("YTRACK_NO_AUTO_UPGRADE", "1") }},
		"an upgrade holding the lock": {setup: func(t *testing.T, _ *cmdtest.Env, _ string) {
			release, err := upgrade.AcquireLock(lockPath())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(release)
		}},
		"an active backoff after a failure": {setup: func(t *testing.T, _ *cmdtest.Env, _ string) {
			var a update.AutoState
			a.RecordAutoFailure("1.1.0", errors.New("boom"), time.Now())
			cacheRelease(t, time.Now().Add(-day), a)
		}},
		"a release published 2 hours ago": {setup: func(t *testing.T, _ *cmdtest.Env, _ string) {
			cacheRelease(t, time.Now().Add(-2*time.Hour), update.AutoState{})
		}},
		"an exe directory that is not writable": {setup: func(t *testing.T, _ *cmdtest.Env, exe string) {
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				t.Skip("directory permissions do not stop this user")
			}
			dir := filepath.Dir(exe)
			if err := os.Chmod(dir, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		}},
	}
	for name, tc := range noSpawn {
		t.Run("given "+name+", it starts no background upgrade", func(t *testing.T) {
			exeRel := tc.exe
			if exeRel == "" {
				exeRel = "bin/ytrack"
			}
			env, _, spawner := spawnEnv(t, exeRel)
			env.IO.SetStderrTTY(false)
			cacheRelease(t, time.Now().Add(-day), update.AutoState{})
			if tc.setup != nil {
				exe, _ := upgrade.Executable()
				tc.setup(t, env, exe)
			}
			env.Run("version")
			if c := spawner.Calls(); len(c) != 0 {
				t.Errorf("started background upgrades %v, want none", c)
			}
		})
	}
}

func TestBackgroundUpgradeNotices(t *testing.T) {
	t.Run("given an earlier background failure and a terminal, the notice names the release and the failure", func(t *testing.T) {
		env, _, _ := spawnEnv(t, "bin/ytrack")
		var a update.AutoState
		a.RecordAutoFailure("1.1.0", errors.New("refusing to upgrade: cannot fetch the Sigstore trusted root"), time.Now().Add(-time.Hour))
		cacheRelease(t, time.Now().Add(-48*time.Hour), a)
		env.Run("version")
		stderr := env.Stderr.String()
		if !strings.Contains(stderr, noticeText) {
			t.Errorf("stderr = %q, want the release notice", stderr)
		}
		if !strings.Contains(stderr, "The background upgrade to 1.1.0 failed") ||
			!strings.Contains(stderr, "cannot fetch the Sigstore trusted root") {
			t.Errorf("stderr = %q, want the failure", stderr)
		}
	})

	t.Run("given an earlier background failure without a terminal, it prints nothing", func(t *testing.T) {
		env, _, _ := spawnEnv(t, "bin/ytrack")
		env.IO.SetStderrTTY(false)
		var a update.AutoState
		a.RecordAutoFailure("1.1.0", errors.New("boom"), time.Now())
		cacheRelease(t, time.Now().Add(-48*time.Hour), a)
		env.Run("version")
		if env.Stderr.Len() != 0 {
			t.Errorf("stderr = %q", env.Stderr)
		}
	})

	t.Run("given a background upgrade that succeeded, a terminal run announces it once", func(t *testing.T) {
		env, _, _ := spawnEnv(t, "bin/ytrack")
		env.Factory.AppVersion = "1.1.0"
		var a update.AutoState
		a.RecordSuccess("1.0.0", "1.1.0", time.Now().Add(-time.Hour), true)
		cacheRelease(t, time.Now().Add(-48*time.Hour), a)
		env.Run("version")
		const want = "ytrack upgraded 1.0.0 → 1.1.0 in the background"
		if got := strings.Count(env.Stderr.String(), want); got != 1 {
			t.Fatalf("stderr = %q, want the announcement once", env.Stderr)
		}
		env.Reset()
		env.Run("version")
		if strings.Contains(env.Stderr.String(), want) {
			t.Errorf("announced again: %q", env.Stderr)
		}
	})

	t.Run("given a background upgrade that succeeded and no terminal, it is kept for a terminal run", func(t *testing.T) {
		env, _, _ := spawnEnv(t, "bin/ytrack")
		env.Factory.AppVersion = "1.1.0"
		env.IO.SetStderrTTY(false)
		var a update.AutoState
		a.RecordSuccess("1.0.0", "1.1.0", time.Now(), true)
		cacheRelease(t, time.Now().Add(-48*time.Hour), a)
		env.Run("version")
		if env.Stderr.Len() != 0 {
			t.Errorf("stderr = %q", env.Stderr)
		}
		if s := update.ReadState(statePath()); s.Auto.Announced {
			t.Error("marked announced without a terminal")
		}
	})
}
