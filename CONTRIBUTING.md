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
   exist) and in the README or `docs/`. A change to the skill's text is
   measured with the skill bench before and after
   (`go run ./bench --model claude-haiku-4-5 --repeat 3`, then
   `go run ./bench compare`); see [bench/README.md](bench/README.md).
5. Verify:

   ```sh
   make fmt          # gofumpt + goimports
   make lint         # go vet, golangci-lint, go mod tidy, deadcode
   make test         # go test -race ./...
   make vulncheck    # if you changed dependencies
   ```

   `make test` includes the drift tests: every `ytrack` command and flag
   named in the skill, the README and `docs/` must exist in the command tree.

   `make it YTRACK_IT_PROJECT=<short>` runs the opt-in suite against your own
   YouTrack instance (add `YTRACK_IT_HOST=<name>.youtrack.cloud` when it is
   not your default host): it creates issues prefixed `ytrack-it` and tags
   named `ytrack-it-<run id>-*`, works on them and deletes them, and refuses
   hosts other than `*.youtrack.cloud` unless you allow one. Read [docs/integration-tests.md](docs/integration-tests.md)
   first. It is never required for a PR.

   `make snapshot` is a local release dry run into `dist/` (needs goreleaser
   v2).

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

## The skill bench

`go run ./bench --model claude-haiku-4-5 --repeat 3` gives Claude Code
YouTrack tasks in throwaway projects with the skill installed and a fake
`ytrack`, and grades what the model did (pass rate and tokens per case). It
needs a Claude Code login, spends subscription usage and never runs in CI;
`make test` runs only its unit tests. Compare a skill change against a
baseline with `go run ./bench compare <before> <after>`. Details, flags and
cases: [bench/README.md](bench/README.md).

## The REST contract

The REST surface ytrack relies on is listed by hand in
`api/contract/youtrack-contract.json`, derived from JetBrains' public
[YouTrack REST API docs](https://www.jetbrains.com/help/youtrack/devportal/youtrack-rest-api.html).
Tests check every request against it, and the weekly `openapi-contract`
workflow checks it against the live API descriptions of youtrack.jetbrains.com
and a YouTrack Cloud instance (YouTrack Server has no public instance to
download one from).

- `types` lists the attributes ytrack reads or writes; `writes` lists, by
  entity, the attributes ytrack sets at the top level of a JSON request body.
  A request that writes an attribute missing from `writes` fails the tests,
  and the weekly check fails when the description marks a written attribute
  `readOnly`. An endpoint with `"body": "reference"` adds an existing entity
  to a collection (a tag, a link), so its body writes nothing. A write that
  YouTrack accepts although the description marks it `readOnly` goes in
  `readOnlyWrites` with its evidence.
- `api/contract/openapi-snapshot.json` is a digest of the
  youtrack.jetbrains.com description (its operations and the attributes of
  the contract's entities). The weekly run lists what changed upstream
  against it (new, removed or deprecated) in the run summary without
  failing. Refresh it with
  `YTRACK_OPENAPI=<openapi.json> YTRACK_OPENAPI_SNAPSHOT_UPDATE=1 go test ./internal/youtrack/contract -run '^TestUpstreamChanges$'`.
- Run the weekly check locally against a downloaded description with
  `YTRACK_OPENAPI=<openapi.json> go test ./internal/youtrack/adapter/ -run OpenAPI`.

## Releases

The maintainer cuts releases from an up-to-date `main` with
`make release VERSION=vX.Y.Z`. It runs the tests, tags `main` and pushes the
tag; the `Release` workflow then builds the archives with goreleaser, signs
the checksums file with cosign keyless (the workflow's GitHub OIDC identity),
publishes the GitHub release with notes that start with the version's
`CHANGELOG.md` section, and updates the Homebrew cask when the
`HOMEBREW_TAP_GITHUB_TOKEN` secret is set (the upload is skipped without it).

To rerun a release, dispatch the workflow from the tag itself, never from a
branch: `gh workflow run release.yml --ref v0.7.0 -f tag=v0.7.0`. The
signature's identity is the ref the run was dispatched from, and
`ytrack upgrade` and the install scripts accept only `refs/tags/vX.Y.Z[-pre]`;
the workflow refuses a dispatch whose ref is not the tag it releases.

## Reporting bugs and security issues

Use the [issue forms](https://github.com/BrainerVirus/youtrack-cli/issues/new/choose)
and never paste tokens or private instance data. Report security
vulnerabilities privately, as [SECURITY.md](SECURITY.md) describes, never in a
public issue.
