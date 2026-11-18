package upgrade_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
	cmdupgrade "github.com/BrainerVirus/youtrack-cli/pkg/cmd/upgrade"
)

type fixture struct {
	env *cmdtest.Env
	srv *upgradetest.Server
	exe string
	sig *upgradetest.Sigstore
	// runs lists the binaries run ("<content>: <args>"); version makes a
	// binary's `version` fail when it returns an error.
	runs    []string
	version func(content string) error
}

const oldBinary = "old ytrack 1.0.0"

// newFixture installs a fake ytrack 1.0.0 at exeRel under a temp dir and
// serves releases from a fake GitHub.
func newFixture(t *testing.T, exeRel string, releases ...*upgradetest.Release) *fixture {
	t.Helper()
	s := upgradetest.NewSigstore(t)
	fx := &fixture{env: cmdtest.New(t), sig: s, srv: upgradetest.NewServer(t, s, releases...)}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fx.env.Factory.AppVersion = "1.0.0"
	fx.exe = filepath.Join(t.TempDir(), filepath.FromSlash(exeRel))
	if err := os.MkdirAll(filepath.Dir(fx.exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fx.exe, []byte(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	return fx
}

func (fx *fixture) run(t *testing.T, args ...string) error {
	t.Helper()
	opts := &cmdupgrade.Options{
		HTTP:        fx.srv.Client(),
		APIURL:      fx.srv.URL,
		TrustedRoot: func() (root.TrustedMaterial, error) { return fx.sig.VS, nil },
		NoSCT:       true, // the virtual Sigstore issues certificates without SCTs
		Executable:  func() (string, error) { return fx.exe, nil },
		GOOS:        "linux",
		GOARCH:      "amd64",
		Sleep:       func(context.Context, time.Duration) error { return nil },
		Run:         fx.runBinary,
	}
	cmd := cmdupgrade.NewCmdUpgrade(fx.env.Factory, opts)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

// runBinary runs a fake binary: its `version` prints the file's content
// (which names the release), and `skill sync` reports success.
func (fx *fixture) runBinary(_ context.Context, exe string, args ...string) ([]byte, error) {
	b, err := os.ReadFile(exe)
	if err != nil {
		return nil, err
	}
	fx.runs = append(fx.runs, strings.TrimSpace(string(b))+": "+strings.Join(args, " "))
	if len(args) == 1 && args[0] == "version" && fx.version != nil {
		if err := fx.version(string(b)); err != nil {
			return []byte("segmentation fault\n"), err
		}
	}
	if len(args) == 2 && args[0] == "skill" && args[1] == "sync" {
		return []byte("refreshed 1 skill copy\n"), nil
	}
	return b, nil
}

func (fx *fixture) binary(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(fx.exe)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpgrade(t *testing.T) {
	t.Run("given a newer signed release, it replaces the binary", func(t *testing.T) {
		rel := &upgradetest.Release{Version: "1.1.0"}
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.0.0"}, rel)
		if err := fx.run(t); err != nil {
			t.Fatalf("%v; stderr: %s", err, fx.env.Stderr)
		}
		if got, want := fx.binary(t), string(rel.BinaryFor("linux/amd64")); got != want {
			t.Errorf("binary = %q, want %q", got, want)
		}
		if !strings.Contains(fx.env.Stderr.String(), "Upgraded ytrack 1.0.0 to 1.1.0") {
			t.Errorf("stderr = %q", fx.env.Stderr)
		}
		if runtime.GOOS != "windows" {
			if fi, _ := os.Stat(fx.exe); fi.Mode().Perm()&0o111 == 0 {
				t.Errorf("new binary is not executable: %v", fi.Mode())
			}
		}
	})

	for name, rel := range map[string]*upgradetest.Release{
		"a wrong checksum":                   {Version: "1.1.0", WrongChecksums: true},
		"a signature over different content": {Version: "1.1.0", SignOther: true},
		"a signature by another workflow":    {Version: "1.1.0", Subject: "https://github.com/BrainerVirus/youtrack-cli/.github/workflows/ci.yml@refs/heads/main"},
		"no signature":                       {Version: "1.1.0", Unsigned: true},
	} {
		t.Run("given "+name+", it refuses and leaves the binary untouched", func(t *testing.T) {
			fx := newFixture(t, "bin/ytrack", rel)
			err := fx.run(t)
			if err == nil {
				t.Fatal("upgrade succeeded, want a refusal")
			}
			if !strings.Contains(err.Error(), "refusing to upgrade") || !strings.Contains(err.Error(), "was not changed") {
				t.Errorf("err = %v", err)
			}
			if got := fx.binary(t); got != oldBinary {
				t.Errorf("binary = %q, want it unchanged", got)
			}
			entries, _ := os.ReadDir(filepath.Dir(fx.exe))
			if len(entries) != 1 {
				t.Errorf("left files next to the binary: %v", entries)
			}
		})
	}

	managers := map[string]string{
		"Homebrew": "opt/homebrew/Caskroom/ytrack/1.0.0/ytrack",
		"Scoop":    "Users/jd/scoop/apps/ytrack/current/ytrack.exe",
		"WinGet":   "Users/jd/AppData/Local/Microsoft/WinGet/Packages/BrainerVirus.ytrack/ytrack.exe",
	}
	for name, path := range managers {
		t.Run("given a binary installed by "+name+", it prints that manager's command and changes nothing", func(t *testing.T) {
			fx := newFixture(t, path, &upgradetest.Release{Version: "1.1.0"})
			if err := fx.run(t); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(fx.env.Stderr.String(), "installed with "+name) || fx.env.Stdout.Len() == 0 {
				t.Errorf("stdout = %q, stderr = %q", fx.env.Stdout, fx.env.Stderr)
			}
			if got := fx.binary(t); got != oldBinary {
				t.Errorf("binary = %q, want it unchanged", got)
			}
			if r := fx.srv.Requests(); len(r) != 0 {
				t.Errorf("made requests %v", r)
			}
		})
	}

	t.Run("given --check and a newer release, it reports it and changes nothing", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0"})
		if err := fx.run(t, "--check"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(fx.env.Stdout.String(), "ytrack 1.1.0 is available (installed: 1.0.0)") {
			t.Errorf("stdout = %q", fx.env.Stdout)
		}
		if fx.binary(t) != oldBinary {
			t.Error("binary changed")
		}
		for _, p := range fx.srv.Requests() {
			if strings.HasPrefix(p, "/download/") {
				t.Errorf("downloaded %s", p)
			}
		}
	})

	t.Run("given the latest release is installed, it does nothing", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.0.0"})
		if err := fx.run(t); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(fx.env.Stdout.String(), "up to date") || fx.binary(t) != oldBinary {
			t.Errorf("stdout = %q", fx.env.Stdout)
		}
	})

	t.Run("given --version, it installs that release", func(t *testing.T) {
		pinned := &upgradetest.Release{Version: "1.0.5"}
		fx := newFixture(t, "bin/ytrack", pinned, &upgradetest.Release{Version: "1.1.0"})
		if err := fx.run(t, "--version", "v1.0.5"); err != nil {
			t.Fatalf("%v; stderr: %s", err, fx.env.Stderr)
		}
		if !bytes.Equal([]byte(fx.binary(t)), pinned.BinaryFor("linux/amd64")) {
			t.Errorf("binary = %q", fx.binary(t))
		}
	})

	t.Run("given --version of a missing release, it fails", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0"})
		if err := fx.run(t, "--version", "9.9.9"); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("err = %v", err)
		}
	})
}

// cdnDown makes every release download URL answer 504, as GitHub's download
// CDN did for one asset of v0.3.1 (issue #21), and serves tampered bytes for
// tamper's object when it is not empty.
func cdnDown(tamper string, data []byte) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case strings.HasPrefix(r.URL.Path, "/download/"):
			w.WriteHeader(http.StatusGatewayTimeout)
			return true
		case tamper != "" && r.URL.Path == upgradetest.ObjectPath("v1.1.0", tamper):
			_, _ = w.Write(data)
			return true
		}
		return false
	}
}

