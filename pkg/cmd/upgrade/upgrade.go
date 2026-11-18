// Package upgrade implements `ytrack upgrade`.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	// Run runs a ytrack binary: the new one's `version` before it is swapped
	// in, and its `skill sync` after.
	Run upgrade.Runner
}

func defaults(f *cmdutil.Factory) *Options {
	return &Options{
		HTTP:        &http.Client{Transport: f.RoundTripper, Timeout: 5 * time.Minute},
		TrustedRoot: upgrade.PublicGoodTrustedRoot,
		Executable:  upgrade.Executable,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Run:         upgrade.RunBinary,
	}
}

// NewCmdUpgrade returns the upgrade command. opts nil means the real
// GitHub releases, trust root and executable.
func NewCmdUpgrade(f *cmdutil.Factory, opts *Options) *cobra.Command {
	var check, auto bool
	var version string
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade ytrack to the latest release",
		Long: `Upgrade ytrack to the latest release, or to the release named by --version.

The release's checksums file must carry a Sigstore (cosign keyless) signature
made by this repository's release workflow on GitHub Actions, and the archive
must match its SHA-256 in that file. The new binary must then run
` + "`ytrack version`" + `; only then is the running binary replaced, atomically.
Any failed check refuses the upgrade and changes nothing. After an upgrade the
new binary runs ` + "`ytrack skill sync`" + ` to refresh the agent skill copies.

ytrack also upgrades itself in the background, once a newer release is a day
old; turn that off with ` + "`auto_upgrade: false`" + ` in config.yml or
YTRACK_NO_AUTO_UPGRADE=1.

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
			if auto {
				return runAuto(cmd.Context(), f, o)
			}
			return run(cmd.Context(), f, o, check, version)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Only report whether a newer release exists")
	cmd.Flags().StringVar(&version, "version", "", "Install this release `tag` instead of the latest")
	// The background upgrade the update check starts (see pkg/cmd/root).
	cmd.Flags().BoolVar(&auto, "auto", false, "Upgrade in the background to a release at least a day old")
	_ = cmd.Flags().MarkHidden("auto")
	cmd.MarkFlagsMutuallyExclusive("auto", "check")
	cmd.MarkFlagsMutuallyExclusive("auto", "version")
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
	// Without a state directory there is no lock and nothing to record.
	statePath, _ := update.StatePath()

	if lp := lockPath(statePath); lp != "" {
		unlock, err := upgrade.AcquireLock(lp)
		if err != nil {
			return fmt.Errorf("%w; try again in a minute", err)
		}
		defer unlock()
	}
	fmt.Fprintf(errOut, "Downloading ytrack %s for %s/%s...\n", latest, o.GOOS, o.GOARCH)
	if err := install(ctx, f, o, rel, resolved); err != nil {
		return err
	}
	if statePath != "" {
		update.Update(statePath, func(s *update.State) { s.Auto.RecordSuccess(current, latest, f.Clock(), false) })
	}
	ok := f.IOStreams.ErrColorScheme().SuccessIcon()
	fmt.Fprintf(errOut, "%s Upgraded ytrack %s to %s (%s)\n", ok, current, latest, resolved)
	if msg, err := upgrade.SyncSkills(ctx, o.Run, resolved); err != nil {
		f.Warnf("the agent skills were not refreshed: %v", err)
	} else if msg != "" {
		fmt.Fprintln(errOut, msg)
	}
	return nil
}

// lockPath is the upgrade lock next to the state file, or "" without one.
func lockPath(statePath string) string {
	if statePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(statePath), upgrade.LockFile)
}

// install downloads and verifies rel, runs the new binary's `version` and
// swaps it in for resolved. Any failure leaves resolved untouched.
func install(ctx context.Context, f *cmdutil.Factory, o *Options, rel *update.Release, resolved string) error {
	material, err := o.TrustedRoot()
	if err != nil {
		return fmt.Errorf("%w: %w; %s was not changed", upgrade.ErrRefused, err, resolved)
	}
	errOut := f.IOStreams.ErrOut
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
	bin, err := d.Binary(ctx, rel)
	if err != nil {
		if errors.Is(err, upgrade.ErrRefused) {
			return fmt.Errorf("%w; %s was not changed", err, resolved)
		}
		return err
	}
	smoke := func(candidate string) error { return upgrade.SmokeTest(ctx, o.Run, candidate, rel.Version()) }
	if err := upgrade.ReplaceExecutable(resolved, bin, smoke); err != nil {
		if errors.Is(err, upgrade.ErrRefused) {
			return fmt.Errorf("%w; %s was not changed", err, resolved)
		}
		return fmt.Errorf("replacing %s: %w", resolved, err)
	}
	return nil
}

// runAuto is `ytrack upgrade --auto`, the background upgrade: it installs
// the latest release once it is update.SoakPeriod old, unless a package
// manager owns the binary, another upgrade holds the lock or an earlier
// failure's backoff is active. It records the outcome in the state file,
// where the next interactive run reports it.
func runAuto(ctx context.Context, f *cmdutil.Factory, o *Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	errOut := f.IOStreams.ErrOut
	statePath, err := update.StatePath()
	if err != nil {
		return err
	}
	exe, err := o.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the ytrack binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	if m := update.DetectManager(exe, resolved); m != nil {
		fmt.Fprintf(errOut, "ytrack was installed with %s; not upgrading in the background\n", m.Name)
		return nil
	}
	unlock, err := upgrade.AcquireLock(lockPath(statePath))
	if err != nil {
		fmt.Fprintf(errOut, "%v; not upgrading in the background\n", err)
		return nil
	}
	defer unlock()
	now := f.Clock()
	if st := update.ReadState(statePath); st.Auto.BackingOff(now) {
		fmt.Fprintf(errOut, "the last background upgrade failed; next attempt after %s\n", st.Auto.RetryAt.Format(time.RFC3339))
		return nil
	}
	upgrade.RemoveOld(resolved)

	current := f.AppVersion
	client := &update.Client{HTTP: o.HTTP, APIURL: o.APIURL, UserAgent: "ytrack/" + current}
	rel, err := client.Latest(ctx)
	if err != nil {
		update.Update(statePath, func(s *update.State) { s.Auto.RecordAutoFailure(s.LatestVersion, err, now) })
		return err
	}
	stable := !rel.Draft && !rel.Prerelease
	update.Update(statePath, func(s *update.State) {
		s.CheckedAt = now
		if stable {
			s.SetLatest(rel)
		}
	})
	latest := rel.Version()
	switch {
	case !stable || !update.Newer(latest, current):
		fmt.Fprintf(errOut, "ytrack %s is up to date\n", current)
		return nil
	case !update.Soaked(rel.PublishedAt, now):
		fmt.Fprintf(errOut, "ytrack %s was published at %s; waiting %s before installing it\n",
			latest, rel.PublishedAt.Format(time.RFC3339), update.SoakPeriod)
		return nil
	}
	fmt.Fprintf(errOut, "%s upgrading ytrack %s to %s (%s)\n", now.Format(time.RFC3339), current, latest, resolved)
	if err := install(ctx, f, o, rel, resolved); err != nil {
		update.Update(statePath, func(s *update.State) { s.Auto.RecordAutoFailure(latest, err, now) })
		return err
	}
	update.Update(statePath, func(s *update.State) { s.Auto.RecordSuccess(current, latest, now, true) })
	fmt.Fprintf(errOut, "upgraded ytrack %s to %s\n", current, latest)
	if msg, err := upgrade.SyncSkills(ctx, o.Run, resolved); err != nil {
		fmt.Fprintf(errOut, "the agent skills were not refreshed: %v\n", err)
	} else if msg != "" {
		fmt.Fprintln(errOut, msg)
	}
	return nil
}
