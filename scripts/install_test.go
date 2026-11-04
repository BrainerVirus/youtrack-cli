package scripts

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
)

// mirror serves releases 1.0.0 and 1.1.0 (the latest) as a static release
// directory: <url>/download/<tag>/<asset> and <url>/api/releases/latest.
func mirror(t *testing.T, releases ...*upgradetest.Release) (*httptest.Server, []*upgradetest.Release) {
	t.Helper()
	if releases == nil {
		releases = []*upgradetest.Release{{Version: "1.0.0"}, {Version: "1.1.0"}}
	}
	dir := t.TempDir()
	upgradetest.WriteDir(t, dir, upgradetest.NewSigstore(t), releases...)
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	return srv, releases
}

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

type shRun struct {
	home, installDir, cosignLog string
	stderr                      string
	err                         error
}

// runInstallSh runs install.sh with uname reporting osName/machine, cosign
// present only when cosignExit >= 0, and extra environment. "PATH+=dir"
// in env puts dir on PATH.
func runInstallSh(t *testing.T, srv *httptest.Server, osName, machine string, cosignExit int, env ...string) shRun {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for Linux and macOS")
	}
	for _, tool := range []string{"sh", "curl", "tar", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	r := shRun{home: t.TempDir()}
	r.installDir = filepath.Join(r.home, ".local", "bin")
	r.cosignLog = filepath.Join(r.home, "cosign.log")
	shims := t.TempDir()
	writeExe(t, filepath.Join(shims, "uname"), `case "$1" in -s) echo "`+osName+`" ;; -m) echo "`+machine+`" ;; *) echo "`+osName+`" ;; esac`)
	writeExe(t, filepath.Join(shims, "sysctl"), "echo 0")
	// Hide any real cosign: tests choose whether it exists and what it says.
	if cosignExit >= 0 {
		writeExe(t, filepath.Join(shims, "cosign"), `printf '%s\n' "$@" > "`+r.cosignLog+`"; exit `+strconv.Itoa(cosignExit))
	}
	path := shims + string(os.PathListSeparator) + pathWithout("cosign")
	var extra []string
	for _, e := range env {
		if dir, ok := strings.CutPrefix(e, "PATH+="); ok {
			path = dir + string(os.PathListSeparator) + path
			continue
		}
		extra = append(extra, e)
	}

	cmd := exec.Command("sh", "../install.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+r.home,
		"PATH="+path,
		"YTRACK_VERSION=",
		"YTRACK_INSTALL_DIR=",
		"YTRACK_RELEASES_URL="+srv.URL,
		"YTRACK_RELEASES_API_URL="+srv.URL+"/api",
	)
	cmd.Env = append(cmd.Env, extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	r.err = cmd.Run()
	r.stderr = stderr.String()
	return r
}

// pathWithout returns $PATH minus directories that hold tool.
func pathWithout(tool string) string {
	var keep []string
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(d, tool)); err == nil {
			continue
		}
		keep = append(keep, d)
	}
	return strings.Join(keep, string(os.PathListSeparator))
}

