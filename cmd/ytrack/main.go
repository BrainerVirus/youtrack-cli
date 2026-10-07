// Command ytrack is an unofficial command-line interface for JetBrains YouTrack.
package main

import (
	"os"

	"github.com/BrainerVirus/youtrack-cli/internal/build"
	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/factory"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/root"
)

func main() {
	f := factory.New(build.Version, iostreams.System())
	os.Exit(int(root.Execute(f, os.Args[1:])))
}
