package cmdutil

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// mainCheckoutDirs returns, for each of dirs inside a linked git worktree,
// the same folder in the repository's main checkout: the parent of the
// worktree's common git directory (what `git rev-parse --git-common-dir`
// names), joined with dir's path below the worktree root. It reads the .git
// file and the commondir file directly, so it needs no git binary. A main
// checkout, a submodule and a folder outside any repository give nothing.
func mainCheckoutDirs(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if m, ok := mainCheckoutDir(d); ok && !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	return out
}

func mainCheckoutDir(dir string) (string, bool) {
	for root := dir; ; {
		dotGit := filepath.Join(root, ".git")
		if fi, err := os.Stat(dotGit); err == nil {
			if fi.IsDir() {
				return "", false // a main checkout
			}
			return linkedWorktreeMain(dotGit, root, dir)
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", false
		}
		root = parent
	}
}

// linkedWorktreeMain reads the .git file dotGit of the worktree at root
// ("gitdir: <dir>") and that git directory's commondir file, and maps dir
// into the main checkout. A git directory without a commondir file (a
// submodule's) is not a linked worktree.
func linkedWorktreeMain(dotGit, root, dir string) (string, bool) {
	gitdir, ok := readPathFile(dotGit, "gitdir: ", root)
	if !ok {
		return "", false
	}
	common, ok := readPathFile(filepath.Join(gitdir, "commondir"), "", gitdir)
	if !ok {
		return "", false
	}
	main := filepath.Dir(common)
	if filepath.Base(common) != ".git" {
		// A bare repository: there is no checkout to map dir into, so the
		// folder holding the repository stands for it.
		return main, true
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return main, true
	}
	return filepath.Join(main, rel), true
}

// readPathFile reads a one-line file holding a path after prefix; a relative
// path is taken against base.
func readPathFile(name, prefix, base string) (string, bool) {
	b, err := os.ReadFile(name)
	if err != nil {
		return "", false
	}
	line, _, _ := strings.Cut(string(b), "\n")
	p, ok := strings.CutPrefix(strings.TrimSpace(line), prefix)
	if !ok || p == "" {
		return "", false
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p), true
}