func (r shRun) installed(t *testing.T) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.installDir, "ytrack"))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func TestInstallSh(t *testing.T) {
	srv, rels := mirror(t)
	latest := rels[1]

	for _, tc := range []struct{ uname, machine, platform string }{
		{"Linux", "x86_64", "linux/amd64"},
		{"Linux", "aarch64", "linux/arm64"},
		{"Darwin", "arm64", "darwin/arm64"},
		{"Darwin", "x86_64", "darwin/amd64"},
	} {
		t.Run("given "+tc.uname+" "+tc.machine+", it installs the latest "+tc.platform+" build in ~/.local/bin", func(t *testing.T) {
			r := runInstallSh(t, srv, tc.uname, tc.machine, -1)
			if r.err != nil {
				t.Fatalf("%v: %s", r.err, r.stderr)
			}
			got, ok := r.installed(t)
			if !ok || got != string(latest.BinaryFor(tc.platform)) {
				t.Fatalf("installed %q, want the %s build of 1.1.0", got, tc.platform)
			}
			if fi, _ := os.Stat(filepath.Join(r.installDir, "ytrack")); fi.Mode().Perm()&0o111 == 0 {
				t.Errorf("not executable: %v", fi.Mode())
			}
			if !strings.Contains(r.stderr, "is not on your PATH") || !strings.Contains(r.stderr, "export PATH=") {
				t.Errorf("no PATH hint: %s", r.stderr)
			}
		})
	}

	t.Run("given YTRACK_VERSION, it installs that release", func(t *testing.T) {
		r := runInstallSh(t, srv, "Linux", "x86_64", -1, "YTRACK_VERSION=1.0.0")
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		if got, _ := r.installed(t); got != string(rels[0].BinaryFor("linux/amd64")) {
			t.Errorf("installed %q", got)
		}
	})

	t.Run("given cosign, it verifies the signature with the release workflow identity", func(t *testing.T) {
		r := runInstallSh(t, srv, "Linux", "x86_64", 0)
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		args, _ := os.ReadFile(r.cosignLog)
		for _, want := range []string{"verify-blob", "ytrack_1.1.0_checksums.txt.sigstore.json", "https://token.actions.githubusercontent.com", `^https://github\.com/BrainerVirus/youtrack-cli/\.github/workflows/release\.yml@refs/`} {
			if !strings.Contains(string(args), want) {
				t.Errorf("cosign args lack %q:\n%s", want, args)
			}
		}
	})

	t.Run("given a signature cosign rejects, it installs nothing", func(t *testing.T) {
		r := runInstallSh(t, srv, "Linux", "x86_64", 1)
		if r.err == nil {
			t.Fatal("succeeded, want a failure")
		}
		if _, ok := r.installed(t); ok {
			t.Error("installed a binary")
		}
		if !strings.Contains(r.stderr, "does not verify") {
			t.Errorf("stderr = %s", r.stderr)
		}
	})

	t.Run("given an unsupported OS, it fails without installing", func(t *testing.T) {
		r := runInstallSh(t, srv, "FreeBSD", "x86_64", -1)
		if r.err == nil || !strings.Contains(r.stderr, "unsupported operating system") {
			t.Fatalf("err = %v, stderr = %s", r.err, r.stderr)
		}
	})

	t.Run("given YTRACK_INSTALL_DIR on PATH, it installs there with no PATH hint", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "bin")
		r := runInstallSh(t, srv, "Linux", "x86_64", -1, "YTRACK_INSTALL_DIR="+dir, "PATH+="+dir)
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, "ytrack")); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.stderr, "not on your PATH") {
			t.Errorf("stderr = %s", r.stderr)
		}
	})

	t.Run("it never uses sudo", func(t *testing.T) {
		b, err := os.ReadFile("../install.sh")
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") && strings.Contains(line, "sudo") {
				t.Errorf("install.sh:%d runs sudo: %s", i+1, line)
			}
		}
	})
}

func TestInstallShRefusesAWrongChecksum(t *testing.T) {
	srv, _ := mirror(t, &upgradetest.Release{Version: "1.1.0", WrongChecksums: true})
	r := runInstallSh(t, srv, "Linux", "x86_64", -1)
	if r.err == nil {
		t.Fatal("succeeded, want a failure")
	}
	if !strings.Contains(r.stderr, "checksum mismatch") {
		t.Errorf("stderr = %s", r.stderr)
	}
	if _, ok := r.installed(t); ok {
		t.Error("installed a binary")
	}
}

func TestInstallPs1(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not found")
	}
	run := func(t *testing.T, srv *httptest.Server) (string, string, error) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "Programs", "ytrack")
		cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", "../install.ps1")
		// Hide any real cosign so the result does not depend on the machine.
		cmd.Env = append(os.Environ(),
			"PATH="+pathWithout("cosign"),
			"YTRACK_VERSION=",
			"YTRACK_INSTALL_DIR="+dir,
			"YTRACK_RELEASES_URL="+srv.URL,
			"YTRACK_RELEASES_API_URL="+srv.URL+"/api",
		)
		out, err := cmd.CombinedOutput()
		return dir, string(out), err
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		t.Skipf("no Windows build for %s", arch)
	}

	t.Run("given the latest release, it installs ytrack.exe for this CPU", func(t *testing.T) {
		srv, rels := mirror(t)
		dir, out, err := run(t, srv)
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		b, err := os.ReadFile(filepath.Join(dir, "ytrack.exe"))
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		if string(b) != string(rels[1].BinaryFor("windows/"+arch)) {
			t.Errorf("installed %q", b)
		}
		if !strings.Contains(out, "is not on your PATH") {
			t.Errorf("no PATH hint: %s", out)
		}
	})

	t.Run("given a wrong checksum, it installs nothing", func(t *testing.T) {
		srv, _ := mirror(t, &upgradetest.Release{Version: "1.1.0", WrongChecksums: true})
		dir, out, err := run(t, srv)
		if err == nil {
			t.Fatalf("succeeded: %s", out)
		}
		if !strings.Contains(out, "checksum mismatch") {
			t.Errorf("output = %s", out)
		}
		if _, err := os.Stat(filepath.Join(dir, "ytrack.exe")); err == nil {
			t.Error("installed a binary")
		}
	})
}
