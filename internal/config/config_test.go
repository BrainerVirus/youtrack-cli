package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestHostsRoundTrip(t *testing.T) {
	t.Run("given saved hosts, loading returns the same entries and default", func(t *testing.T) {
		dir := t.TempDir()
		c, _ := LoadFrom(dir)
		c.SetHost("acme.youtrack.cloud", HostEntry{URL: "https://acme.youtrack.cloud", User: "jdoe", Storage: StorageKeyring})
		c.SetHost("tools.acme.com/youtrack", HostEntry{URL: "https://tools.acme.com/youtrack", User: "jd"})
		if err := c.Save(); err != nil {
			t.Fatal(err)
		}

		got, err := LoadFrom(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got.DefaultHost() != "acme.youtrack.cloud" {
			t.Errorf("default = %q, want the first host", got.DefaultHost())
		}
		if h := got.Host("tools.acme.com/youtrack"); h == nil || h.URL != "https://tools.acme.com/youtrack" || h.User != "jd" {
			t.Errorf("host = %+v", h)
		}
		if runtime.GOOS != "windows" {
			fi, _ := os.Stat(filepath.Join(dir, "hosts.yml"))
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("hosts.yml mode = %v", fi.Mode().Perm())
			}
		}
	})

	t.Run("given a missing directory, it loads an empty config", func(t *testing.T) {
		c, err := LoadFrom(filepath.Join(t.TempDir(), "nope"))
		if err != nil || len(c.HostKeys()) != 0 || c.DefaultHost() != "" {
			t.Fatalf("got %v, %v", c, err)
		}
	})

	t.Run("given malformed YAML, it reports the file", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte("hosts: [\n"), 0o600)
		if _, err := LoadFrom(dir); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestDefaultHost(t *testing.T) {
	t.Run("removing the default host promotes the first remaining host", func(t *testing.T) {
		c, _ := LoadFrom(t.TempDir())
		c.SetHost("b.example", HostEntry{URL: "https://b.example"})
		c.SetHost("a.example", HostEntry{URL: "https://a.example"})
		c.SetHost("c.example", HostEntry{URL: "https://c.example"})
		c.RemoveHost("b.example")
		if c.DefaultHost() != "a.example" {
			t.Errorf("default = %q", c.DefaultHost())
		}
		c.RemoveHost("a.example")
		c.RemoveHost("c.example")
		if c.DefaultHost() != "" {
			t.Errorf("default = %q after removing all hosts", c.DefaultHost())
		}
	})

	t.Run("an unknown host cannot become the default", func(t *testing.T) {
		c, _ := LoadFrom(t.TempDir())
		if err := c.SetDefaultHost("nope.example"); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestDir(t *testing.T) {
	t.Run("YTRACK_CONFIG_DIR wins over XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("YTRACK_CONFIG_DIR", "/custom")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		if d, _ := Dir(); d != "/custom" {
			t.Errorf("Dir() = %q", d)
		}
	})
	t.Run("XDG_CONFIG_HOME is used when set", func(t *testing.T) {
		t.Setenv("YTRACK_CONFIG_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		if d, _ := Dir(); d != filepath.Join("/xdg", "ytrack") {
			t.Errorf("Dir() = %q", d)
		}
	})
}
