package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectCacheTTL is how long a cached answer to "does host H have project
// P" is trusted.
const ProjectCacheTTL = 7 * 24 * time.Hour

const projectCacheFile = "projects.json"

// CacheDir returns the directory for data ytrack can rebuild:
// $YTRACK_CACHE_DIR, else $XDG_CACHE_HOME/ytrack, else the OS user cache
// directory plus ytrack (~/.cache/ytrack on Linux).
func CacheDir() (string, error) {
	if d := os.Getenv("YTRACK_CACHE_DIR"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "ytrack"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the cache directory: %w", err)
	}
	return filepath.Join(d, "ytrack"), nil
}

// ProjectCache remembers, per host and project short name, whether the host
// has that project, for ProjectCacheTTL. It is a hint: a missing, stale or
// unreadable file means "unknown".
type ProjectCache struct {
	Dir string
	Now func() time.Time
}

type projectEntry struct {
	Has     bool      `json:"has"`
	Checked time.Time `json:"checked"`
}

func projectKey(host, project string) string { return host + " " + strings.ToUpper(project) }

// Lookup returns whether host has project and whether the cache knows.
func (c ProjectCache) Lookup(host, project string) (has, known bool) {
	e, ok := c.read()[projectKey(host, project)]
	if !ok || c.now().Sub(e.Checked) > ProjectCacheTTL || e.Checked.After(c.now()) {
		return false, false
	}
	return e.Has, true
}

// Store records whether host has project. Errors are ignored: the cache is
// only a hint.
func (c ProjectCache) Store(host, project string, has bool) {
	entries := c.read()
	now := c.now()
	for k, e := range entries {
		if now.Sub(e.Checked) > ProjectCacheTTL {
			delete(entries, k)
		}
	}
	entries[projectKey(host, project)] = projectEntry{Has: has, Checked: now}
	b, err := json.Marshal(entries)
	if err != nil {
		return
	}
	_ = WriteFileAtomic(filepath.Join(c.Dir, projectCacheFile), b)
}

func (c ProjectCache) read() map[string]projectEntry {
	entries := map[string]projectEntry{}
	b, err := os.ReadFile(filepath.Join(c.Dir, projectCacheFile))
	if err != nil || json.Unmarshal(b, &entries) != nil {
		return map[string]projectEntry{}
	}
	return entries
}

func (c ProjectCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
