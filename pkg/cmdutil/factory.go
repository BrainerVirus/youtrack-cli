// Package cmdutil holds what commands share: the dependency Factory, argument
// validators and the --json/--jq/--template flags.
package cmdutil

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/auth"
	"github.com/BrainerVirus/youtrack-cli/internal/browser"
	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
	"github.com/BrainerVirus/youtrack-cli/internal/prompter"
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

	// Bound to the root command's persistent flags.
	HostFlag string
	Debug    bool

	warned map[string]bool
	// urlHost is the service URL of an issue URL given as an argument.
	urlHost string
	// explicitHost is the key of the host named by --host or $YTRACK_HOST.
	explicitHost string
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

// ResolveHost picks the host to talk to: --host, then the host of an issue
// URL argument, then $YTRACK_HOST, then the default host from hosts.yml. A
// known host keeps its stored URL (scheme and path prefix); an unknown one
// is normalized from the input.
func (f *Factory) ResolveHost(cfg *config.Config) (hosts.Host, error) {
	input, explicit := f.HostFlag, true
	if input == "" {
		input, explicit = f.urlHost, false
	}
	if input == "" {
		input, explicit = os.Getenv("YTRACK_HOST"), true
	}
	if input == "" {
		input, explicit = cfg.DefaultHost(), false
	}
	if input == "" {
		return hosts.Host{}, clierr.AuthErrorf("no YouTrack host configured; run `ytrack auth login` or set YTRACK_HOST")
	}
	h, err := hosts.Parse(input)
	if err != nil {
		return hosts.Host{}, clierr.FlagErrorWrap(err)
	}
	if explicit {
		f.explicitHost = h.Key
	}
	if e := cfg.Host(h.Key); e != nil && e.URL != "" {
		if stored, err := hosts.Parse(e.URL); err == nil {
			return stored, nil
		}
	}
	return h, nil
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
	if host.PlainHTTP() {
		f.Warnf("%s uses plain http; requests and tokens are sent unencrypted", host.URL)
	}
	opts := transport.Options{
		BaseURL:      host.URL,
		Token:        token,
		UserAgent:    "ytrack/" + f.AppVersion,
		RoundTripper: f.RoundTripper,
	}
	if f.DebugEnabled() {
		opts.DebugLog = f.IOStreams.ErrOut
	}
	return transport.New(opts)
}

// APIClient resolves the host and token and returns a client for them. It
// fails with an auth error (exit 4) when either is missing.
func (f *Factory) APIClient() (*transport.Client, hosts.Host, error) {
	cfg, err := f.Config()
	if err != nil {
		return nil, hosts.Host{}, err
	}
	host, err := f.ResolveHost(cfg)
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
	return f.NewClient(host, token), host, nil
}
