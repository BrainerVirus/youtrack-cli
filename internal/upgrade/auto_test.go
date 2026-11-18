package upgrade_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
)

func TestReplaceExecutableChecksTheCandidate(t *testing.T) {
	t.Run("given a check that fails, the binary is untouched and no candidate is left", func(t *testing.T) {
		exe := filepath.Join(t.TempDir(), "ytrack")
		if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		var checked string
		err := upgrade.ReplaceExecutable(exe, []byte("new"), func(candidate string) error {
			b, _ := os.ReadFile(candidate)
			checked = string(b)
			return errors.New("does not run")
		})
		if err == nil || !strings.Contains(err.Error(), "does not run") {
			t.Fatalf("err = %v", err)
		}
		if checked != "new" {
			t.Errorf("the check saw %q, want the new binary", checked)
		}
		if b, _ := os.ReadFile(exe); string(b) != "old" {
			t.Errorf("binary = %q, want it untouched", b)
		}
		if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
			t.Errorf("left files next to the binary: %v", entries)
		}
	})
}

// versionRunner is a binary whose `version` prints out, or fails with err.
func versionRunner(out string, err error) upgrade.Runner {
	return func(context.Context, string, ...string) ([]byte, error) { return []byte(out), err }
}

func TestSmokeTest(t *testing.T) {
	ctx := context.Background()
	if err := upgrade.SmokeTest(ctx, versionRunner("ytrack version 1.1.0 (2026-11-17)\n", nil), "c", "1.1.0"); err != nil {
		t.Errorf("given a binary that names its version, err = %v", err)
	}
	for name, run := range map[string]upgrade.Runner{
		"a binary that exits non-zero":        versionRunner("", errors.New("exit status 2")),
		"a binary that names another version": versionRunner("ytrack version 1.0.0\n", nil),
	} {
		err := upgrade.SmokeTest(ctx, run, "c", "1.1.0")
		if !errors.Is(err, upgrade.ErrRefused) {
			t.Errorf("given %s, err = %v, want a refusal", name, err)
		}
	}
}

func TestSyncSkills(t *testing.T) {
	ctx := context.Background()
	var got []string
	run := func(_ context.Context, exe string, args ...string) ([]byte, error) {
		got = append([]string{exe}, args...)
		return []byte("refreshed 2 skill copies\n"), nil
	}
	if msg, err := upgrade.SyncSkills(ctx, run, "/bin/ytrack"); err != nil || msg != "refreshed 2 skill copies" {
		t.Errorf("msg = %q, err = %v", msg, err)
	}
	if strings.Join(got, " ") != "/bin/ytrack skill sync" {
		t.Errorf("ran %v", got)
	}
	older := versionRunner("ytrack: unknown command \"sync\" for \"ytrack skill\"\n", errors.New("exit status 1"))
	if msg, err := upgrade.SyncSkills(ctx, older, "x"); err != nil || msg != "" {
		t.Errorf("given a binary without skill sync, msg = %q, err = %v, want silence", msg, err)
	}
	if _, err := upgrade.SyncSkills(ctx, versionRunner("disk full", errors.New("exit status 1")), "x"); err == nil {
		t.Error("given a failing skill sync, want an error")
	}
}

func TestLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", upgrade.LockFile)
	if upgrade.LockHeld(path) {
		t.Fatal("held before anyone took it")
	}
	release, err := upgrade.AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if !upgrade.LockHeld(path) {
		t.Error("not held after AcquireLock")
	}
	if _, err := upgrade.AcquireLock(path); !errors.Is(err, upgrade.ErrLocked) {
		t.Errorf("second AcquireLock err = %v, want ErrLocked", err)
	}
	release()
	if upgrade.LockHeld(path) {
		t.Error("held after release")
	}

	t.Run("given a lock left an hour ago by a crashed upgrade, it is taken over", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		if upgrade.LockHeld(path) {
			t.Error("a stale lock counts as held")
		}
		release, err := upgrade.AcquireLock(path)
		if err != nil {
			t.Fatal(err)
		}
		release()
	})
}

func TestDirWritable(t *testing.T) {
	dir := t.TempDir()
	if !upgrade.DirWritable(dir) {
		t.Error("a temp dir is not writable")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left %v", entries)
	}
	if upgrade.DirWritable(filepath.Join(dir, "missing")) {
		t.Error("a missing dir is writable")
	}
}

const helperEnv = "YTRACK_TEST_DETACHED_HELPER"

// TestDetachedHelper is the child TestStartDetachedDoesNotWait starts.
func TestDetachedHelper(t *testing.T) {
	if os.Getenv(helperEnv) == "" {
		t.Skip("helper process")
	}
	time.Sleep(3 * time.Second)
	fmt.Println("helper done")
}

func TestStartDetachedDoesNotWait(t *testing.T) {
	t.Setenv(helperEnv, "1")
	logPath := filepath.Join(t.TempDir(), "state", upgrade.LogFile)
	began := time.Now()
	if err := upgrade.StartDetached(os.Args[0], []string{"-test.run=^TestDetachedHelper$", "-test.v"}, logPath); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("StartDetached took %v for a 3 s child, want it not to wait", took)
	}
	// The child's output lands in the log; wait for it so the temp dir can go.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if b, _ := os.ReadFile(logPath); strings.Contains(string(b), "helper done") && strings.Contains(string(b), "PASS") {
			break
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(logPath)
			t.Fatalf("the child never finished; log: %q", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
