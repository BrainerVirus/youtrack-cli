# AGENTS.md

`ytrack`: unofficial YouTrack CLI in Go + Cobra, modelled on `gh`.
Spec: `docs/YT_CLI_SPEC.md` (its Decisions section wins over the body).

## Commands
- `make build` → `bin/ytrack`; `make test` (`go test -race ./...`)
- `make lint` (`go vet` + `golangci-lint run`); `make fmt` (gofumpt, goimports)

## Layout
- `cmd/ytrack` entry point; `pkg/cmd/<group>/<verb>` one package per command
- `pkg/cmd/root` command tree and `Execute` (error printing, exit codes)
- `pkg/cmdutil` Factory (DI), arg validators, `--json/--jq/--template` flags
- `internal/youtrack/transport` HTTP, auth header, redacted debug log, `APIError`
- `internal/youtrack/adapter` typed API calls; commands never parse raw wire JSON
- `internal/youtrack/adapter/customfields` the only decoder of custom field `$type`s
- `internal/youtrack/transport.Paginate` the shared `$skip/$top` pager
- `api/openapi/youtrack.json` REST snapshot; `adapter/contract_test.go` checks requests against it
- `internal/{config,hosts,auth}` YAML config, URL normalization, keyring store
- `internal/{iostreams,output,prompter,browser,editor,clierr,build}`
- `internal/cmdtest` in-process command runner + fake YouTrack server for tests

## Conventions
- Get IO, config, HTTP, browser and prompts from the Factory; no globals.
- Data to stdout, everything else to stderr; never prompt unless `CanPrompt()`.
- Exit codes (contract, follow gh): 0 ok, 1 failure incl. usage, 2 cancelled,
  4 auth required. Return `clierr` errors; do not call `os.Exit` in commands.
- Tokens never in argv, logs, hosts.yml or errors. Tests assert this.
- `--json` field names are domain names, listed per command.
- Tests: BDD names ("given X, it Y"), command tests via `cmdtest`, keyring via
  `keyring.MockInit()`; assert behavior, not implementation.
- Code adapted from gh/glab keeps a header comment and a NOTICE entry.
- Conventional commits.