func TestUpgradeDownloadFallback(t *testing.T) {
	t.Run("given the release download URLs answer 504, it upgrades through the GitHub API and says so", func(t *testing.T) {
		rel := &upgradetest.Release{Version: "1.1.0"}
		fx := newFixture(t, "bin/ytrack", rel)
		fx.srv.Handle(cdnDown("", nil))
		if err := fx.run(t); err != nil {
			t.Fatalf("%v; stderr: %s", err, fx.env.Stderr)
		}
		if got, want := fx.binary(t), string(rel.BinaryFor("linux/amd64")); got != want {
			t.Errorf("binary = %q, want %q", got, want)
		}
		stderr := fx.env.Stderr.String()
		for _, name := range []string{upgrade.ChecksumsName("1.1.0"), upgrade.BundleName("1.1.0"), upgrade.ArchiveName("1.1.0", "linux", "amd64")} {
			if !strings.Contains(stderr, "downloaded "+name+" through the GitHub API instead") {
				t.Errorf("stderr does not report the fallback for %s: %q", name, stderr)
			}
		}
	})

	sums := upgrade.ChecksumsName("1.1.0")
	for name, tc := range map[string]struct{ file, data string }{
		"a tampered archive":        {upgrade.ArchiveName("1.1.0", "linux", "amd64"), "tampered archive"},
		"a tampered checksums file": {sums, "0000000000000000000000000000000000000000000000000000000000000000  ytrack_1.1.0_linux_amd64.tar.gz\n"},
		"a tampered signature":      {upgrade.BundleName("1.1.0"), `{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`},
	} {
		t.Run("given "+name+" from the GitHub API fallback, it refuses and leaves the binary untouched", func(t *testing.T) {
			fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0"})
			fx.srv.Handle(cdnDown(tc.file, []byte(tc.data)))
			err := fx.run(t)
			if err == nil || !strings.Contains(err.Error(), "refusing to upgrade") || !strings.Contains(err.Error(), "was not changed") {
				t.Fatalf("err = %v, want a refusal", err)
			}
			if got := fx.binary(t); got != oldBinary {
				t.Errorf("binary = %q, want it unchanged", got)
			}
		})
	}
}

