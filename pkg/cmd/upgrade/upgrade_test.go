package upgrade_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
	cmdupgrade "github.com/BrainerVirus/youtrack-cli/pkg/cmd/upgrade"
)

type fixture struct {
	env *cmdtest.Env
	srv *upgradetest.Server
	exe string
	sig *upgradetest.Sigstore
}

const oldBinary = "old ytrack 1.0.0"

// newFixture installs a fake ytrack 1.0.0 at exeRel under a temp dir and
// serves releases from a fake GitHub.
func newFixture(t *testing.T, exeRel string, releases ...*upgradetest.Release) *fixture {
	t.Helper()
	s := upgradetest.NewSigstore(t)
	fx := &fixture{env: cmdtest.New(t), sig: s, srv: upgradetest.NewServer(t, s, releases...)}
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
	}
	cmd := cmdupgrade.NewCmdUpgrade(fx.env.Factory, opts)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
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

func TestUpgradeHelpNeedsNoNetwork(t *testing.T) {
	env := cmdtest.New(t)
	if code := env.Run("upgrade", "--help"); code != 0 {
		t.Fatalf("exit %d: %s", code, env.Stderr)
	}
	if !strings.Contains(env.Stdout.String(), "--check") {
		t.Errorf("help = %q", env.Stdout)
	}
}
