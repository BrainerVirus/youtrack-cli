package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestProjectCache(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	c := ProjectCache{Dir: t.TempDir(), Now: func() time.Time { return now }}

	t.Run("given nothing stored, the answer is unknown", func(t *testing.T) {
		if _, known := c.Lookup("a.example", "APP"); known {
			t.Error("known on an empty cache")
		}
	})

	t.Run("given a stored answer, it is known per host and project in any case", func(t *testing.T) {
		c.Store("a.example", "app", true)
		c.Store("b.example", "APP", false)
		if has, known := c.Lookup("a.example", "APP"); !has || !known {
			t.Errorf("a: has %v known %v", has, known)
		}
		if has, known := c.Lookup("b.example", "App"); has || !known {
			t.Errorf("b: has %v known %v", has, known)
		}
	})

	t.Run("given an answer older than the TTL, it is unknown", func(t *testing.T) {
		later := ProjectCache{Dir: c.Dir, Now: func() time.Time { return now.Add(ProjectCacheTTL + time.Minute) }}
		if _, known := later.Lookup("a.example", "APP"); known {
			t.Error("a stale answer is still known")
		}
	})

	t.Run("given an unreadable cache file, the answer is unknown", func(t *testing.T) {
		bad := ProjectCache{Dir: filepath.Join(t.TempDir(), "missing")}
		if _, known := bad.Lookup("a.example", "APP"); known {
			t.Error("known without a cache file")
		}
	})
}

func TestCacheDir(t *testing.T) {
	t.Run("YTRACK_CACHE_DIR wins", func(t *testing.T) {
		t.Setenv("YTRACK_CACHE_DIR", "/c")
		t.Setenv("XDG_CACHE_HOME", "/x")
		if d, _ := CacheDir(); d != "/c" {
			t.Errorf("CacheDir() = %q", d)
		}
	})
	t.Run("XDG_CACHE_HOME is used when set", func(t *testing.T) {
		t.Setenv("YTRACK_CACHE_DIR", "")
		t.Setenv("XDG_CACHE_HOME", "/x")
		if d, _ := CacheDir(); d != filepath.Join("/x", "ytrack") {
			t.Errorf("CacheDir() = %q", d)
		}
	})
}
