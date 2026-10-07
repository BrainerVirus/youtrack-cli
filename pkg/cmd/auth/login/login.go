// Package login implements `ytrack auth login`.
package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/auth"
	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

type options struct {
	withToken       bool
	insecureStorage bool
	web             bool
}

// NewCmdLogin returns the login command.
func NewCmdLogin(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a YouTrack host with a permanent token",
		Long: `Log in to a YouTrack host with a permanent token.

Interactively, ytrack asks for the YouTrack URL, opens your account's token
page in the browser and reads the token from a hidden prompt.

With --with-token, the token is read from standard input. Tokens are never
accepted as command-line arguments. YTRACK_TOKEN is a runtime override for CI
and is never saved by login.

The token is verified against the host before it is saved to the OS keyring.
Without a usable keyring, login fails unless --insecure-storage is given, which
saves the token in a plain-text file readable only by you.`,
		Example: `  # interactive
  $ ytrack auth login

  # scripted
  $ ytrack auth login --host acme.youtrack.cloud --with-token < token.txt`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, f, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.withToken, "with-token", false, "Read the token from standard input")
	cmd.Flags().BoolVar(&opts.insecureStorage, "insecure-storage", false, "Save the token in a plain-text file instead of the OS keyring")
	cmd.Flags().BoolVar(&opts.web, "web", false, "Log in with the browser (not yet supported)")
	return cmd
}

// TokenDocs explains how to create a permanent token, including in Hub.
const TokenDocs = "https://www.jetbrains.com/help/youtrack/cloud/manage-permanent-token.html" //nolint:gosec // a URL, not a credential

// TokenPage is where a user creates a permanent token.
func TokenPage(h hosts.Host) string {
	return h.URL + "/users/me?tab=account-security"
}

func run(cmd *cobra.Command, f *cmdutil.Factory, opts *options) error {
	ios := f.IOStreams
	if opts.web {
		return errors.New("browser login (--web) is not supported yet; use a permanent token: ytrack auth login --with-token")
	}
	cfg, err := f.Config()
	if err != nil {
		return err
	}

	host, err := resolveLoginHost(f, opts)
	if err != nil {
		return err
	}

	if os.Getenv("YTRACK_TOKEN") != "" {
		fmt.Fprintln(ios.ErrOut, "! YTRACK_TOKEN is set; it overrides the saved token until you unset it.")
	}

	token, err := readToken(cmd.Context(), f, opts, host)
	if err != nil {
		return err
	}

	user, err := adapter.CurrentUser(cmd.Context(), f.NewClient(host, token))
	if err != nil {
		var apiErr *transport.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
			return clierr.AuthErrorf("%s rejected the token: %v", host.Key, err)
		}
		return fmt.Errorf("could not verify the token with %s: %w", host.URL, err)
	}

	storage, err := f.CredentialStore(cfg).Set(host.Key, token, opts.insecureStorage)
	if errors.Is(err, auth.ErrNoKeyring) {
		return fmt.Errorf("%w\n\nThe token was verified but not saved. Either:\n"+
			"  - export YTRACK_TOKEN for this session, or\n"+
			"  - re-run with --insecure-storage to save it in plain text (mode 0600) under %s", err, cfg.Dir())
	}
	if err != nil {
		return err
	}

	cfg.SetHost(host.Key, config.HostEntry{URL: host.URL, User: user.Login, Auth: "token", Storage: storage})
	if err := cfg.Save(); err != nil {
		return err
	}

	ok, _ := ios.Symbols()
	name := user.Login
	if user.FullName != "" && user.FullName != user.Login {
		name = fmt.Sprintf("%s (%s)", user.Login, user.FullName)
	}
	fmt.Fprintf(ios.ErrOut, "%s Logged in to %s as %s\n", ok, host.Key, name)
	if storage == config.StorageFile {
		fmt.Fprintf(ios.ErrOut, "! Token saved in plain text in %s\n", cfg.Dir())
	}
	if cfg.DefaultHost() != host.Key {
		fmt.Fprintf(ios.ErrOut, "Run `ytrack auth switch --host %s` to make it the default host.\n", host.Key)
	}
	return nil
}

func resolveLoginHost(f *cmdutil.Factory, opts *options) (hosts.Host, error) {
	input := f.HostFlag
	if input == "" {
		input = os.Getenv("YTRACK_HOST")
	}
	if input == "" {
		if opts.withToken || !f.IOStreams.CanPrompt() {
			return hosts.Host{}, clierr.FlagErrorf("--host is required when not running interactively")
		}
		var err error
		input, err = f.Prompter.Input("YouTrack URL (e.g. https://acme.youtrack.cloud):", "")
		if err != nil {
			return hosts.Host{}, err
		}
	}
	h, err := hosts.Parse(input)
	if err != nil {
		return hosts.Host{}, clierr.FlagErrorWrap(err)
	}
	return h, nil
}

func readToken(ctx context.Context, f *cmdutil.Factory, opts *options, host hosts.Host) (string, error) {
	ios := f.IOStreams
	if opts.withToken {
		b, err := io.ReadAll(ios.In)
		if err != nil {
			return "", fmt.Errorf("reading the token from standard input: %w", err)
		}
		t := strings.TrimSpace(string(b))
		if t == "" {
			return "", clierr.FlagErrorf("--with-token: no token on standard input")
		}
		return t, nil
	}
	if !ios.CanPrompt() {
		return "", clierr.FlagErrorf("--with-token is required when not running interactively")
	}

	page := TokenPage(host)
	if tokenPageMissing(ctx, f, host, page) {
		// Server with an external Hub keeps the token page in Hub.
		page = TokenDocs
		fmt.Fprintf(ios.ErrOut, "\n%s has no token page; it probably uses an external Hub.\nSee %s to create a permanent token in Hub.\n", host.Key, TokenDocs)
	}
	fmt.Fprintf(ios.ErrOut, `
Create a permanent token:
  1. Open %s
  2. Under Tokens, click "New token..."
  3. Name it "ytrack" and select the scope "YouTrack"
  4. Create it, copy the token and paste it below

`, page)
	if err := f.Browser.Browse(page); err != nil {
		fmt.Fprintf(ios.ErrOut, "Could not open the browser (%v); open the URL above yourself.\n", err)
	}
	t, err := f.Prompter.Password("Paste your token:")
	if err != nil {
		return "", err
	}
	if t == "" {
		return "", errors.New("no token entered")
	}
	return t, nil
}

// tokenPageMissing reports whether the instance answers 404 for its token
// page. Network errors count as present: the browser can still try.
func tokenPageMissing(ctx context.Context, f *cmdutil.Factory, host hosts.Host, page string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "text/html")
	resp, err := f.NewClient(host, "").Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusNotFound
}
