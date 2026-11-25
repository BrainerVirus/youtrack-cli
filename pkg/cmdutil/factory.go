// Package cmdutil holds what commands share: the dependency Factory, argument
// validators and the --json/--jq/--template flags.
package cmdutil

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/auth"
	"github.com/BrainerVirus/youtrack-cli/internal/browser"
	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
	"github.com/BrainerVirus/youtrack-cli/internal/prompter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

// Factory carries the dependencies commands use instead of globals, so tests
// can swap IO, config, browser, prompts and HTTP.
type Factory struct {
	IOStreams  *iostreams.IOStreams
	Prompter   prompter.Prompter
	Browser    browser.Browser
	AppVersion string
	Config     func() (*config.Config, error)
	// RoundTripper replaces the HTTP transport when non-nil (tests).
	RoundTripper http.RoundTripper
	// Now returns the current time; nil means time.Now (tests set it).
	Now func() time.Time
	// WorkDir returns the folder host paths are matched against; nil means
	// the process's working directory (tests set it).
	WorkDir func() (string, error)

	// Bound to the root command's persistent flags.
	HostFlag string
	Debug    bool

	warned map[string]bool
	// urlHost is the service URL of an issue URL given as an argument.
	urlHost string
	// explicitHost is the key of the host named by --host or $YTRACK_HOST.
	explicitHost string
	// keyHost is the host that owns the issue key's project (see
	// WithIssueHost), and keyProject that project.
	keyHost, keyProject string
}

// Clock returns the current time from Now, or time.Now.
func (f *Factory) Clock() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// UseURLHost makes serviceURL, taken from an issue URL argument, the host to
// talk to when neither --host nor $YTRACK_HOST names one.
func (f *Factory) UseURLHost(serviceURL string) { f.urlHost = serviceURL }

// Warnf prints a "! " warning to stderr once per distinct message.
func (f *Factory) Warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if f.warned == nil {
		f.warned = map[string]bool{}
	}
	if f.warned[msg] {
		return
	}
	f.warned[msg] = true
	fmt.Fprintf(f.IOStreams.ErrOut, "! %s\n", msg)
}

// CredentialStore returns the token store for the config directory.
func (f *Factory) CredentialStore(cfg *config.Config) auth.Store {
	return auth.Store{Dir: cfg.Dir()}
}

// Sources of the host a command talks to, as HostResolution reports them.
const (
	HostFromFlag    = "flag"    // --host
	HostFromURL     = "url"     // an issue URL argument
	HostFromEnv     = "env"     // $YTRACK_HOST
	HostFromKey     = "key"     // the logged-in host that has the issue key's project
	HostFromPath    = "path"    // a hosts.yml path glob matching the working directory
	HostFromDefault = "default" // default_host in hosts.yml
	// HostFromMainCheckout is a hosts.yml path glob matching the main
	// checkout of the linked git worktree the working directory is in.
	HostFromMainCheckout = "path (main checkout)"
)

// Resolution says why a host was picked.
type Resolution struct {
	// Source is one of the HostFrom* values.
	Source string
	// Glob is the matching path glob when Source is HostFromPath or
	// HostFromMainCheckout.
	Glob string
	// Project is the issue key's project when Source is HostFromKey.
	Project string
}

// Why completes "<host> chosen ..." for messages that explain the choice.
func (r Resolution) Why() string {
	switch r.Source {
	case HostFromFlag:
		return "by --host"
	case HostFromURL:
		return "by the issue URL"
	case HostFromEnv:
		return "by YTRACK_HOST"
	case HostFromKey:
		return "because it has project " + r.Project
	case HostFromPath:
		return "by the folder rule " + r.Glob
	case HostFromMainCheckout:
		return "by the folder rule " + r.Glob + " (this worktree's main checkout)"
	}
	return "as the default host"
}

// ResolveHost picks the host to talk to (see HostResolution).
func (f *Factory) ResolveHost(cfg *config.Config) (hosts.Host, error) {
	h, _, err := f.HostResolution(cfg)
	return h, err
}

// HostResolution picks the host to talk to and says why: --host, then the
// host of an issue URL argument, then $YTRACK_HOST, then the logged-in host
// that owns the issue key's project (see WithIssueHost), then the logged-in
// host whose path glob matches the working directory (see folderRule), then the default host
// from hosts.yml. A known host keeps its stored URL (scheme and path prefix); an
// unknown one is normalized from the input.
func (f *Factory) HostResolution(cfg *config.Config) (hosts.Host, Resolution, error) {
	var res Resolution
	input := ""
	for _, c := range []struct{ source, value string }{
		{HostFromFlag, f.HostFlag},
		{HostFromURL, f.urlHost},
		{HostFromEnv, os.Getenv("YTRACK_HOST")},
	} {
		if c.value != "" {
			input, res.Source = c.value, c.source
			break
		}
	}
	if input == "" && f.keyHost != "" {
		input, res = f.keyHost, Resolution{Source: HostFromKey, Project: f.keyProject}
	}
	if input == "" {
		m, source, ok, err := f.folderRule(cfg)
		if err != nil {
			return hosts.Host{}, Resolution{}, err
		}
		if ok {
			input, res = m.Host, Resolution{Source: source, Glob: m.Glob}
		}
	}
	if input == "" {
		input, res.Source = cfg.DefaultHost(), HostFromDefault
	}
	if input == "" {
		return hosts.Host{}, Resolution{}, clierr.AuthErrorf("no YouTrack host configured; run `ytrack auth login` or set YTRACK_HOST")
	}
	h, err := hosts.Parse(input)
	if err != nil {
		return hosts.Host{}, Resolution{}, clierr.FlagErrorWrap(err)
	}
	if res.Source == HostFromFlag || res.Source == HostFromEnv {
		f.explicitHost = h.Key
	}
	if e := cfg.Host(h.Key); e != nil && e.URL != "" {
		if stored, err := hosts.Parse(e.URL); err == nil {
			return stored, res, nil
		}
	}
	return h, res, nil
}

