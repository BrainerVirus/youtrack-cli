// Package build exposes version information injected at link time.
package build

import (
	"runtime/debug"
	"strings"
)

// Set with -ldflags "-X github.com/BrainerVirus/youtrack-cli/internal/build.Version=1.2.3 ...".
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func init() {
	// `go install module@version` builds carry the module version but no ldflags.
	if Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = info.Main.Version
		}
	}
	Version = strings.TrimPrefix(Version, "v")
}
