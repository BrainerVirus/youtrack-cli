package cmdtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ansiNames renders the SGR sequences ytrack emits as readable tags, so a
// golden file shows where styling starts and ends. Any other escape stays raw
// and fails the comparison visibly.
var ansiNames = strings.NewReplacer(
	"\x1b[0m", "</>",
	"\x1b[1m", "<bold>",
	"\x1b[2m", "<dim>",
	"\x1b[31m", "<red>",
	"\x1b[32m", "<green>",
	"\x1b[33m", "<yellow>",
	"\x1b[36m", "<accent>",
)

// ShowANSI replaces ytrack's ANSI styles in s with tags such as <bold>.
func ShowANSI(s string) string { return ansiNames.Replace(s) }

// Golden compares got with testdata/<name>.golden, after ShowANSI. Run the
// tests with YTRACK_UPDATE_GOLDEN=1 to rewrite the file.
func Golden(t *testing.T, name, got string) {
	t.Helper()
	got = ShowANSI(got)
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv("YTRACK_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with YTRACK_UPDATE_GOLDEN=1 to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s does not match (YTRACK_UPDATE_GOLDEN=1 rewrites it)\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}
