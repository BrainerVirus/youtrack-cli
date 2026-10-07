// Package status implements `ytrack auth status`.
package status

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/auth"
	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields lists the --json fields of auth status.
var Fields = []string{"host", "url", "active", "login", "fullName", "token", "tokenSource", "storage", "state", "error"}

// States of a host's credentials, reported as the "state" field.
const (
	StateOK       = "ok"       // the host accepted the token
	StateRejected = "rejected" // HTTP 401, or 403 (token lacks the YouTrack scope)
	StateNoToken  = "no_token" // no token in the environment or the store
	StateError    = "error"    // the check failed otherwise (network, server)
)

type hostStatus struct {
	Host        string
	URL         string
	Active      bool
	Login       string
	FullName    string
	Token       string // masked
	TokenSource string
	Storage     string
	State       string
	Error       string
}

func (s hostStatus) ExportData(fields []string) map[string]any {
	all := map[string]any{
		"host": s.Host, "url": s.URL, "active": s.Active, "login": s.Login,
		"fullName": s.FullName, "token": s.Token, "tokenSource": s.TokenSource,
		"storage": s.Storage, "state": s.State, "error": s.Error,
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = all[f]
	}
	return out
}

// NewCmdStatus returns the status command.
func NewCmdStatus(f *cmdutil.Factory) *cobra.Command {
	var exporter *output.Exporter
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show authentication status for each host",
		Long: `Show which YouTrack hosts ytrack is logged in to, check each token
against its host and show the account it belongs to.

With --host, only that host is checked.

JSON fields:
  host         host key, e.g. acme.youtrack.cloud or tools.acme.com/youtrack
  url          service URL
  active       whether commands use this host by default
  login        account login (when the token works)
  fullName     account full name
  token        the token, masked (e.g. perm-****); use "ytrack auth token" to print it
  tokenSource  where the token came from: YTRACK_TOKEN, keyring or file
  storage      where login saved the token: keyring or file
  state        ok, rejected, no_token or error
  error        why the state is not ok

Exit status: 0 when every token works; 4 when a host has no token or rejects
it (a 403 from /api/users/me means the token lacks the YouTrack scope, which
is treated as rejected); 1 when a check fails for another reason.`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			statuses, err := collect(cmd, f, cfg)
			if err != nil {
				return err
			}
			if exporter != nil {
				if err := exporter.Write(f.IOStreams, statuses); err != nil {
					return err
				}
			} else {
				render(f, statuses)
			}
			return outcome(statuses)
		},
	}
	cmdutil.AddJSONFlags(cmd, &exporter, Fields)
	return cmd
}

func collect(cmd *cobra.Command, f *cmdutil.Factory, cfg *config.Config) ([]hostStatus, error) {
	active, activeErr := f.ResolveHost(cfg)
	envToken := os.Getenv("YTRACK_TOKEN")

	var targets []hosts.Host
	if f.HostFlag != "" {
		if activeErr != nil {
			return nil, activeErr
		}
		targets = []hosts.Host{active}
	} else {
		seen := map[string]bool{}
		for _, key := range cfg.HostKeys() {
			h, err := hosts.Parse(cfg.Host(key).URL)
			if err != nil {
				h = hosts.Host{URL: cfg.Host(key).URL, Key: key}
			}
			targets = append(targets, h)
			seen[h.Key] = true
		}
		// An environment-only host (YTRACK_HOST + YTRACK_TOKEN) is still a login.
		if activeErr == nil && envToken != "" && !seen[active.Key] {
			targets = append(targets, active)
		}
	}
	if len(targets) == 0 {
		return nil, clierr.AuthErrorf("not logged in to any YouTrack host; run `ytrack auth login`")
	}

	store := f.CredentialStore(cfg)
	statuses := make([]hostStatus, 0, len(targets))
	for _, h := range targets {
		s := hostStatus{Host: h.Key, URL: h.URL, Active: activeErr == nil && h.Key == active.Key}
		if e := cfg.Host(h.Key); e != nil {
			s.Storage = e.Storage
		}
		var token, source string
		var err error
		if s.Active {
			token, source, err = f.ResolveToken(cfg, h)
		} else {
			token, source, err = store.Get(h.Key)
		}
		if err != nil {
			return nil, err
		}
		s.TokenSource = source
		if token == "" {
			s.State, s.Error = StateNoToken, "no token stored"
			statuses = append(statuses, s)
			continue
		}
		s.Token = auth.Mask(token)
		user, err := adapter.CurrentUser(cmd.Context(), f.NewClient(h, token))
		var apiErr *transport.APIError
		switch {
		case err == nil:
			s.State, s.Login, s.FullName = StateOK, user.Login, user.FullName
		case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden:
			s.State, s.Error = StateRejected, err.Error()+"; the token may lack the YouTrack scope"
		case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized:
			s.State, s.Error = StateRejected, err.Error()
		default:
			s.State, s.Error = StateError, err.Error()
		}
		statuses = append(statuses, s)
	}
	return statuses, nil
}

func render(f *cmdutil.Factory, statuses []hostStatus) {
	w := f.IOStreams.Out
	ok, fail := f.IOStreams.Symbols()
	for i, s := range statuses {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, s.Host)
		if s.State == StateOK {
			name := s.Login
			if s.FullName != "" && s.FullName != s.Login {
				name = fmt.Sprintf("%s (%s)", s.Login, s.FullName)
			}
			fmt.Fprintf(w, "  %s Logged in as %s\n", ok, name)
		} else {
			fmt.Fprintf(w, "  %s Not authenticated: %s\n", fail, s.Error)
		}
		fmt.Fprintf(w, "  - Active host: %t\n", s.Active)
		fmt.Fprintf(w, "  - URL: %s\n", s.URL)
		if s.Token != "" {
			fmt.Fprintf(w, "  - Token: %s (from %s)\n", s.Token, s.TokenSource)
		}
	}
}

// outcome turns the statuses into the exit code: 4 when a token is missing or
// rejected, 1 for other failures, after the report was printed.
func outcome(statuses []hostStatus) error {
	authFailed, otherFailed := false, false
	for _, s := range statuses {
		switch s.State {
		case StateOK:
		case StateRejected, StateNoToken:
			authFailed = true
		default:
			otherFailed = true
		}
	}
	switch {
	case authFailed:
		return clierr.AuthErrorWrap(clierr.ErrSilent)
	case otherFailed:
		return clierr.ErrSilent
	}
	return nil
}
