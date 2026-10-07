package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestStore(t *testing.T) {
	t.Run("a keyring token is stored under ytrack:<host> and read back", func(t *testing.T) {
		keyring.MockInit()
		s := Store{Dir: t.TempDir()}
		where, err := s.Set("tools.acme.com/youtrack", "perm-x", false)
		if err != nil || where != SourceKeyring {
			t.Fatalf("Set = %q, %v", where, err)
		}
		if v, _ := keyring.Get("ytrack:tools.acme.com/youtrack", keyringUser); v != "perm-x" {
			t.Errorf("keyring item = %q", v)
		}
		tok, src, err := s.Get("tools.acme.com/youtrack")
		if tok != "perm-x" || src != SourceKeyring || err != nil {
			t.Errorf("Get = %q, %q, %v", tok, src, err)
		}
	})

	t.Run("without a keyring, Set fails with ErrNoKeyring and writes nothing", func(t *testing.T) {
		keyring.MockInitWithError(errors.New("dbus: no secret service"))
		dir := t.TempDir()
		_, err := Store{Dir: dir}.Set("h", "perm-x", false)
		if !errors.Is(err, ErrNoKeyring) {
			t.Fatalf("err = %v, want ErrNoKeyring", err)
		}
		if _, err := os.Stat(filepath.Join(dir, credentialFile)); !os.IsNotExist(err) {
			t.Error("fell back to a plaintext file")
		}
	})

	t.Run("insecure storage writes a file that Get falls back to", func(t *testing.T) {
		keyring.MockInitWithError(errors.New("no keyring"))
		s := Store{Dir: t.TempDir()}
		if where, err := s.Set("h", "perm-x", true); err != nil || where != SourceFile {
			t.Fatalf("Set = %q, %v", where, err)
		}
		tok, src, _ := s.Get("h")
		if tok != "perm-x" || src != SourceFile {
			t.Errorf("Get = %q, %q", tok, src)
		}
	})

	t.Run("a later keyring login removes the plaintext copy", func(t *testing.T) {
		keyring.MockInit()
		s := Store{Dir: t.TempDir()}
		_, _ = s.Set("h", "perm-old", true)
		if _, err := s.Set("h", "perm-new", false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(s.path()); !os.IsNotExist(err) {
			t.Error("credentials.yml still exists")
		}
	})

	t.Run("Delete removes the token from both places", func(t *testing.T) {
		keyring.MockInit()
		s := Store{Dir: t.TempDir()}
		_, _ = s.Set("a", "perm-a", true)
		_, _ = s.Set("b", "perm-b", false)
		if err := s.Delete("a"); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("b"); err != nil {
			t.Fatal(err)
		}
		for _, h := range []string{"a", "b"} {
			if tok, _, _ := s.Get(h); tok != "" {
				t.Errorf("token for %s survived Delete", h)
			}
		}
	})
}

func TestStoreKeyringFailures(t *testing.T) {
	t.Run("given a broken keyring and no file, Get reports the keyring error instead of no token", func(t *testing.T) {
		keyring.MockInitWithError(errors.New("org.freedesktop.Secret.Error.IsLocked"))
		_, _, err := Store{Dir: t.TempDir()}.Get("h")
		if err == nil || !strings.Contains(err.Error(), "IsLocked") {
			t.Fatalf("err = %v, want the keyring error", err)
		}
	})

	t.Run("given an empty keyring, Get reports no token without an error", func(t *testing.T) {
		keyring.MockInit()
		tok, _, err := Store{Dir: t.TempDir()}.Get("h")
		if tok != "" || err != nil {
			t.Fatalf("Get = %q, %v", tok, err)
		}
	})

	t.Run("given a broken keyring and a file copy, Delete succeeds", func(t *testing.T) {
		keyring.MockInitWithError(errors.New("no keyring"))
		s := Store{Dir: t.TempDir()}
		_, _ = s.Set("h", "perm-x", true)
		if err := s.Delete("h"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	})
}

func TestMask(t *testing.T) {
	for token, want := range map[string]string{
		"perm-YWRtaW4=.NDQtMA==.secret": "perm-****",
		"perm:YWRtaW4=.secret":          "perm:****",
		"something-else":                "****",
	} {
		if got := Mask(token); got != want {
			t.Errorf("Mask(%q) = %q, want %q", token, got, want)
		}
	}
}
