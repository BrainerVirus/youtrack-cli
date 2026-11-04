# ytrack

An unofficial command-line interface for JetBrains YouTrack, in the style of
`gh` and `glab`. Not affiliated with or endorsed by JetBrains.

Status: early. This release covers authentication, the raw `ytrack api`
command, reading, creating, editing and commenting on issues, YouTrack
commands on one or many issues, and time tracking (work items).

## Install

Linux and macOS, no package manager or sudo needed (installs to `~/.local/bin`):

```sh
curl -fsSL https://raw.githubusercontent.com/BrainerVirus/youtrack-cli/main/install.sh | sh
```

Windows (PowerShell; installs to `%LOCALAPPDATA%\Programs\ytrack`):

```powershell
irm https://raw.githubusercontent.com/BrainerVirus/youtrack-cli/main/install.ps1 | iex
```

Both scripts pick the archive for your OS and CPU, check its SHA-256 against
the release's checksums file, check that file's cosign signature when
`cosign` is installed, and print a hint if the install directory is not on
your `PATH`. `YTRACK_VERSION=v0.3.0` installs a given release and
`YTRACK_INSTALL_DIR` changes the directory. See [docs/install.md](docs/install.md).

Homebrew (macOS and Linux):

```sh
brew install brainervirus/tap/ytrack
```

With Go:

```sh
go install github.com/BrainerVirus/youtrack-cli/cmd/ytrack@latest
```

