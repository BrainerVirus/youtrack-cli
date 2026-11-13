package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// PathMatch is the host a folder resolves to through the paths in hosts.yml.
type PathMatch struct {
	Host string // host key
	Glob string // the matching glob, as stored
}

// pathOS decides how paths compare: macOS and Windows file systems ignore
// case by default, so globs match folders regardless of case there and
// case-sensitively elsewhere. Windows also takes \ as a separator and a
// drive letter as the root. Every rule follows pathOS, never the rules of
// the OS the code runs on, so tests can stand in for any OS.
var pathOS = runtime.GOOS

// SetPathOS makes path globs compare as on goos (a runtime.GOOS value) until
// restore is called. It lets tests cover every OS from one machine.
func SetPathOS(goos string) (restore func()) {
	old := pathOS
	pathOS = goos
	return func() { pathOS = old }
}

// foldCase reports whether paths compare case-insensitively.
func foldCase() bool { return pathOS == "darwin" || pathOS == "windows" }

// samePath reports whether two stored globs name the same path: the same
// text, on Windows with \ and / alike, and on macOS and Windows in any case.
func samePath(a, b string) bool {
	a, b = toSlash(a), toSlash(b)
	if foldCase() {
		return strings.EqualFold(a, b)
	}
	return a == b
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
	if isHome(glob) {
		_, err := expandHome(glob)
		return err
	}
	if g := toSlash(glob); !strings.HasPrefix(g, "/") && !hasDrive(g) {
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

// FindHostPath returns the stored path of host key that names the same path
// as glob (see samePath).
func (c *Config) FindHostPath(key, glob string) (string, bool) {
	if e := c.hosts.Hosts[key]; e != nil {
		if i := slices.IndexFunc(e.Paths, func(p string) bool { return samePath(p, glob) }); i >= 0 {
			return e.Paths[i], true
		}
	}
	return "", false
}

// AddHostPath adds glob to the paths of host key, which must be known. It
// reports whether the glob was new (see FindHostPath).
func (c *Config) AddHostPath(key, glob string) (bool, error) {
	e := c.hosts.Hosts[key]
	if e == nil {
		return false, fmt.Errorf("not logged in to %s", key)
	}
	if err := ValidatePathGlob(glob); err != nil {
		return false, err
	}
	if _, ok := c.FindHostPath(key, glob); ok {
		return false, nil
	}
	e.Paths = append(e.Paths, glob)
	return true, nil
}

// RemoveHostPath removes glob from the paths of host key and reports
// whether it was there (see FindHostPath).
func (c *Config) RemoveHostPath(key, glob string) bool {
	if _, ok := c.FindHostPath(key, glob); !ok {
		return false
	}
	e := c.hosts.Hosts[key]
	e.Paths = slices.DeleteFunc(e.Paths, func(p string) bool { return samePath(p, glob) })
	return true
}

// HostForPath returns the host whose path glob matches one of dirs. The
// most specific glob wins: the one with the longest literal text before its
// first wildcard. A tie goes to the host key that sorts first.
//
// On macOS and Windows globs match regardless of case. When the most
// specific matching globs include two of different hosts that differ only
// by case, it is an error naming both: the user meant one folder, so
// neither host is picked. A narrower glob that also matches still wins, and
// the same glob on two hosts keeps the tie rule.
func (c *Config) HostForPath(dirs ...string) (PathMatch, bool, error) {
	type match struct {
		PathMatch
		prefixLen int
	}
	var matched []match
	bestLen := -1
	for _, key := range c.HostKeys() {
		for _, glob := range c.hosts.Hosts[key].Paths {
			re, prefix, err := compileGlob(glob)
			if err != nil || !matchesAny(re, dirs) {
				continue
			}
			matched = append(matched, match{PathMatch{Host: key, Glob: glob}, len(prefix)})
			bestLen = max(bestLen, len(prefix))
		}
	}
	var top []PathMatch
	for _, m := range matched {
		if m.prefixLen == bestLen {
			top = append(top, m.PathMatch)
		}
	}
	if len(top) == 0 {
		return PathMatch{}, false, nil
	}
	if foldCase() {
		for i, a := range top {
			for _, b := range top[i+1:] {
				if a.Host != b.Host && a.Glob != b.Glob && samePath(a.Glob, b.Glob) {
					return PathMatch{}, false, fmt.Errorf(
						"cannot pick a host by folder: path %q of %s and path %q of %s differ only by case, and case does not matter on this OS; remove one with `ytrack auth paths --remove`",
						a.Glob, a.Host, b.Glob, b.Host)
				}
			}
		}
	}
	return top[0], true, nil
}

func matchesAny(re *regexp.Regexp, dirs []string) bool {
	for _, d := range dirs {
		if d != "" && re.MatchString(slashClean(d)) {
			return true
		}
	}
	return false
}

func slashClean(p string) string { return path.Clean(toSlash(p)) }

// toSlash turns \ into / when paths compare as on Windows; elsewhere \ is
// an ordinary character of a folder name.
func toSlash(p string) string {
	if pathOS == "windows" {
		return strings.ReplaceAll(p, `\`, "/")
	}
	return p
}

// hasDrive reports whether p starts with a Windows drive, as in C:/, when
// paths compare as on Windows.
func hasDrive(p string) bool {
	return pathOS == "windows" && len(p) >= 3 && p[1] == ':' && p[2] == '/' &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

func isHome(glob string) bool {
	return glob == "~" || strings.HasPrefix(glob, "~/") || strings.HasPrefix(glob, `~\`)
}

// expandHome replaces a leading ~ with the home directory, which comes from
// the OS the code runs on and so is written with its separators.
func expandHome(glob string) (string, error) {
	if !isHome(glob) {
		return glob, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot expand ~ in %q: %w", glob, err)
	}
	return filepath.ToSlash(home) + glob[1:], nil
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
	g := toSlash(expanded)
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
	flags := ""
	if foldCase() {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + "^" + b.String() + "$")
	return re, prefix, err
}
