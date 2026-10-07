// Package cmdtest runs ytrack commands in-process against fake IO, a temp
// config dir, go-keyring's mock and a fake YouTrack server. Test-only.
package cmdtest

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/BrainerVirus/youtrack-cli/internal/browser"
	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
	"github.com/BrainerVirus/youtrack-cli/internal/prompter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/root"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Env is an isolated ytrack environment.
type Env struct {
	t         *testing.T
	IO        *iostreams.IOStreams
	Stdin     *bytes.Buffer
	Stdout    *bytes.Buffer
	Stderr    *bytes.Buffer
	Browser   *browser.Stub
	ConfigDir string
	Factory   *cmdutil.Factory
}

// New returns an Env with no TTYs, an empty config dir, an empty mock keyring
// and every YTRACK_* variable cleared. Tests using it must not run in parallel
// (the keyring mock and environment are process-global).
func New(t *testing.T) *Env {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"YTRACK_HOST", "YTRACK_TOKEN", "YTRACK_DEBUG", "YTRACK_BROWSER", "YTRACK_EDITOR"} {
		t.Setenv(k, "")
	}
	t.Setenv("YTRACK_CONFIG_DIR", dir)
	keyring.MockInit()

	ios, stdin, stdout, stderr := iostreams.Test()
	e := &Env{t: t, IO: ios, Stdin: stdin, Stdout: stdout, Stderr: stderr, Browser: &browser.Stub{}, ConfigDir: dir}
	e.Factory = &cmdutil.Factory{
		IOStreams:  ios,
		Prompter:   prompter.New(stdin, stderr),
		Browser:    e.Browser,
		AppVersion: "test",
		Config:     func() (*config.Config, error) { return config.LoadFrom(dir) },
	}
	return e
}

// Run executes ytrack with args and returns the exit code. Output accumulates
// in Stdout and Stderr; call Reset between runs to inspect one run.
func (e *Env) Run(args ...string) clierr.ExitCode {
	e.t.Helper()
	return root.Execute(e.Factory, args)
}

// Reset clears captured output.
func (e *Env) Reset() {
	e.Stdout.Reset()
	e.Stderr.Reset()
}

// Interactive marks stdin and stdout as terminals so prompts are allowed.
func (e *Env) Interactive() {
	e.IO.SetStdinTTY(true)
	e.IO.SetStdoutTTY(true)
	e.IO.SetStderrTTY(true)
}

// Config loads the config as written to disk.
func (e *Env) Config() *config.Config {
	e.t.Helper()
	c, err := config.LoadFrom(e.ConfigDir)
	if err != nil {
		e.t.Fatalf("loading config: %v", err)
	}
	return c
}

// Login stores token for the fake server's host as `auth login --with-token` would.
func (e *Env) Login(yt *FakeYouTrack) {
	e.t.Helper()
	e.Stdin.WriteString(yt.Token + "\n")
	if code := e.Run("auth", "login", "--host", yt.URL(), "--with-token"); code != 0 {
		e.t.Fatalf("login failed with exit %d: %s", code, e.Stderr.String())
	}
	e.Reset()
}

// IsUsageError reports whether a run failed as a usage error: exit 1 (as in
// gh) with the --help hint on stderr.
func (e *Env) IsUsageError(code clierr.ExitCode) bool {
	return code == clierr.ExitError && strings.Contains(e.Stderr.String(), "--help' for usage")
}

// RouteAllTo sends every HTTP connection to the fake server, whatever host
// the URL names, so tests can use non-loopback host names.
func (e *Env) RouteAllTo(yt *FakeYouTrack) {
	addr := yt.Server.Listener.Addr().String()
	e.Factory.RoundTripper = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}
