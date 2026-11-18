// Package skill renders and installs the agent skill embedded in the binary
// (skills/ytrack), stamped with the binary's version so an installed skill
// always describes the ytrack that installed it.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/BrainerVirus/youtrack-cli/skills"
)

// Name is the skill's name: its directory and its frontmatter name.
const Name = "ytrack"

// EntryFile is the skill's entry point inside its directory.
const EntryFile = "SKILL.md"

// Meta is the SKILL.md frontmatter.
type Meta struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Metadata    map[string]string `yaml:"metadata"`
}

// Files returns the embedded skill directory: SKILL.md and references/.
func Files() fs.FS {
	sub, err := fs.Sub(skills.FS, Name)
	if err != nil {
		panic(err) // the embed pattern guarantees the directory
	}
	return sub
}

// The metadata.version line inside the frontmatter; the committed file
// carries a placeholder that Render replaces.
var versionLine = regexp.MustCompile(`(?m)^  version:[^\r\n]*`)

// Render returns SKILL.md with metadata.version set to version.
func Render(version string) ([]byte, error) {
	b, err := fs.ReadFile(Files(), EntryFile)
	if err != nil {
		return nil, err
	}
	front, _, ok := splitFrontmatter(b)
	if !ok || !versionLine.Match(front) {
		return nil, errors.New("embedded SKILL.md has no metadata.version line")
	}
	stamped := versionLine.ReplaceAllLiteral(front, []byte("  version: "+strconv.Quote(version)))
	return append(stamped, b[len(front):]...), nil
}

// ParseMeta reads the frontmatter of a SKILL.md.
func ParseMeta(b []byte) (Meta, error) {
	var m Meta
	front, _, ok := splitFrontmatter(b)
	if !ok {
		return m, errors.New("no YAML frontmatter")
	}
	yamlText := bytes.TrimPrefix(bytes.TrimPrefix(front, []byte("---")), []byte("\r"))
	err := yaml.Unmarshal(yamlText, &m)
	return m, err
}

// splitFrontmatter splits b after the frontmatter's closing "---" line: front
// is the frontmatter including both fences, without the closing line break.
func splitFrontmatter(b []byte) (front, body []byte, ok bool) {
	if !bytes.HasPrefix(b, []byte("---\n")) && !bytes.HasPrefix(b, []byte("---\r\n")) {
		return nil, nil, false
	}
	for i := 3; i < len(b); {
		nl := bytes.IndexByte(b[i:], '\n')
		if nl < 0 {
			return nil, nil, false
		}
		start := i + nl + 1
		line := b[start:]
		if end := bytes.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		if string(bytes.TrimRight(line, "\r")) == "---" {
			end := start + len(bytes.TrimRight(line, "\r"))
			return b[:end], b[end:], true
		}
		i = start
	}
	return nil, nil, false
}

// rename is os.Rename; tests swap it to fail a step of Install.
var rename = os.Rename

// ForeignError reports a skill directory that Install will not replace
// because it does not hold the ytrack skill.
type ForeignError struct{ Dir string }

func (e *ForeignError) Error() string {
	return fmt.Sprintf("%s exists and is not the ytrack skill (its %s does not say \"name: %s\"); left it untouched",
		e.Dir, EntryFile, Name)
}

// Install writes the skill, stamped with version, to <root>/ytrack and
// returns that directory. root is created when missing. An existing
// <root>/ytrack is replaced only when its SKILL.md has "name: ytrack";
// anything else there is refused with a *ForeignError and left untouched. A
// symlinked <root>/ytrack (as `npx skills add` makes) is followed and its
// target updated.
//
// The new copy is written to a temporary sibling directory and renamed into
// place, so a failed install leaves the earlier skill as it was. Installs
// racing on one directory (two ytrack processes refreshing the same copy)
// all succeed and leave one complete copy and no temporary directories.
func Install(root, version string) (string, error) {
	return install(root, version, true)
}

// ErrBusy reports that InstallNoWait found another install working on the
// directory and left it alone.
var ErrBusy = errors.New("another install is updating this skill directory")

// InstallNoWait is Install for the background refresh, which must never
// delay a command: when another install holds the directory it returns
// ErrBusy at once and changes nothing.
func InstallNoWait(root, version string) (string, error) {
	return install(root, version, false)
}

