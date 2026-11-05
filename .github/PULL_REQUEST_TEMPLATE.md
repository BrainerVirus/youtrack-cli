<!-- PR title = squash commit subject and release-notes line: a Conventional Commit, e.g. `feat(issue): add --assignee to issue list`. Add `!` for a breaking change. -->

## What and why

<!-- What changes, and the problem it solves. Link the issue (`Closes #n`). -->

## Scenarios covered

<!-- Given / When / Then for the behaviour this PR adds or changes, and the test for each. -->

- Given …, when …, then … (`…_test.go`)

## Verification

<!-- Evidence, not claims: paste command output. -->

- [ ] `make lint` (go vet, golangci-lint, go mod tidy, deadcode) and `gofmt -l .` clean
- [ ] `make test` (`go test -race ./...`)
- [ ] `make vulncheck` if dependencies changed
- [ ] Tried the command against a real YouTrack (or `make it`), if it calls a new endpoint

```text

```

## Breaking changes

<!-- None, or what breaks and how users migrate (also add `!` to the title). -->

None.

## Checklist

- [ ] README / `docs/` updated for user-facing changes
- [ ] New REST calls are listed in `api/contract/youtrack-contract.json`
- [ ] The agent skill (`skills/ytrack/`) still matches the commands and flags
- [ ] No token, cookie or private instance data in code, tests or this PR
