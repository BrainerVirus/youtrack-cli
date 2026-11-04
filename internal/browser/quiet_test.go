package browser_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/browser"
)

// captureStderr runs fn with os.Stderr redirected to a file and returns what
// was written there.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = orig }()
	fn()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestQuietBrowser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the launcher stub is a POSIX shell script")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "opened")
	launcher := filepath.Join(dir, "browser")
	// Like a real browser: chatter on both streams, and a long-lived child
	// that inherits them.
	script := "#!/bin/sh\necho \"$1\" > '" + marker + "'\necho 'Sandbox: CanCreateUserNamespace() clone() failure: EPERM' >&2\necho noise\n(sleep 30) &\n"
	if err := os.WriteFile(launcher, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	t.Run("given a browser that prints diagnostics, it opens the URL and nothing reaches the terminal", func(t *testing.T) {
		start := time.Now()
		var err error
		out := captureStderr(t, func() { err = browser.Quiet(launcher).Browse("https://example.test/token") })
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("Browse waited %v for the browser's children", elapsed)
		}
		if out != "" {
			t.Fatalf("browser output reached stderr: %q", out)
		}
		got, err := os.ReadFile(marker)
		if err != nil || strings.TrimSpace(string(got)) != "https://example.test/token" {
			t.Fatalf("launcher did not get the URL: %q, %v", got, err)
		}
	})

	t.Run("given a launcher that fails, the error is returned", func(t *testing.T) {
		if err := browser.Quiet(filepath.Join(dir, "missing")).Browse("https://example.test"); err == nil {
			t.Fatal("a missing launcher returned no error")
		}
	})
}
