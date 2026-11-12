package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// PathMatch is the host a folder resolves to through the paths in hosts.yml.
type PathMatch struct {
	Host string // host key
	Glob string // the matching glob, as stored
}

// ValidatePathGlob reports why glob cannot be a host path. A path glob is
// absolute or starts with ~/, and its only wildcards are * (within one
// folder name) and ** (any number of folders): /home/me/work/**.
func ValidatePathGlob(glob string) error {
	if glob == "" {
		return fmt.Errorf("empty path")
	}
	if i := strings.IndexAny(glob, "?[]{}"); i >= 0 || strings.HasPrefix(glob, "!") {
		if i < 0 {
			i = 0
		}
		return fmt.Errorf("unsupported %q in path %q: use * and ** only, e.g. /home/me/work/**", glob[i], glob)
	}
	expanded, err := expandHome(glob)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(filepath.ToSlash(expanded), "/") && !filepath.IsAbs(expanded) {
		return fmt.Errorf("path %q is not absolute: start it with / or ~/", glob)
	}
	return nil
}

// HostPaths returns the path globs of host key.
func (c *Config) HostPaths(key string) []string {
	if e := c.hosts.Hosts[key]; e != nil {
		return slices.Clone(e.Paths)
	}
	return nil
}

// AddHostPath adds glob to the paths of host key, which must be known. It
// reports whether the glob was new.
func (c *Config) AddHostPath(key, glob string) (bool, error) {
	e := c.hosts.Hosts[key]
	if e == nil {
		return false, fmt.Errorf("not logged in to %s", key)
	}
	if err := ValidatePathGlob(glob); err != nil {
		return false, err
	}
	if slices.Contains(e.Paths, glob) {
		return false, nil
	}
	e.Paths = append(e.Paths, glob)
	return true, nil
}

// RemoveHostPath removes glob from the paths of host key and reports
// whether it was there.
func (c *Config) RemoveHostPath(key, glob string) bool {
	e := c.hosts.Hosts[key]
	if e == nil || !slices.Contains(e.Paths, glob) {
		return false
	}
	e.Paths = slices.DeleteFunc(e.Paths, func(p string) bool { return p == glob })
	return true
}

// HostForPath returns the host whose path glob matches one of dirs. The
// most specific glob wins: the one with the longest literal text before its
// first wildcard. A tie goes to the host key that sorts first.
func (c *Config) HostForPath(dirs ...string) (PathMatch, bool) {
	var best PathMatch
	bestLen := -1
	for _, key := range c.HostKeys() {
		for _, glob := range c.hosts.Hosts[key].Paths {
			re, prefix, err := compileGlob(glob)
			if err != nil {
				continue
			}
			if len(prefix) <= bestLen {
				continue
			}
			for _, d := range dirs {
				if d != "" && re.MatchString(slashClean(d)) {
					best, bestLen = PathMatch{Host: key, Glob: glob}, len(prefix)
					break
				}
			}
		}
	}
	return best, bestLen >= 0
}

func slashClean(p string) string { return filepath.ToSlash(filepath.Clean(p)) }

func expandHome(glob string) (string, error) {
	if glob != "~" && !strings.HasPrefix(glob, "~/") && !strings.HasPrefix(glob, `~\`) {
		return glob, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot expand ~ in %q: %w", glob, err)
	}
	return home + glob[1:], nil
}

// compileGlob turns a path glob into a regular expression and returns the
// literal text before its first wildcard. A trailing /** also matches the
// folder itself; **/ matches zero or more folders; * stays within a folder.
func compileGlob(glob string) (*regexp.Regexp, string, error) {
	if err := ValidatePathGlob(glob); err != nil {
		return nil, "", err
	}
	expanded, err := expandHome(glob)
	if err != nil {
		return nil, "", err
	}
	g := filepath.ToSlash(expanded)
	if len(g) > 1 {
		g = strings.TrimRight(g, "/")
	}
	prefix := g
	if i := strings.IndexByte(g, '*'); i >= 0 {
		prefix = g[:i]
	}
	var b strings.Builder
	for i := 0; i < len(g); i++ {
		switch {
		case strings.HasPrefix(g[i:], "/**") && i+3 == len(g):
			b.WriteString("(?:/.*)?")
			i += 2
		case strings.HasPrefix(g[i:], "**/"):
			b.WriteString("(?:[^/]+/)*")
			i += 2
		case strings.HasPrefix(g[i:], "**"):
			b.WriteString(".*")
			i++
		case g[i] == '*':
			b.WriteString("[^/]*")
		default:
			b.WriteString(regexp.QuoteMeta(g[i : i+1]))
		}
	}
	re, err := regexp.Compile("^" + b.String() + "$")
	return re, prefix, err
}
