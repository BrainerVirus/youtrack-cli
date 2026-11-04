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

// noticeTimeout bounds the daily release check, which runs while the
// command does; a command that finishes first waits at most this long.
const noticeTimeout = 3 * time.Second

// updateNotice prints a one-line notice on stderr after a command when a
// newer ytrack release exists. It asks GitHub at most once per 24 hours
// (update.CheckDaily) and never in CI, when stderr is not a terminal, under
// --json, for development builds, or when turned off with
// YTRACK_NO_UPDATE_NOTIFIER or `check_update: false` in config.yml.
type updateNotice struct {
	f    *cmdutil.Factory
	done chan *update.Notice
}

func (n *updateNotice) start(cmd *cobra.Command) {
	if !n.enabled(cmd) {
		return
	}
	statePath, err := update.StatePath()
	if err != nil {
		return
	}
	now := n.f.Clock()
	if !update.Due(statePath, now) {
		return
	}
	n.done = make(chan *update.Notice, 1)
	client := &update.Client{
		HTTP:      &http.Client{Transport: n.f.RoundTripper},
		UserAgent: "ytrack/" + n.f.AppVersion,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), noticeTimeout)
		defer cancel()
		notice, _ := update.CheckDaily(ctx, client, statePath, n.f.AppVersion, now)
		n.done <- notice
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
	if n.done == nil {
		return
	}
	notice := <-n.done
	if notice == nil {
		return
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
}