// folderRule returns the host whose path glob matches the working
// directory, with HostFromPath as its source. When none does and the working
// directory is inside a linked git worktree, the globs are matched against
// the worktree's main checkout instead (HostFromMainCheckout), so a worktree
// kept outside the repository's folder still gets the repository's host.
func (f *Factory) folderRule(cfg *config.Config) (config.PathMatch, string, bool, error) {
	dirs := f.workDirs()
	m, ok, err := cfg.HostForPath(dirs...)
	if err != nil || ok {
		return m, HostFromPath, ok, err
	}
	main := mainCheckoutDirs(dirs)
	if len(main) == 0 {
		return config.PathMatch{}, "", false, nil
	}
	m, ok, err = cfg.HostForPath(main...)
	return m, HostFromMainCheckout, ok, err
}

// workDirs returns the working directory and, when it differs, its path
// with symbolic links resolved; nil when it cannot be read.
func (f *Factory) workDirs() []string {
	getwd := f.WorkDir
	if getwd == nil {
		getwd = os.Getwd
	}
	wd, err := getwd()
	if err != nil || wd == "" {
		return nil
	}
	dirs := []string{wd}
	if real, err := filepath.EvalSymlinks(wd); err == nil && real != wd {
		dirs = append(dirs, real)
	}
	return dirs
}

// ResolveToken returns the token for host and where it came from:
// $YTRACK_TOKEN wins over stored credentials. It returns "" when there is none.
// It warns when YTRACK_TOKEN goes to a host that is not in hosts.yml, unless
// the user named that host with --host or $YTRACK_HOST (the usual CI setup).
func (f *Factory) ResolveToken(cfg *config.Config, host hosts.Host) (token, source string, err error) {
	if t := os.Getenv("YTRACK_TOKEN"); t != "" {
		if cfg.Host(host.Key) == nil && host.Key != f.explicitHost {
			f.Warnf("sending YTRACK_TOKEN to %s, which is not a logged-in host in hosts.yml", host.Key)
		}
		return t, auth.SourceEnv, nil
	}
	return f.CredentialStore(cfg).Get(host.Key)
}

// DebugEnabled reports whether --debug or $YTRACK_DEBUG asks for HTTP traces.
func (f *Factory) DebugEnabled() bool {
	if f.Debug {
		return true
	}
	v := os.Getenv("YTRACK_DEBUG")
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return v != "" && v != "0"
}

// NewClient returns an API client for host using token. It warns when the
// host is reached over plain http and is not loopback.
func (f *Factory) NewClient(host hosts.Host, token string) *transport.Client {
	return f.newClient(host, token, nil)
}

// origin names host and why res picked it, for API errors; nil when host is
// the only host in play. It names no other host: only WithIssueHost, which
// knows which hosts own an issue key's project, suggests one.
func origin(cfg *config.Config, host hosts.Host, res Resolution) *transport.Origin {
	if len(otherHosts(cfg, host.Key)) == 0 {
		return nil
	}
	return &transport.Origin{Host: host.Key, Why: res.Why()}
}

// otherHosts returns the logged-in host keys other than key.
func otherHosts(cfg *config.Config, key string) []string {
	var out []string
	for _, k := range cfg.HostKeys() {
		if k != key {
			out = append(out, k)
		}
	}
	return out
}

func (f *Factory) newClient(host hosts.Host, token string, o *transport.Origin) *transport.Client {
	if host.PlainHTTP() {
		f.Warnf("%s uses plain http; requests and tokens are sent unencrypted", host.URL)
	}
	opts := transport.Options{
		BaseURL:      host.URL,
		Token:        token,
		UserAgent:    "ytrack/" + f.AppVersion,
		RoundTripper: f.RoundTripper,
		Origin:       o,
	}
	if f.DebugEnabled() {
		opts.DebugLog = f.IOStreams.ErrOut
	}
	return transport.New(opts)
}

// CurrentLogin returns the login of the account client talks as on host. The
// login `auth login` saved in hosts.yml is the per-host cache while the
// stored token is in use; with $YTRACK_TOKEN, or without a saved login, it
// asks GET /api/users/me.
func (f *Factory) CurrentLogin(ctx context.Context, client *transport.Client, host hosts.Host) (string, error) {
	if os.Getenv("YTRACK_TOKEN") == "" {
		if cfg, err := f.Config(); err == nil {
			if e := cfg.Host(host.Key); e != nil && e.User != "" {
				return e.User, nil
			}
		}
	}
	u, err := adapter.CurrentUser(ctx, client)
	if err != nil {
		return "", err
	}
	return u.Login, nil
}

// APIClient resolves the host and token and returns a client for them. It
// fails with an auth error (exit 4) when either is missing.
func (f *Factory) APIClient() (*transport.Client, hosts.Host, error) {
	cfg, err := f.Config()
	if err != nil {
		return nil, hosts.Host{}, err
	}
	host, res, err := f.HostResolution(cfg)
	if err != nil {
		return nil, hosts.Host{}, err
	}
	token, _, err := f.ResolveToken(cfg, host)
	if err != nil {
		return nil, hosts.Host{}, err
	}
	if token == "" {
		return nil, hosts.Host{}, clierr.AuthErrorf("not logged in to %s; run `ytrack auth login --host %s` or set YTRACK_TOKEN", host.Key, host.Key)
	}
	return f.newClient(host, token, origin(cfg, host, res)), host, nil
}