func TestUpgradeHelpNeedsNoNetwork(t *testing.T) {
	env := cmdtest.New(t)
	if code := env.Run("upgrade", "--help"); code != 0 {
		t.Fatalf("exit %d: %s", code, env.Stderr)
	}
	if !strings.Contains(env.Stdout.String(), "--check") {
		t.Errorf("help = %q", env.Stdout)
	}
}

func statePath() string { return filepath.Join(os.Getenv("XDG_STATE_HOME"), "ytrack", "state.yml") }

func TestUpgradeSmokeTestsAndSyncsSkills(t *testing.T) {
	t.Run("given a manual upgrade, it runs the new binary's version before the swap and its skill sync after", func(t *testing.T) {
		rel := &upgradetest.Release{Version: "1.1.0"}
		fx := newFixture(t, "bin/ytrack", rel)
		if err := fx.run(t); err != nil {
			t.Fatalf("%v; stderr: %s", err, fx.env.Stderr)
		}
		newBin := strings.TrimSpace(string(rel.BinaryFor("linux/amd64")))
		want := []string{newBin + ": version", newBin + ": skill sync"}
		if strings.Join(fx.runs, "\n") != strings.Join(want, "\n") {
			t.Errorf("runs = %q, want %q", fx.runs, want)
		}
		if !strings.Contains(fx.env.Stderr.String(), "refreshed 1 skill copy") {
			t.Errorf("stderr = %q", fx.env.Stderr)
		}
	})

	t.Run("given a new binary whose version fails, it refuses and leaves the binary untouched", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0"})
		fx.version = func(string) error { return errors.New("exit status 139") }
		err := fx.run(t)
		if err == nil || !strings.Contains(err.Error(), "refusing to upgrade") || !strings.Contains(err.Error(), "was not changed") {
			t.Fatalf("err = %v", err)
		}
		if got := fx.binary(t); got != oldBinary {
			t.Errorf("binary = %q, want it unchanged", got)
		}
		for _, r := range fx.runs {
			if strings.HasSuffix(r, "skill sync") {
				t.Errorf("ran %q after a refused upgrade", r)
			}
		}
		if entries, _ := os.ReadDir(filepath.Dir(fx.exe)); len(entries) != 1 {
			t.Errorf("left files next to the binary: %v", entries)
		}
	})

	t.Run("given another upgrade holds the lock, a manual upgrade refuses", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0"})
		release, err := upgrade.AcquireLock(filepath.Join(filepath.Dir(statePath()), upgrade.LockFile))
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if err := fx.run(t); err == nil || !strings.Contains(err.Error(), "another ytrack upgrade is running") {
			t.Fatalf("err = %v", err)
		}
		if fx.binary(t) != oldBinary {
			t.Error("binary changed")
		}
	})
}

// fixedNow pins the fixture's clock.
func (fx *fixture) fixedNow(now time.Time) { fx.env.Factory.Now = func() time.Time { return now } }

