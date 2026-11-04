package update

import (
	"path/filepath"
	"strings"
)

// Manager is a package manager that owns an installed ytrack binary.
type Manager struct {
	Name    string // "Homebrew", "Scoop", "WinGet"
	Command string // the command that upgrades ytrack with it
}

// managers maps path fragments (lower case, forward slashes) to the package
// manager whose install layout contains them.
var managers = []struct {
	fragments []string
	manager   Manager
}{
	{
		[]string{"/cellar/", "/caskroom/", "/opt/homebrew/", "/home/linuxbrew/.linuxbrew/", "/.linuxbrew/"},
		Manager{Name: "Homebrew", Command: "brew upgrade brainervirus/tap/ytrack"},
	},
	{
		[]string{"/scoop/apps/", "/scoop/shims/"},
		Manager{Name: "Scoop", Command: "scoop update ytrack"},
	},
	{
		[]string{"/microsoft/winget/packages/", "/microsoft/winget/links/"},
		Manager{Name: "WinGet", Command: "winget upgrade ytrack"},
	},
}

// DetectManager returns the package manager that installed the binary at
// any of paths (typically the executable path and its symlink target), or
// nil when none did.
func DetectManager(paths ...string) *Manager {
	for _, p := range paths {
		if p == "" {
			continue
		}
		norm := strings.ToLower(filepath.ToSlash(p))
		norm = strings.ReplaceAll(norm, `\`, "/")
		for _, m := range managers {
			for _, f := range m.fragments {
				if strings.Contains(norm, f) {
					mgr := m.manager
					return &mgr
				}
			}
		}
	}
	return nil
}
