package editor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const modeEnv = "YTRACK_TEST_EDITOR_MODE"

// TestMain makes the test binary a stub editor when modeEnv is set. It writes
// the path it was given to $YTRACK_TEST_EDITOR_SEEN; then "append" appends
// " edited" to the file and "fail" exits 3.
func TestMain(m *testing.M) {
	if mode := os.Getenv(modeEnv); mode != "" && len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-test.") {
		path := os.Args[len(os.Args)-1]
		if seen := os.Getenv("YTRACK_TEST_EDITOR_SEEN"); seen != "" {
			_ = os.WriteFile(seen, []byte(path), 0o600)
		}
		if mode == "fail" {
			os.Exit(3)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(path, append(b, " edited"...), 0o600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCommand(t *testing.T) {
	vars := []string{"YTRACK_EDITOR", "GIT_EDITOR", "VISUAL", "EDITOR"}
	for i, winner := range vars {
		t.Run("given "+winner+" and the variables after it, "+winner+" wins", func(t *testing.T) {
			for j, k := range vars {
				v := ""
				if j >= i {
					v = "ed-" + k
				}
				t.Setenv(k, v)
			}
			if got := Command(); got != "ed-"+winner {
				t.Errorf("Command() = %q", got)
			}
		})
	}
	t.Run("given none, it falls back to a platform editor", func(t *testing.T) {
		for _, k := range vars {
			t.Setenv(k, "")
		}
		if got := Command(); got != "vi" && got != "notepad" {
			t.Errorf("Command() = %q", got)
		}
	})
}

func TestSplit(t *testing.T) {
	t.Run("given the path of an existing file with spaces, it is taken whole", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "My Editor", `ed\it`)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := split(path); err != nil || !reflect.DeepEqual(got, []string{path}) {
			t.Errorf("split = %q, %v", got, err)
		}
	})
	t.Run("given a command with arguments, it splits it like a shell", func(t *testing.T) {
		got, err := split(`code --wait "--title=a b"`)
		if err != nil || !reflect.DeepEqual(got, []string{"code", "--wait", "--title=a b"}) {
			t.Errorf("split = %q, %v", got, err)
		}
	})
	t.Run("given a directory, it is not taken whole", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "a b")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if got, err := split(dir); err == nil && len(got) == 1 {
			t.Errorf("split = %q", got)
		}
	})
	t.Run("given an empty command, it fails", func(t *testing.T) {
		if _, err := split("  "); err == nil {
			t.Error("no error")
		}
	})
}

func TestEdit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(t.TempDir(), "seen")
	t.Setenv("YTRACK_TEST_EDITOR_SEEN", seen)

	t.Run("it hands the editor the initial text and returns what it saved, then removes the file", func(t *testing.T) {
		t.Setenv(modeEnv, "append")
		var out strings.Builder
		got, err := Edit(exe, ".md", "draft", strings.NewReader(""), &out)
		if err != nil || got != "draft edited" {
			t.Fatalf("Edit = %q, %v", got, err)
		}
		path, _ := os.ReadFile(seen)
		if !strings.HasSuffix(string(path), ".md") {
			t.Errorf("temp file %q lacks the suffix", path)
		}
		if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
			t.Errorf("temp file %s still exists", path)
		}
	})

	t.Run("given an editor that fails, it reports its exit status", func(t *testing.T) {
		t.Setenv(modeEnv, "fail")
		_, err := Edit(exe, ".md", "", strings.NewReader(""), &strings.Builder{})
		if err == nil || !strings.Contains(err.Error(), "exited with status 3") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("given an editor that does not exist, it fails", func(t *testing.T) {
		_, err := Edit("ytrack-no-such-editor-xyz", ".md", "", strings.NewReader(""), &strings.Builder{})
		if err == nil || !strings.Contains(err.Error(), "running editor") {
			t.Errorf("err = %v", err)
		}
	})
}
