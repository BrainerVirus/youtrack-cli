package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/BrainerVirus/youtrack-cli/internal/config"
)

// registryFile, in config.StateDir, lists the skills directories ytrack
// installed the skill into.
const registryFile = "skills.yml"

// Registry is the list of skills directories holding a copy of the skill
// that ytrack installed or adopted, and the version every copy it keeps up
// to date was last synced to.
type Registry struct {
	// Synced is the version of the last sync that rewrote every copy.
	Synced string `yaml:"synced,omitempty"`
	// Dirs are absolute skills directories; each holds <dir>/ytrack.
	Dirs []string `yaml:"dirs"`

	path    string
	existed bool
}

// HomeDirs returns the home-level skills directories of the agents ytrack
// knows, in adoption order: ~/.agents/skills first, as `npx skills add`
// keeps its canonical copy there and links the others to it.
func HomeDirs() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, rel := range []string{
		".agents/skills",          // Codex, Cursor, OpenCode, Pi and others
		".claude/skills",          // Claude Code
		".codex/skills",           // Codex (older)
		".cursor/skills",          // Cursor
		".config/opencode/skills", // OpenCode
		".pi/agent/skills",        // Pi
	} {
		dirs = append(dirs, filepath.Join(home, filepath.FromSlash(rel)))
	}
	return dirs, nil
}

// DefaultDirs returns the directories `ytrack skill install` writes without
// --dir: ~/.agents/skills, which Codex, Cursor, OpenCode and Pi read, and
// ~/.claude/skills, which Claude Code reads.
func DefaultDirs() ([]string, error) {
	dirs, err := HomeDirs()
	if err != nil {
		return nil, err
	}
	return dirs[:2], nil
}

// RegistryPath returns the registry file's path.
func RegistryPath() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, registryFile), nil
}

// OpenRegistry reads the registry. While the file does not exist yet, the
// registry adopts every home-level directory (HomeDirs) that holds the
// ytrack skill already, never one holding anything else; Save then creates
// the file.
func OpenRegistry() (*Registry, error) {
	path, err := RegistryPath()
	if err != nil {
		return nil, err
	}
	r := &Registry{path: path}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		r.existed = true
		if err := yaml.Unmarshal(b, r); err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		return r, nil
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	dirs, err := HomeDirs()
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if _, installed, err := resolveTarget(filepath.Join(d, Name)); err == nil && installed {
			r.Record(d)
		}
	}
	return r, nil
}

// Dirty reports whether the registry holds anything its file does not:
// copies adopted while the file did not exist yet.
func (r *Registry) Dirty() bool { return !r.existed && len(r.Dirs) > 0 }

// Save writes the registry: to a temporary file renamed into place, so a
// concurrent reader sees the old file or the new one, never a torn one.
func (r *Registry) Save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	body, err := yaml.Marshal(r)
	if err != nil {
		return err
	}
	b := append([]byte("# The skills directories ytrack installed its agent skill into. Managed by\n# ytrack skill install, uninstall and sync; see ytrack skill list.\n"), body...)
	tmp, err := os.CreateTemp(filepath.Dir(r.path), ".skills-*.yml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // gone after the rename
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), r.path); err != nil {
		return err
	}
	r.existed = true
	return nil
}

// Record adds the skills directory dir, made absolute, and reports whether
// the registry changed. Directories whose ytrack copy is the same one (one
// copy symlinked into several directories, as `npx skills add` does) are
// recorded once; the directory holding the copy itself is preferred over one
// holding a link to it.
func (r *Registry) Record(dir string) bool {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	id, link := copyID(dir)
	for i, d := range r.Dirs {
		if d == dir {
			return false
		}
		if other, otherLink := copyID(d); other == id {
			if otherLink && !link {
				r.Dirs[i] = dir
				return true
			}
			return false
		}
	}
	r.Dirs = append(r.Dirs, dir)
	return true
}

// Forget removes dir, made absolute, and reports whether it was recorded.
func (r *Registry) Forget(dir string) bool {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	n := len(r.Dirs)
	r.Dirs = slices.DeleteFunc(r.Dirs, func(d string) bool { return d == dir })
	return len(r.Dirs) != n
}

// copyID identifies the copy in <dir>/ytrack: its resolved path, and whether
// <dir>/ytrack is a symlink to it.
func copyID(dir string) (id string, link bool) {
	p := filepath.Join(dir, Name)
	if fi, err := os.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		link = true
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, link
	}
	return filepath.Clean(p), link
}

