// Package upgrade implements `ytrack upgrade`.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Options are the command's dependencies; tests replace them.
type Options struct {
	HTTP   *http.Client
	APIURL string
	// TrustedRoot returns the Sigstore trusted material signatures must
	// chain to; NoSCT is for test Sigstores only.
	TrustedRoot func() (root.TrustedMaterial, error)
	NoSCT       bool
	// Executable returns the path of the running binary.
	Executable func() (string, error)
	GOOS       string
	GOARCH     string
	// Sleep waits between download retries; nil waits for real.
	Sleep func(ctx context.Context, d time.Duration) error
}

func defaults(f *cmdutil.Factory) *Options {
	return &Options{
		HTTP:        &http.Client{Transport: f.RoundTripper, Timeout: 5 * time.Minute},
		TrustedRoot: upgrade.PublicGoodTrustedRoot,
		Executable:  os.Executable,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
	}
}

// NewCmdUpgrade returns the upgrade command. opts nil means the real
// GitHub releases, trust root and executable.
func NewCmdUpgrade(f *cmdutil.Factory, opts *Options) *cobra.Command {
	var check bool
	var version string
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade ytrack to the latest release",
		Long: `Upgrade ytrack to the latest release, or to the release named by --version.

The release's checksums file must carry a Sigstore (cosign keyless) signature
made by this repository's release workflow on GitHub Actions, and the archive
must match its SHA-256 in that file. Only then is the running binary replaced,
atomically. Any failed check refuses the upgrade and changes nothing.

A download that fails with a network error, HTTP 5xx or 429 is retried
(3 attempts). If the release download URL still fails, the file is fetched
through the GitHub API instead, with the same checks.

When Homebrew, Scoop or WinGet installed ytrack, upgrade prints that package
manager's command instead and changes nothing.`,
		Example: `  $ ytrack upgrade
  $ ytrack upgrade --check
  $ ytrack upgrade --version v0.3.0`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := opts
			if o == nil {
				o = defaults(f)
			}
			return run(cmd.Context(), f, o, check, version)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Only report whether a newer release exists")
	cmd.Flags().StringVar(&version, "version", "", "Install this release `tag` instead of the latest")
	return cmd
}

func run(ctx context.Context, f *cmdutil.Factory, o *Options, check bool, version string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	out, errOut := f.IOStreams.Out, f.IOStreams.ErrOut
	exe, err := o.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the ytrack binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	if m := update.DetectManager(exe, resolved); m != nil {
		fmt.Fprintf(errOut, "ytrack was installed with %s; upgrade it with:\n", m.Name)
		fmt.Fprintf(out, "%s\n", m.Command)
		return nil
	}
	upgrade.RemoveOld(resolved)

	client := &update.Client{HTTP: o.HTTP, APIURL: o.APIURL, UserAgent: "ytrack/" + f.AppVersion}
	var rel *update.Release
	if version != "" {
		rel, err = client.Tag(ctx, version)
		if errors.Is(err, update.ErrReleaseNotFound) {
			return fmt.Errorf("release %s not found", version)
		}
	} else {
		rel, err = client.Latest(ctx)
	}
	if err != nil {
		return err
	}

	current := f.AppVersion
	latest := rel.Version()
	if check {
		switch {
		case update.Newer(latest, current):
			fmt.Fprintf(out, "ytrack %s is available (installed: %s)\n%s\nRun `ytrack upgrade` to install it.\n", latest, current, rel.URL)
		case !update.IsRelease(current):
			fmt.Fprintf(out, "the latest release is %s (installed: %s, a development build)\n", latest, current)
		default:
			fmt.Fprintf(out, "ytrack %s is up to date\n", current)
		}
		return nil
	}
	if version == "" && !update.Newer(latest, current) && update.IsRelease(current) {
		fmt.Fprintf(out, "ytrack %s is up to date\n", current)
		return nil
	}
	if strings.TrimPrefix(version, "v") == current {
		fmt.Fprintf(out, "ytrack %s is already installed\n", current)
		return nil
	}

	material, err := o.TrustedRoot()
	if err != nil {
		return fmt.Errorf("%w: %w; %s was not changed", upgrade.ErrRefused, err, resolved)
	}
	d := &upgrade.Downloader{
		HTTP:      o.HTTP,
		Trust:     upgrade.Trust{Material: material, Identity: upgrade.ReleaseIdentity, NoSCT: o.NoSCT},
		GOOS:      o.GOOS,
		GOARCH:    o.GOARCH,
		UserAgent: "ytrack/" + f.AppVersion,
		APIURL:    o.APIURL,
		Sleep:     o.Sleep,
		Notice:    func(msg string) { fmt.Fprintf(errOut, "%s\n", msg) },
	}
	fmt.Fprintf(errOut, "Downloading ytrack %s for %s/%s...\n", latest, o.GOOS, o.GOARCH)
	bin, err := d.Binary(ctx, rel)
	if err != nil {
		if errors.Is(err, upgrade.ErrRefused) {
			return fmt.Errorf("%w; %s was not changed", err, resolved)
		}
		return err
	}
	if err := upgrade.ReplaceExecutable(resolved, bin); err != nil {
		return fmt.Errorf("replacing %s: %w", resolved, err)
	}
	ok := f.IOStreams.ErrColorScheme().SuccessIcon()
	fmt.Fprintf(errOut, "%s Upgraded ytrack %s to %s (%s)\n", ok, current, latest, resolved)
	return nil
}