func TestUpgradeAuto(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

	t.Run("given a release published 2 days ago, it upgrades, records it and syncs the skills", func(t *testing.T) {
		rel := &upgradetest.Release{Version: "1.1.0", PublishedAt: now.Add(-48 * time.Hour)}
		fx := newFixture(t, "bin/ytrack", rel)
		fx.fixedNow(now)
		if err := fx.run(t, "--auto"); err != nil {
			t.Fatalf("%v; stderr: %s", err, fx.env.Stderr)
		}
		if got, want := fx.binary(t), string(rel.BinaryFor("linux/amd64")); got != want {
			t.Errorf("binary = %q, want %q", got, want)
		}
		a := update.ReadState(statePath()).Auto
		if a.From != "1.0.0" || a.To != "1.1.0" || !a.UpgradedAt.Equal(now) || a.Announced || a.Failures != 0 {
			t.Errorf("auto state = %+v", a)
		}
		if len(fx.runs) != 2 || !strings.HasSuffix(fx.runs[1], ": skill sync") {
			t.Errorf("runs = %q, want version then skill sync", fx.runs)
		}
		if upgrade.LockHeld(filepath.Join(filepath.Dir(statePath()), upgrade.LockFile)) {
			t.Error("the lock is still held")
		}
	})

	t.Run("given a candidate whose version fails, the binary is untouched and the failure and backoff are recorded", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0", PublishedAt: now.Add(-48 * time.Hour)})
		fx.fixedNow(now)
		fx.version = func(string) error { return errors.New("exit status 139") }
		if err := fx.run(t, "--auto"); err == nil {
			t.Fatal("want an error")
		}
		if got := fx.binary(t); got != oldBinary {
			t.Errorf("binary = %q, want it untouched", got)
		}
		a := update.ReadState(statePath()).Auto
		if a.Failures != 1 || a.FailedVersion != "1.1.0" || !a.FailedAt.Equal(now) ||
			!strings.Contains(a.Error, "ytrack version") || !a.RetryAt.Equal(now.Add(24*time.Hour)) {
			t.Errorf("auto state = %+v", a)
		}

		// Within the backoff the next start does nothing, not even a request.
		fx.version = nil
		before := len(fx.srv.Requests())
		fx.fixedNow(now.Add(time.Hour))
		if err := fx.run(t, "--auto"); err != nil {
			t.Fatal(err)
		}
		if got := len(fx.srv.Requests()); got != before {
			t.Errorf("made %d requests during the backoff", got-before)
		}
		if fx.binary(t) != oldBinary {
			t.Error("binary changed during the backoff")
		}
		// A second failure after the backoff doubles it.
		fx.version = func(string) error { return errors.New("exit status 139") }
		later := now.Add(25 * time.Hour)
		fx.fixedNow(later)
		_ = fx.run(t, "--auto")
		if a := update.ReadState(statePath()).Auto; a.Failures != 2 || !a.RetryAt.Equal(later.Add(48*time.Hour)) {
			t.Errorf("after a second failure: %+v", a)
		}
	})

	t.Run("given a release published 2 hours ago, it waits and changes nothing", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0", PublishedAt: now.Add(-2 * time.Hour)})
		fx.fixedNow(now)
		if err := fx.run(t, "--auto"); err != nil {
			t.Fatal(err)
		}
		if fx.binary(t) != oldBinary {
			t.Error("binary changed")
		}
		s := update.ReadState(statePath())
		if s.Auto.Failures != 0 || s.LatestVersion != "1.1.0" || !s.LatestPublishedAt.Equal(now.Add(-2*time.Hour)) {
			t.Errorf("state = %+v, want the fresh release cached and no failure", s)
		}
	})

	t.Run("given a package manager owns the binary, it changes nothing and makes no request", func(t *testing.T) {
		fx := newFixture(t, "opt/homebrew/Caskroom/ytrack/1.0.0/ytrack", &upgradetest.Release{Version: "1.1.0"})
		if err := fx.run(t, "--auto"); err != nil {
			t.Fatal(err)
		}
		if fx.binary(t) != oldBinary || len(fx.srv.Requests()) != 0 {
			t.Errorf("binary = %q, requests = %v", fx.binary(t), fx.srv.Requests())
		}
	})

	t.Run("given another upgrade holds the lock, it changes nothing and makes no request", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0"})
		release, err := upgrade.AcquireLock(filepath.Join(filepath.Dir(statePath()), upgrade.LockFile))
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if err := fx.run(t, "--auto"); err != nil {
			t.Fatal(err)
		}
		if fx.binary(t) != oldBinary || len(fx.srv.Requests()) != 0 {
			t.Errorf("binary = %q, requests = %v", fx.binary(t), fx.srv.Requests())
		}
	})

	t.Run("given a manual upgrade after a background failure, the backoff is cleared", func(t *testing.T) {
		fx := newFixture(t, "bin/ytrack", &upgradetest.Release{Version: "1.1.0"})
		update.Update(statePath(), func(s *update.State) {
			s.Auto.RecordAutoFailure("1.1.0", errors.New("boom"), time.Now())
		})
		if err := fx.run(t); err != nil {
			t.Fatal(err)
		}
		if a := update.ReadState(statePath()).Auto; a.Failures != 0 || a.Error != "" || a.To != "" {
			t.Errorf("auto state = %+v", a)
		}
	})
}
