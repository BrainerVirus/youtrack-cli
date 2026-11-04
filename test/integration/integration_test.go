package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestIntegration runs the real-instance suite. It is skipped unless
// YTRACK_IT=1, so `go test ./...` and CI never reach a YouTrack server.
// Run it with `make it YTRACK_IT_PROJECT=<short name>`; see
// docs/integration-tests.md.
func TestIntegration(t *testing.T) {
	if os.Getenv(envEnable) != "1" {
		t.Skipf("set %s=1 and %s to run the real-instance suite (make it)", envEnable, envProject)
	}
	cfg, err := loadConfig(os.Getenv, configDefaultHost)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bin == "" {
		cfg.Bin = buildYtrack(t)
	}
	if err := newSuite(cfg, os.Environ(), t.Logf).run(); err != nil {
		t.Fatal(err)
	}
}

// buildYtrack builds the ytrack binary into a temp dir and returns its path.
func buildYtrack(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ytrack")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/BrainerVirus/youtrack-cli/cmd/ytrack").CombinedOutput()
	if err != nil {
		t.Fatalf("building ytrack: %v\n%s", err, out)
	}
	return bin
}
