package root

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// noticeTimeout bounds the daily release check, which runs in the
// background while the command does.
const noticeTimeout = 3 * time.Second

// noticeGrace is how long a finished command waits for a release check still
// in flight before abandoning it, so a slow or unreachable server never
// delays the exit by more than this. A var so tests can widen it.
var noticeGrace = 300 * time.Millisecond

// updateNotice prints a one-line notice on stderr after a command when a
// newer ytrack release is known. The notice comes from the cached result in
// state.yml, so printing it is instant, and is printed at most once per 24
// hours per release. The cache is refreshed from GitHub at most once per 24
// hours (update.CheckDaily) in a background goroutine; a refresh that has not
// finished noticeGrace after the command is abandoned and retried next run.
// It never runs in CI, when stderr is not a terminal, under --json, for
// development builds, or when turned off with YTRACK_NO_UPDATE_NOTIFIER or
// `check_update: false` in config.yml.
type updateNotice struct {
	f         *cmdutil.Factory
	statePath string
	done      chan struct{}
	cancel    context.CancelFunc
}

func (n *updateNotice) start(cmd *cobra.Command) {
	if !n.enabled(cmd) {
		return
	}
	statePath, err := update.StatePath()
	if err != nil {
		return
	}
	n.statePath = statePath
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

func (n *updateNotice) enabled(cmd *cobra.Command) bool {
	if !update.IsRelease(n.f.AppVersion) || !n.f.IOStreams.IsStderrTTY() || isCI() {
		return false
	}
	if v := os.Getenv("YTRACK_NO_UPDATE_NOTIFIER"); v != "" && v != "0" && v != "false" {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "upgrade", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return false
		}
	}
	if fl := cmd.Flags().Lookup("json"); fl != nil && fl.Changed {
		return false
	}
	cfg, err := n.f.Config()
	return err == nil && cfg.CheckUpdate()
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
	printed := n.printPending(now)
	if n.done != nil {
		select {
		case <-n.done:
		case <-time.After(noticeGrace):
		}
		// Abandoned checks record nothing (update.CheckDaily); never wait
		// for the goroutine, whatever the transport does with the cancel.
		n.cancel()
	}
	if !printed {
		n.printPending(now)
	}
}

// printPending prints the cached newer release, if one is due a notice, and
// records that it did.
func (n *updateNotice) printPending(now time.Time) bool {
	notice := update.Pending(n.statePath, n.f.AppVersion, now)
	if notice == nil {
		return false
	}
	hint := "ytrack upgrade"
	if exe, err := os.Executable(); err == nil {
		resolved, _ := filepath.EvalSymlinks(exe)
		if m := update.DetectManager(exe, resolved); m != nil {
			hint = m.Command
		}
	}
	fmt.Fprintf(n.f.IOStreams.ErrOut, "A new release of ytrack is available: %s → %s; run `%s` (%s)\n",
		notice.Current, notice.Latest, hint, notice.URL)
	update.MarkNotified(n.statePath, notice.Latest, now)
	return true
}
