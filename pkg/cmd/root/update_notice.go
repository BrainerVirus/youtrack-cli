package root

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// noticeTimeout bounds the daily release check, which runs in the
// background while the command does.
const noticeTimeout = 3 * time.Second

// noticeGrace is how long a finished command waits for a release check still
// in flight before abandoning it, so a slow or unreachable server never
// delays the exit by more than this. A var so tests can widen it.
var noticeGrace = 300 * time.Millisecond

// updateNotice keeps ytrack current after a command.
//
// The release check: the latest release is cached in state.yml and refreshed
// from GitHub at most once per 24 hours (update.CheckDaily) in a background
// goroutine; a refresh that has not finished noticeGrace after the command is
// abandoned and retried next run. It runs for every command, with or without
// a terminal or --json (agents run ytrack that way), except `ytrack skill`
// (which works offline), `upgrade` and `completion`, in CI, for development
// builds, and when turned off with YTRACK_NO_UPDATE_NOTIFIER or
// `check_update: false` in config.yml.
//
// The background upgrade: when the cache knows a newer release at least
// update.SoakPeriod old, and `auto_upgrade` (config.yml) and
// YTRACK_NO_AUTO_UPGRADE allow it, a detached `ytrack upgrade --auto` is
// started unless a package manager owns the binary, its directory is not
// writable, another upgrade holds the lock or an earlier failure's backoff
// is active. The command never waits for it.
//
// The notice: on a terminal and without --json, a one-line notice on stderr
// names a newer cached release (at most once per 24 hours per release), with
// the last background upgrade failure, and a background upgrade that
// succeeded is announced once.
type updateNotice struct {
	f         *cmdutil.Factory
	statePath string
	notify    bool // print notices
	auto      bool // may start a background upgrade
	done      chan struct{}
	cancel    context.CancelFunc
}

func (n *updateNotice) start(cmd *cobra.Command) {
	if !n.checkEnabled(cmd) {
		return
	}
	statePath, err := update.StatePath()
	if err != nil {
		return
	}
	n.statePath = statePath
	n.notify = n.f.IOStreams.IsStderrTTY() && !jsonOutput(cmd)
	if cfg, err := n.f.Config(); err == nil {
		n.auto = cfg.AutoUpgrade() && !envOn("YTRACK_NO_AUTO_UPGRADE")
	}
	now := n.f.Clock()
	if !update.Due(statePath, now) {
		return
	}
	n.done = make(chan struct{})
	client := &update.Client{
		HTTP:      &http.Client{Transport: n.f.RoundTripper},
		UserAgent: "ytrack/" + n.f.AppVersion,
	}
	ctx, cancel := context.WithTimeout(context.Background(), noticeTimeout)
	n.cancel = cancel
	go func() {
		defer close(n.done)
		_, _ = update.CheckDaily(ctx, client, statePath, n.f.AppVersion, now)
	}()
}

// checkEnabled reports whether the release check runs for cmd.
func (n *updateNotice) checkEnabled(cmd *cobra.Command) bool {
	if !update.IsRelease(n.f.AppVersion) || isCI() || envOn("YTRACK_NO_UPDATE_NOTIFIER") {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "upgrade", "skill", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return false
		}
	}
	cfg, err := n.f.Config()
	return err == nil && cfg.CheckUpdate()
}

func jsonOutput(cmd *cobra.Command) bool {
	fl := cmd.Flags().Lookup("json")
	return fl != nil && fl.Changed
}

// envOn reports whether the variable name is set to something other than
// "", "0" or "false".
func envOn(name string) bool {
	v := os.Getenv(name)
	return v != "" && v != "0" && v != "false"
}

// isCI reports whether ytrack runs in a CI system (the variables gh checks).
func isCI() bool {
	return os.Getenv("CI") != "" || os.Getenv("BUILD_NUMBER") != "" || os.Getenv("RUN_ID") != ""
}

func (n *updateNotice) print() {
	if n.statePath == "" {
		return
	}
	now := n.f.Clock()
	// A release already cached prints at once; the refresh then gets the
	// rest of the grace to land in the cache for the next run.
	printed := n.notify && n.printPending(now)
	if n.done != nil {
		select {
		case <-n.done:
		case <-time.After(noticeGrace):
		}
		// Abandoned checks record nothing (update.CheckDaily); never wait
		// for the goroutine, whatever the transport does with the cancel.
		n.cancel()
	}
	if n.notify {
		if !printed {
			n.printPending(now)
		}
		n.printUpgraded()
	}
	if n.auto {
		n.startAutoUpgrade(now)
	}
}

// printPending prints the cached newer release, if one is due a notice, with
// the last background upgrade failure, and records that it did.
func (n *updateNotice) printPending(now time.Time) bool {
	notice := update.Pending(n.statePath, n.f.AppVersion, now)
	if notice == nil {
		return false
	}
	hint := "ytrack upgrade"
	if exe, err := upgrade.Executable(); err == nil {
		resolved, _ := filepath.EvalSymlinks(exe)
		if m := update.DetectManager(exe, resolved); m != nil {
			hint = m.Command
		}
	}
	w := n.f.IOStreams.ErrOut
	fmt.Fprintf(w, "A new release of ytrack is available: %s → %s; run `%s` (%s)\n",
		notice.Current, notice.Latest, hint, notice.URL)
	if a := update.ReadState(n.statePath).Auto; a.Error != "" {
		fmt.Fprintf(w, "The background upgrade to %s failed at %s: %s; it retries after %s\n",
			a.FailedVersion, a.FailedAt.Local().Format(time.DateTime), output.SanitizeText(a.Error),
			a.RetryAt.Local().Format(time.DateTime))
	}
	update.MarkNotified(n.statePath, notice.Latest, now)
	return true
}

// printUpgraded announces, once, a background upgrade that succeeded.
func (n *updateNotice) printUpgraded() {
	a := update.ReadState(n.statePath).Auto
	if a.To == "" || a.Announced {
		return
	}
	fmt.Fprintf(n.f.IOStreams.ErrOut, "ytrack upgraded %s → %s in the background\n", a.From, a.To)
	update.Update(n.statePath, func(s *update.State) { s.Auto.Announced = true })
}

// startAutoUpgrade starts a detached `ytrack upgrade --auto` when the cached
// check calls for one (see updateNotice). It never waits for the child.
func (n *updateNotice) startAutoUpgrade(now time.Time) {
	s := update.ReadState(n.statePath)
	if !s.AutoDue(n.f.AppVersion, now) {
		return
	}
	exe, err := upgrade.Executable()
	if err != nil {
		return
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	if update.DetectManager(exe, resolved) != nil || !upgrade.DirWritable(filepath.Dir(resolved)) {
		return
	}
	dir := filepath.Dir(n.statePath)
	if upgrade.LockHeld(filepath.Join(dir, upgrade.LockFile)) {
		return
	}
	update.Update(n.statePath, func(s *update.State) { s.Auto.StartedAt = now })
	_ = upgrade.Spawn(resolved, []string{"upgrade", "--auto"}, filepath.Join(dir, upgrade.LogFile))
}
