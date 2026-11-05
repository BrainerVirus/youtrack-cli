# Contributing to ytrack

Thanks for helping. ytrack is a Go command-line client for JetBrains
YouTrack in the style of `gh` and `glab`. [AGENTS.md](AGENTS.md) is the full
layout and conventions; this page is the short version.

## Set up

You need Go (the version in `go.mod`; `GOTOOLCHAIN=auto` fetches the pinned
toolchain) and, for `make lint`, [golangci-lint](https://golangci-lint.run/) v2.

```sh
git clone https://github.com/BrainerVirus/youtrack-cli.git
cd youtrack-cli
make build        # bin/ytrack
```

Development tools (govulncheck, deadcode) are pinned in `tools/go.mod` and run
with `go tool -modfile=tools/go.mod <name>`; no separate install needed.

## Make a change

1. Branch from `main`. For anything larger than a small fix, open an issue
   first so the command shape can be agreed before you write it.
2. Keep the change focused, and write tests for the behaviour
   (Given/When/Then is a good shape). Tests never contact a real YouTrack or
   GitHub: use the `httptest` servers and helpers under `internal/`.
3. A new REST call goes in `api/contract/youtrack-contract.json`; tests
   check every request against it.
4. A new or renamed command or flag is reflected in the agent skill under
   `skills/ytrack/` (a test fails when the skill names something that does not
   exist) and in the README or `docs/`.
5. Verify:

   ```sh
   make fmt          # gofumpt + goimports
   make lint         # go vet, golangci-lint, go mod tidy, deadcode
   make test         # go test -race ./...
   make vulncheck    # if you changed dependencies
   ```

   `make it YTRACK_IT_PROJECT=<short>` runs the opt-in suite against your own
   YouTrack instance; read [docs/integration-tests.md](docs/integration-tests.md)
   first. It is never required for a PR.

## Commits and pull requests

- [Conventional Commits](https://www.conventionalcommits.org/): `feat`, `fix`,
  `docs`, `refactor`, `test`, `build`, `ci`, `chore`, with an optional scope
  (`feat(issue): …`). Mark a breaking change with `!`.
- PRs are squash-merged and **the PR title becomes the commit subject**, which
  the release notes are grouped from. Keep it a valid conventional commit.
- Fill in the PR template: what and why, scenarios, verification output,
  breaking notes.
- CI must be green: lint and tests on Linux, macOS and Windows on every
  change, plus govulncheck when Go code or dependencies change and workflow
  lint (actionlint, zizmor) when `.github/` changes.

Releases are cut by the maintainer with `make release VERSION=vX.Y.Z`; see the
README's Development section.

## Reporting bugs and security issues

Use the [issue forms](https://github.com/BrainerVirus/youtrack-cli/issues/new/choose)
and never paste tokens or private instance data. Report security
vulnerabilities privately, as [SECURITY.md](SECURITY.md) describes, never in a
public issue.