Or download an archive for your platform (Linux, macOS, Windows; amd64 or
arm64) from the [releases page](https://github.com/BrainerVirus/youtrack-cli/releases),
unpack it and put `ytrack` on your `PATH`. Archives also contain shell
completions for bash, zsh and fish. Each release has a
`ytrack_<version>_checksums.txt` with SHA-256 sums, signed with cosign by the
release workflow; [docs/install.md](docs/install.md#verifying-a-download)
shows how to check both.

Or build from a clone with `make build` (binary in `bin/ytrack`).

More: [docs/install.md](docs/install.md) covers the install scripts and their
options, [verifying a download](docs/install.md#verifying-a-download),
[upgrades](docs/install.md#upgrades) and the
[new-release notice](docs/install.md#new-release-notice).

## Upgrade

```sh
ytrack upgrade                 # to the latest release
ytrack upgrade --check         # only say whether a newer release exists
ytrack upgrade --version v0.3.0
```

`ytrack upgrade` replaces the running binary only after the release's
checksums file passes a cosign keyless signature check pinned to this
repository's release workflow and the archive matches its SHA-256; any
mismatch refuses and changes nothing. If Homebrew, Scoop or WinGet installed
ytrack, it prints that package manager's upgrade command instead.

Once a day, after a command, ytrack tells you on stderr when a newer release
exists. It stays quiet in CI, when stderr is not a terminal and under `--json`;
turn it off with `YTRACK_NO_UPDATE_NOTIFIER=1` or `check_update: false` in
`config.yml`. Details: [docs/install.md](docs/install.md#upgrades).

## Log in

```sh
ytrack auth login
```

ytrack asks for your YouTrack URL (`acme.youtrack.cloud`, or a Server URL
such as `https://tools.acme.com/youtrack`) and opens your profile's
**Account Security** tab. Create a permanent token named `ytrack` with the
scope `YouTrack` and paste it at the hidden prompt. If your Server uses an
external Hub, ytrack links to the
[token docs](https://www.jetbrains.com/help/youtrack/cloud/manage-permanent-token.html) instead.

Scripted:

```sh
ytrack auth login --host acme.youtrack.cloud --with-token < token.txt
```

The token is checked against `/api/users/me` and saved in the OS keyring
(macOS Keychain, Windows Credential Manager, Secret Service on Linux). With no
keyring, login fails unless you pass `--insecure-storage`, which writes
`credentials.yml` (mode 0600) in the config directory. On Windows the mode
bits are not enforced; the file is protected by the ACL of your
`%AppData%\ytrack` folder, which by default only your account can read.

```sh
ytrack auth status        # hosts, accounts, masked tokens; exit 4 if not logged in
ytrack auth switch --host tools.acme.com/youtrack
ytrack auth token         # print the token (only on request)
ytrack auth logout
```

## Issues

```sh
ytrack issue list -q 'project: APP for: me #Unresolved'      # native query syntax
ytrack issue list --project APP --assignee me --state Open --sort 'updated desc'
ytrack issue list --project APP --all                         # resolved too
ytrack issue list -q '#Unresolved' --json idReadable,summary,state,url
ytrack issue view APP-123 --comments
ytrack issue view https://acme.youtrack.cloud/issue/APP-123 --web
ytrack issue comment APP-123 --body 'Reproduced on 2026.2'
git log -1 --format=%B | ytrack issue comment APP-123 --body-file -
```

Without `--query` or `--state`, `issue list` shows only unresolved issues
(it adds `#Unresolved`); `--all` includes resolved ones. `--json` with no
fields lists the available ones. Only the attributes needed
for the table or the requested fields are fetched. In `issue list`, `-q` is
`--query`; use `--jq` for jq. `--limit` defaults to 30; `--limit 0` lists all.
The editor for `issue comment --editor` is `$YTRACK_EDITOR`, `$GIT_EDITOR`,
`$VISUAL` or `$EDITOR`.

### Commands

```sh
ytrack issue create --project APP --summary 'Login fails on Safari'
ytrack issue edit APP-123 --summary 'Login fails on Safari 18' --field 'Priority=Critical'
ytrack issue command APP-123 'State In Progress for me'
ytrack issue command APP-123 APP-124 APP-130 'Assignee me' --dry-run   # preview only
ytrack issue command APP-123 APP-124 'tag regression' --json issues,commands
```

`issue command` takes YouTrack's own [command
language](https://www.jetbrains.com/help/youtrack/cloud/commands.html). The
last argument is the command and every argument before it is an issue; all of
them change in one request. `--dry-run` asks YouTrack how it parses the
command and changes nothing (exit 1 when a part is not understood);
`--comment` adds a comment with the change and `--silent` skips
notifications. `--json` reports `issue`, `url`, `issues` (every issue with its
URL), `command`, `dryRun` and `commands` (the parsed parts).

## Time tracking

```sh
ytrack work-item list APP-123 --author me --since 2026-10-01
ytrack work-item add APP-123 --duration 1h30m --text 'Code review'
ytrack work-item add APP-123 --duration 1.5h --date 2026-10-06 --type Development
ytrack work-item add TEAM-1 --duration 30m --meeting        # text "Meetings"
ytrack work-item edit APP-123 115-3 --duration 2h
ytrack work-item delete APP-123 115-3 --yes
```

`--duration` takes `1h30m`, `1h 30m`, `90m`, `1.5h`, `2 hours 15 minutes` or
a bare number of minutes. Days and weeks are refused (their length depends on
the instance's work schedule). `--date auto` (the default) is today in your
work timezone: `timezone: Europe/Madrid` in `config.yml` when set, otherwise
the system timezone (`TZ`). YouTrack stores the day only, at midnight UTC.
`--type` takes a work item type name from the project's time tracking
settings; an unknown name lists the valid ones. `delete` asks first in a
terminal and needs `--yes` otherwise.

## Raw API

```sh
ytrack api /users/me --fields login,fullName
ytrack api /issues -f query='project: APP #Unresolved' --fields idReadable,summary --paginate
ytrack api /issues --fields idReadable --jq '.[].idReadable'
ytrack api /issues -F 'project[id]=0-0' -f summary='New issue'
ytrack api /commands --input command.json
```

`/issues` and `/api/issues` are the same path. `--paginate` pages with
`$skip`/`$top` and prints one JSON array. The token is sent only to the
configured service URL: redirects to other hosts and from https to http are
refused. Plain-http hosts other than localhost trigger a warning. See
`ytrack api --help`.

## Scripting

- Data goes to stdout; errors and prompts to stderr. No color or prompts when
  output is not a terminal (`NO_COLOR` also disables color).
- `--json <fields>` (where offered), `--jq` and `--template` follow `gh`.
- Exit codes: 0 success, 1 failure (including usage errors), 2 cancelled,
  4 authentication required.
- Environment: `YTRACK_HOST`, `YTRACK_TOKEN` (runtime override, never saved),
  `YTRACK_DEBUG=1` (request log with credentials redacted, same as `--debug`),
  `YTRACK_CONFIG_DIR`, `YTRACK_BROWSER`, `YTRACK_EDITOR`,
  `YTRACK_NO_UPDATE_NOTIFIER=1` (no new-release notice).
- Host precedence: `--host`, `YTRACK_HOST`, the default host. Token
  precedence: `YTRACK_TOKEN`, then the stored token. `YTRACK_TOKEN` goes to
  a host named by `--host` or `YTRACK_HOST` without a warning (the CI setup),
  and to a host taken from a pasted issue URL only when that host is logged in.

Configuration lives in `~/.config/ytrack/` (`$XDG_CONFIG_HOME/ytrack`,
`%AppData%\ytrack` on Windows): `config.yml` (`browser`, `timezone`,
`check_update`) and `hosts.yml`, which never holds tokens. The daily update
check keeps its state in `~/.local/state/ytrack/state.yml`
(`$XDG_STATE_HOME/ytrack`, `%LOCALAPPDATA%\ytrack` on Windows).

Shell completion: `ytrack completion bash|zsh|fish|powershell`.

## Development

```sh
make build test lint
make snapshot              # local release dry run into dist/ (needs goreleaser v2)
make it YTRACK_IT_PROJECT=<short>   # opt-in suite against your own YouTrack instance
```

`make it` runs the real-instance suite with the session from
`ytrack auth login` against a personal scratch project: it creates issues
prefixed `ytrack-it`, works on them and deletes them, and refuses hosts other
than `*.youtrack.cloud` unless you allow one. See [docs/integration-tests.md](docs/integration-tests.md) before
running it.

Releases: from an up-to-date `main`, run `make release VERSION=v0.1.0`. It
runs the tests, tags `main` and pushes the tag; the `Release` workflow then
builds the archives with goreleaser, signs the checksums file with cosign
keyless (the workflow's GitHub OIDC identity), publishes the GitHub release
with a changelog grouped from the conventional commit titles, and updates the
Homebrew cask when the `HOMEBREW_TAP_GITHUB_TOKEN` secret is set (the upload
is skipped without it).

To rerun a release, dispatch the workflow from the tag itself, never from a
branch: `gh workflow run release.yml --ref v0.1.0 -f tag=v0.1.0`. The
signature's identity is the ref the run was dispatched from, and
`ytrack upgrade` and the install scripts accept only `refs/tags/vX.Y.Z[-pre]`;
the workflow refuses a dispatch whose ref is not the tag it releases.

The REST surface ytrack relies on is listed by hand in
`api/contract/youtrack-contract.json`, derived from JetBrains' public
[YouTrack REST API docs](https://www.jetbrains.com/help/youtrack/devportal/youtrack-rest-api.html).
Tests check every request against it, and a weekly workflow checks it against
the live API description.

See [AGENTS.md](AGENTS.md) for layout and conventions.

## License

MIT. Parts are adapted from [GitHub CLI](https://github.com/cli/cli) (MIT);
see [NOTICE](NOTICE).
