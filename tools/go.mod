// Development tools only (`go tool -modfile=tools/go.mod <name>`), kept apart
// from ../go.mod so their dependencies never change what ytrack is built with.
// Dependabot keeps them current (.github/dependabot.yml).
module github.com/BrainerVirus/youtrack-cli/tools

go 1.27.1

tool (
	golang.org/x/tools/cmd/deadcode
	golang.org/x/vuln/cmd/govulncheck
)

require (
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
)
