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
		// Completions follow $SHELL and the XDG dirs; tests choose them.
		"SHELL=/bin/sh",
		"XDG_DATA_HOME=",
		"XDG_CONFIG_HOME=",
		"YTRACK_NO_COMPLETIONS=",
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

func TestInstallShCompletions(t *testing.T) {
	srv, _ := mirror(t)
	// fakeZsh is a zsh whose $fpath is /usr/share/zsh/functions plus, when
	// withHomeDir is set, ~/.zfunc.
	fakeZsh := func(t *testing.T, withHomeDir bool) string {
		dir := t.TempDir()
		body := `printf '%s\n' /usr/share/zsh/functions`
		if withHomeDir {
			body += ` "$HOME/.zfunc"`
		}
		writeExe(t, filepath.Join(dir, "zsh"), body)
		return "PATH+=" + dir
	}
	read := func(t *testing.T, path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("completion not installed: %v", err)
		}
		return string(b)
	}

	t.Run("given zsh, it installs _ytrack in the XDG site-functions dir and says how to add it to $fpath", func(t *testing.T) {
		r := runInstallSh(t, srv, "Linux", "x86_64", -1, "SHELL=/usr/bin/zsh", fakeZsh(t, false))
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		dest := filepath.Join(r.home, ".local", "share", "zsh", "site-functions", "_ytrack")
		if got := read(t, dest); got != "#compdef ytrack\n" {
			t.Errorf("_ytrack = %q", got)
		}
		for _, want := range []string{"Installed zsh completions to " + dest, `fpath=("` + filepath.Dir(dest) + `" $fpath)`, "compinit"} {
			if !strings.Contains(r.stderr, want) {
				t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
			}
		}
	})

	t.Run("given zsh with a writable $fpath dir under $HOME, it installs there", func(t *testing.T) {
		zsh := fakeZsh(t, true)
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".zfunc"), 0o755); err != nil {
			t.Fatal(err)
		}
		r := runInstallSh(t, srv, "Linux", "x86_64", -1, "SHELL=/bin/zsh", zsh, "HOME="+home)
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		read(t, filepath.Join(home, ".zfunc", "_ytrack"))
		if !strings.Contains(r.stderr, "new zsh sessions load them") {
			t.Errorf("stderr = %s", r.stderr)
		}
	})

	t.Run("given bash, it installs into bash-completion's user dir", func(t *testing.T) {
		r := runInstallSh(t, srv, "Linux", "x86_64", -1, "SHELL=/bin/bash")
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		dest := filepath.Join(r.home, ".local", "share", "bash-completion", "completions", "ytrack")
		if got := read(t, dest); got != "# ytrack bash\n" {
			t.Errorf("ytrack = %q", got)
		}
		if !strings.Contains(r.stderr, "Installed bash completions to "+dest) || !strings.Contains(r.stderr, "~/.bashrc") {
			t.Errorf("stderr = %s", r.stderr)
		}
	})

	t.Run("given fish, it installs into ~/.config/fish/completions", func(t *testing.T) {
		r := runInstallSh(t, srv, "Darwin", "arm64", -1, "SHELL=/opt/homebrew/bin/fish")
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		dest := filepath.Join(r.home, ".config", "fish", "completions", "ytrack.fish")
		if got := read(t, dest); got != "# ytrack fish\n" {
			t.Errorf("ytrack.fish = %q", got)
		}
		if !strings.Contains(r.stderr, "fish loads them") {
			t.Errorf("stderr = %s", r.stderr)
		}
	})

	t.Run("given XDG_DATA_HOME, bash completions go there", func(t *testing.T) {
		data := t.TempDir()
		r := runInstallSh(t, srv, "Linux", "x86_64", -1, "SHELL=/bin/bash", "XDG_DATA_HOME="+data)
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		read(t, filepath.Join(data, "bash-completion", "completions", "ytrack"))
	})

	t.Run("given YTRACK_NO_COMPLETIONS=1, it installs none", func(t *testing.T) {
		r := runInstallSh(t, srv, "Linux", "x86_64", -1, "SHELL=/bin/bash", "YTRACK_NO_COMPLETIONS=1")
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		if _, err := os.Stat(filepath.Join(r.home, ".local", "share", "bash-completion")); err == nil {
			t.Error("completions were installed")
		}
	})

	t.Run("given another shell, it points at ytrack completion", func(t *testing.T) {
		r := runInstallSh(t, srv, "Linux", "x86_64", -1, "SHELL=/bin/tcsh")
		if r.err != nil {
			t.Fatalf("%v: %s", r.err, r.stderr)
		}
		if !strings.Contains(r.stderr, "ytrack completion --help") {
			t.Errorf("stderr = %s", r.stderr)
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
		if !strings.Contains(out, "ytrack completion powershell | Out-String | Invoke-Expression") {
			t.Errorf("no PowerShell completion instructions: %s", out)
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
