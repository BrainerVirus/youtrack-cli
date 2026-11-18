package upgrade

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Runner runs the ytrack binary at exe with args and returns its combined
// output.
type Runner func(ctx context.Context, exe string, args ...string) ([]byte, error)

// runTimeout bounds one run of a new binary (smoke test or skill sync).
const runTimeout = time.Minute

// RunBinary is the real Runner. The binary runs with the release check and
// the background upgrade turned off, so it never starts one of its own.
func RunBinary(ctx context.Context, exe string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...) //nolint:gosec // exe is the verified ytrack binary
	cmd.Env = append(os.Environ(), "YTRACK_NO_UPDATE_NOTIFIER=1", "YTRACK_NO_AUTO_UPGRADE=1")
	return cmd.CombinedOutput()
}

// SmokeTest runs the candidate binary's `version` and refuses it unless it
// exits 0 and names version.
func SmokeTest(ctx context.Context, run Runner, candidate, version string) error {
	out, err := run(ctx, candidate, "version")
	if err != nil {
		return fmt.Errorf("%w: the new binary failed `ytrack version`: %w", ErrRefused, err)
	}
	if !bytes.Contains(out, []byte(version)) {
		return fmt.Errorf("%w: the new binary's `ytrack version` does not name %s: %q", ErrRefused, version, bytes.TrimSpace(out))
	}
	return nil
}

// SyncSkills runs `ytrack skill sync` of the binary at exe, which refreshes
// the agent skill copies it installed, and returns its output. A binary
// without that command is not an error.
func SyncSkills(ctx context.Context, run Runner, exe string) (string, error) {
	out, err := run(ctx, exe, "skill", "sync")
	if err != nil {
		if bytes.Contains(out, []byte("unknown command")) {
			return "", nil
		}
		return "", fmt.Errorf("`ytrack skill sync`: %w: %s", err, bytes.TrimSpace(out))
	}
	return string(bytes.TrimSpace(out)), nil
}

// LockFile is the file in the state directory that an upgrade holds.
const LockFile = "upgrade.lock"

// LogFile is the file in the state directory where the background upgrade
// writes its output.
const LogFile = "auto-upgrade.log"

// lockStale is the age after which a lock is taken to be left by a crashed
// upgrade.
const lockStale = 30 * time.Minute

// ErrLocked is returned when another upgrade holds the lock.
var ErrLocked = errors.New("another ytrack upgrade is running")

// LockHeld reports whether an upgrade holds the lock at path.
func LockHeld(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && time.Since(fi.ModTime()) < lockStale
}

// AcquireLock takes the lock at path, replacing one older than lockStale.
// It returns ErrLocked when another upgrade holds it.
func AcquireLock(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) >= lockStale {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%w (%s)", ErrLocked, path)
	}
	if err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Close()
	return func() { _ = os.Remove(path) }, nil
}

// Executable returns the running binary's path. A var so tests can name
// another binary.
var Executable = os.Executable

// Spawn starts the background upgrade. A var so tests can watch it instead
// of starting a process.
var Spawn = StartDetached

// StartDetached starts exe with args in its own session (its own process
// group without a console on Windows), with no standard input and its output
// in logPath, and returns without waiting for it: the caller can exit at
// once, and a pipe the caller's caller reads never stays open for the child.
func StartDetached(exe string, args []string, logPath string) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = logf.Close() }()
	cmd := exec.Command(exe, args...) //nolint:gosec // exe is the running ytrack binary
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
