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
// place, so a failed install leaves the earlier skill as it was.
func Install(root, version string) (string, error) {
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
	dir, exists, err := resolveTarget(path)
	if err != nil {
		return "", err
	}

	parent := filepath.Dir(dir)
	stage, err := os.MkdirTemp(parent, ".ytrack-skill-new-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }() // gone already after a successful rename
	if err := writeTree(stage, skillMD); err != nil {
		return "", err
	}
	if !exists {
		return path, rename(stage, dir)
	}

	old, err := os.MkdirTemp(parent, ".ytrack-skill-old-")
	if err != nil {
		return "", err
	}
	// MkdirTemp reserved a unique name; rename needs it free.
	if err := os.Remove(old); err != nil {
		return "", err
	}
	if err := rename(dir, old); err != nil {
		return "", err
	}
	if err := rename(stage, dir); err != nil {
		_ = rename(old, dir)
		return "", err
	}
	return path, os.RemoveAll(old)
}

// resolveTarget returns the directory to write: path itself, or the target
// of a symlink at path. exists reports whether it holds the ytrack skill
// already; a directory holding anything else is an error.
func resolveTarget(path string) (dir string, exists bool, err error) {
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