func install(root, version string, wait bool) (string, error) {
	skillMD, err := Render(version)
	if err != nil {
		return "", err
	}
	if root, err = filepath.Abs(root); err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(root, Name)
	dir, _, err := resolveTarget(path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(dir)
	// Installs into one directory take turns: Windows refuses to rename a
	// directory another install is renaming. Without the lock (a read-only
	// directory, or a holder slower than installLockWait) swapIn still
	// copes with the race.
	lock := filepath.Join(parent, installLockName)
	var unlock func()
	if wait {
		unlock, _ = lockFile(lock, installLockWait)
	} else {
		var busy bool
		if unlock, busy = tryLockFile(lock); unlock == nil && busy {
			return "", ErrBusy
		}
	}
	if unlock != nil {
		defer unlock()
		// Another install may have changed the copy while this one waited.
		if dir, _, err = resolveTarget(path); err != nil {
			return "", err
		}
		parent = filepath.Dir(dir)
	}

	sweepStale(parent)
	stage, err := os.MkdirTemp(parent, stagePrefix+"new-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }() // gone already after a successful rename
	if err := writeTree(stage, skillMD); err != nil {
		return "", err
	}
	// The stage's name is unique, so the earlier copy's parking place is too.
	old := filepath.Join(parent, stagePrefix+"old-"+strings.TrimPrefix(filepath.Base(stage), stagePrefix+"new-"))
	defer func() { _ = os.RemoveAll(old) }()
	return path, swapIn(stage, dir, old)
}

// stagePrefix starts the name of every temporary directory Install makes.
const stagePrefix = ".ytrack-skill-"

// installLockName, next to the skill directory, is held while one install
// swaps its copy in.
const installLockName = ".ytrack-install.lock"

// installLockWait bounds how long an install waits for another into the
// same directory, which takes milliseconds.
const installLockWait = 10 * time.Second

// renameRetryFor bounds how long a rename refused by a transient Windows
// sharing violation (an indexer, antivirus or another install holding the
// directory open) is retried.
const renameRetryFor = 2 * time.Second

// transientRename reports whether a failed rename is worth retrying: on
// Windows, access denied or a sharing violation. A var so tests can make
// any error transient.
var transientRename = isTransientRename

// renameRetry is rename, retried with a short backoff while it fails
// transiently.
func renameRetry(oldpath, newpath string) error {
	deadline := time.Now().Add(renameRetryFor)
	backoff := 5 * time.Millisecond
	for {
		err := rename(oldpath, newpath)
		// A rename onto an existing directory is refused for good, not
		// transiently: swapIn handles that.
		if err == nil || !transientRename(err) || exists(newpath) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(backoff)
		backoff = min(2*backoff, 200*time.Millisecond)
	}
}

// swapAttempts bounds how often swapIn restarts after losing a race.
const swapAttempts = 50

// swapIn moves stage to dir, first parking an existing dir at old. Another
// process may move dir away or put its own copy there at any step; each such
// lost race restarts the swap. When stage cannot be moved in for any other
// reason, the parked copy goes back.
func swapIn(stage, dir, old string) error {
	var err error
	for range swapAttempts {
		if !exists(dir) {
			if err = renameRetry(stage, dir); err == nil || !occupied(err, dir) {
				return err
			}
			continue // another install got there first; replace its copy
		}
		if err = renameRetry(dir, old); err != nil {
			if !errors.Is(err, fs.ErrNotExist) && exists(dir) {
				return err
			}
			continue // another install moved dir away first
		}
		if err = renameRetry(stage, dir); err == nil {
			return nil
		}
		if occupied(err, dir) {
			_ = os.RemoveAll(old) // another install put a copy in; retry over it
			continue
		}
		_ = renameRetry(old, dir)
		return err
	}
	return fmt.Errorf("installing into %s: too many concurrent installs: %w", dir, err)
}

// occupied reports whether a rename onto dir failed because dir exists, as
// it does when another install put its copy there first.
func occupied(err error, dir string) bool {
	return errors.Is(err, fs.ErrExist) || exists(dir)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// staleAfter is how old a temporary directory must be before an install
// takes it for the litter of a killed install and removes it.
const staleAfter = 10 * time.Minute

// sweepStale removes temporary directories that an install killed midway
// left in parent.
func sweepStale(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), stagePrefix) {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > staleAfter {
			_ = os.RemoveAll(filepath.Join(parent, e.Name()))
		}
	}
}

// resolveTarget returns the directory to write: path itself, or the target
// of a symlink at path. installed reports whether it holds the ytrack skill
// already; a directory holding anything else is an error.
func resolveTarget(path string) (dir string, installed bool, err error) {
	// A SKILL.md that vanishes between the checks is most likely another
	// install swapping its copy in; look again before calling it foreign.
	for range 5 {
		dir, installed, err = resolveOnce(path)
		if !errors.Is(err, errVanished) {
			break
		}
	}
	if errors.Is(err, errVanished) {
		return "", false, &ForeignError{Dir: path}
	}
	return dir, installed, err
}

var errVanished = errors.New("SKILL.md vanished")

func resolveOnce(path string) (dir string, installed bool, err error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, false, nil
	}
	if err != nil {
		return "", false, err
	}
	dir = path
	if fi.Mode()&fs.ModeSymlink != 0 {
		if dir, err = filepath.EvalSymlinks(path); err != nil {
			return "", false, fmt.Errorf("%s is a symlink that cannot be followed: %w", path, err)
		}
		if fi, err = os.Stat(dir); err != nil {
			return "", false, err
		}
	}
	if !fi.IsDir() {
		return "", false, &ForeignError{Dir: path}
	}
	b, err := os.ReadFile(filepath.Join(dir, EntryFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, errVanished
	}
	if err != nil {
		return "", false, &ForeignError{Dir: path}
	}
	if m, err := ParseMeta(b); err != nil || m.Name != Name {
		return "", false, &ForeignError{Dir: path}
	}
	return dir, true, nil
}

// writeTree copies the embedded skill into dir, with skillMD as SKILL.md.
// dir is os.MkdirTemp's, so the skill directory itself is private (0700).
func writeTree(dir string, skillMD []byte) error {
	src := Files()
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.Mkdir(dst, 0o750)
		}
		data := skillMD
		if p != EntryFile {
			if data, err = fs.ReadFile(src, p); err != nil {
				return err
			}
		}
		return os.WriteFile(dst, data, 0o600)
	})
}

// Uninstall removes <root>/ytrack: the symlink itself when it is one, else
// the directory, which must hold the ytrack skill; anything else there is
// refused with a *ForeignError and left untouched. It reports whether there
// was anything to remove.
func Uninstall(root string) (bool, error) {
	path := filepath.Join(root, Name)
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return true, os.Remove(path)
	}
	if _, _, err := resolveTarget(path); err != nil {
		return false, err
	}
	return true, os.RemoveAll(path)
}