// Copy states.
const (
	StateCurrent = "current" // holds the skill at the running version
	StateStale   = "stale"   // holds the skill at another version
	StateMissing = "missing" // holds no ytrack skill (deleted or replaced)
)

// Copy is a recorded skill copy as found on disk.
type Copy struct {
	Dir     string // the skills directory
	Path    string // Dir/ytrack
	Version string // metadata.version of its SKILL.md; empty when missing
	State   string // StateCurrent, StateStale or StateMissing
	// Managed reports whether ytrack keeps the copy at its version (see
	// Managed); a copy in a repository is updated by hand.
	Managed bool
}

// Inspect returns the copy in the recorded directory dir, compared with
// version.
func Inspect(dir, version string) Copy {
	c := Copy{Dir: dir, Path: filepath.Join(dir, Name), State: StateMissing, Managed: Managed(dir)}
	target, installed, err := resolveTarget(c.Path)
	if err != nil || !installed {
		return c
	}
	b, err := os.ReadFile(filepath.Join(target, EntryFile))
	if err != nil {
		return c
	}
	m, err := ParseMeta(b)
	if err != nil {
		return c
	}
	c.Version = m.Metadata["version"]
	c.State = StateStale
	if c.Version == version {
		c.State = StateCurrent
	}
	return c
}

// Copies inspects every recorded copy.
func (r *Registry) Copies(version string) []Copy {
	copies := make([]Copy, 0, len(r.Dirs))
	for _, d := range r.Dirs {
		copies = append(copies, Inspect(d, version))
	}
	return copies
}

// Managed reports whether ytrack keeps the copy in skills directory dir at
// its own version: dir is one of HomeDirs, or under the home directory and
// not inside a git work tree. A copy in a repository is the repository's:
// it changes only through ytrack skill install --dir.
func Managed(dir string) bool {
	dir = filepath.Clean(dir)
	homeDirs, err := HomeDirs()
	if err != nil {
		return false
	}
	if slices.Contains(homeDirs, dir) {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(home, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return !inGitWorkTree(dir)
}

// inGitWorkTree reports whether dir or one of its parents has a .git entry.
func inGitWorkTree(dir string) bool {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// Sync outcomes.
const (
	SyncUpdated = "updated" // rewritten at the version
	SyncDropped = "dropped" // missing or replaced by something else; forgotten
	SyncManual  = "manual"  // a repository copy; left as it is
	SyncFailed  = "failed"  // could not be rewritten; still recorded
)

// SyncResult is what Sync did with one recorded directory.
type SyncResult struct {
	Copy
	Action string
	Err    error
}

// Sync rewrites every managed copy at version and forgets every recorded
// directory whose copy is gone or holds something else: a missing copy is
// never re-created. Copies in repositories are left alone. When every
// managed copy was rewritten, Synced becomes version. It reports what it did
// per directory, and whether it all succeeded. The caller saves the
// registry.
func (r *Registry) Sync(version string) ([]SyncResult, bool) {
	var results []SyncResult
	ok := true
	keep := r.Dirs[:0:0]
	done := map[string]bool{} // copies already rewritten, by copyID
	for _, d := range r.Dirs {
		c := Inspect(d, version)
		res := SyncResult{Copy: c}
		switch {
		case c.State == StateMissing:
			res.Action = SyncDropped
			results = append(results, res)
			continue
		case !c.Managed:
			res.Action = SyncManual
		default:
			id, _ := copyID(d)
			if !done[id] {
				if _, err := Install(d, version); err != nil {
					res.Action, res.Err, ok = SyncFailed, err, false
					break
				}
				done[id] = true
			}
			res.Action = SyncUpdated
			res.Version, res.State = version, StateCurrent
		}
		keep = append(keep, d)
		results = append(results, res)
	}
	r.Dirs = keep
	if ok {
		r.Synced = version
	}
	return results, ok
}

// Equal reports whether two registries record the same thing.
func (r *Registry) Equal(o *Registry) bool {
	return r.Synced == o.Synced && slices.Equal(r.Dirs, o.Dirs)
}

// Clone returns a copy of r for a later Equal.
func (r *Registry) Clone() *Registry {
	c := *r
	c.Dirs = slices.Clone(r.Dirs)
	return &c
}
