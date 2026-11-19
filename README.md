# ytrack

[![CI](https://github.com/BrainerVirus/youtrack-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/BrainerVirus/youtrack-cli/actions/workflows/ci.yml)
[![Vulnerabilities](https://github.com/BrainerVirus/youtrack-cli/actions/workflows/vulncheck.yml/badge.svg)](https://github.com/BrainerVirus/youtrack-cli/actions/workflows/vulncheck.yml)
[![Release](https://img.shields.io/github/v/release/BrainerVirus/youtrack-cli?sort=semver)](https://github.com/BrainerVirus/youtrack-cli/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/BrainerVirus/youtrack-cli)](go.mod)
[![license: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

An unofficial command-line interface for JetBrains YouTrack, in the style of
`gh` and `glab`. Not affiliated with or endorsed by JetBrains.

Status: early. This release covers authentication, the raw `ytrack api`
command, reading, creating, editing and commenting on issues, YouTrack
commands on one or many issues, time tracking (work items), and an
[agent skill](#agent-skill) that teaches coding agents to use ytrack.

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
your `PATH`. `install.sh` also installs shell completions for zsh, bash or
fish. `YTRACK_VERSION=v0.3.0` installs a given release and
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
mismatch refuses and changes nothing. Failed downloads are retried, then
fetched through the GitHub API, under the same checks. If Homebrew, Scoop or WinGet installed
ytrack, it prints that package manager's upgrade command instead. The new
binary must pass `ytrack version` before it is swapped in.

ytrack keeps itself current: once a day it checks GitHub for a newer release
(also when agents run it without a terminal or with `--json`), and once that
release is a day old it upgrades itself in the background, with the same
signature and checksum checks, then refreshes its agent skills
(`ytrack skill sync`). On a terminal it tells you on stderr when a newer
release exists, when a background upgrade failed, and once after one
succeeded. Turn the background upgrade off with `auto_upgrade: false` in
`config.yml` or `YTRACK_NO_AUTO_UPGRADE=1`, and both the check and the upgrade
with `check_update: false` or `YTRACK_NO_UPDATE_NOTIFIER=1`. It never runs in
CI or for Homebrew, Scoop or WinGet installs. Details:
[docs/install.md](docs/install.md#upgrades).

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

With more than one host, tie each to the folders you work in; a command run
inside one of them talks to that host:

```sh
ytrack auth paths --host work.youtrack.cloud --add '~/work/**'
ytrack auth paths --host acme.youtrack.cloud --add '~/oss/acme/**'
ytrack auth paths         # every host's paths
ytrack auth status        # shows which host this folder uses and why
```

A path is absolute or starts with `~/`; `*` matches within one folder name
and `**` any number of folders (`~/work/**` also matches `~/work`). When
paths of several hosts match, the one with the longest text before its first
wildcard wins. The paths live in `hosts.yml` next to each host.

On macOS and Windows, whose file systems ignore case by default, paths match
folders in any case: `~/Work/**` and `~/work/**` are one path, and adding the
second reports it as already present. Windows paths may use `\` and a drive
letter (`C:/Code/**` matches `c:\code\app`). On Linux and other systems case
matters. Two paths of different hosts that differ only by case are an error
when they are the most specific paths a folder matches: `ytrack` names both
and picks neither. A narrower path that also matches still wins.

## Issues

```sh
ytrack issue list -q 'project: APP Assignee: me #Unresolved' # native query syntax
ytrack issue list --project APP --assignee me --state Open --sort 'updated desc'
ytrack issue list --project APP --all                         # resolved too
ytrack issue list -q '#Unresolved' --json idReadable,summary,state,url
ytrack issue view APP-123 --comments
ytrack issue view https://acme.youtrack.cloud/issue/APP-123 --web
ytrack issue view https://acme.youtrack.cloud/projects/APP/issues/APP-123
ytrack issue comment APP-123 --body 'Reproduced on 2026.2'
git log -1 --format=%B | ytrack issue comment APP-123 --body-file -
ytrack issue comment APP-123 --body 'Fixed: ![](after.png)' --attach after.png --attach report.html
ytrack issue attach APP-123 dump.sql repro.sh               # prints name<TAB>id per file
ytrack issue attachments APP-123                             # name, id, size, type, created, author, comment
ytrack issue download APP-123 -o out/                        # prints name<TAB>bytes per file
```

An issue URL may be `/issue/APP-123`, with or without the title slug, or
`/projects/APP/issues/APP-123`; it selects its host. `issue comment --attach`
(repeatable) posts the comment, then uploads the files to it: the text shows
a file inline when it names it, as `![](after.png)` or
`[report](report.html)`, and the others are listed under the comment. If a
file ends up on the issue instead, the command warns and exits 0 (the
comment still shows a file it names); it exits 1 naming each file that is
neither on the comment nor shown by it. After either, do not run it again,
since the comment and the uploads exist: check with `issue attachments`.
`--json attachments` reports each file's state: `comment`, `issue` or
`failed`.
`issue attach` attaches files to the issue itself, streaming each regular
file from disk, so a large file is not held in memory; a file that changes
size during the upload fails it. A pipe such as `<(cmd)` is read into
memory first. A missing file stops either command before anything is sent.

`issue attachments` lists the files on an issue and on its comments
(`--json` gives the same fields). `issue download` saves them, or only those
named by name or attachment ID, or with `--comment <id>` the files of one
comment, into `-o/--dir` (the current folder by default), using the same
host as `issue view`. It streams each file to disk, never overwrites an
existing file unless `--clobber` is given, saves a name holding a path such
as `../x` as `.._x` (and says so), and exits 1 naming any file that failed.

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
notifications (it needs the *Apply Commands Silently* permission; on a 403,
retry without it). `--json` reports `issue`, `url`, `issues` (every issue with its
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
ytrack api /issues -X GET -f query='project: APP #Unresolved' --fields idReadable,summary --paginate
ytrack api /issues --fields idReadable --jq '.[].idReadable'
ytrack api /issues -F 'project[id]=0-0' -f summary='New issue'
ytrack api /commands --input command.json
```

`/issues` and `/api/issues` are the same path. `-f`, `-F` and `--input`
make the request a POST unless you pass `-X GET`, and a POST to `/issues`
creates an issue, so reads with parameters need `-X GET`. `--paginate` (GET
only) pages with `$skip`/`$top` and prints one JSON array. The token is sent only to the
configured service URL: redirects to other hosts and from https to http are
refused. Plain-http hosts other than localhost trigger a warning. See
`ytrack api --help`.

## Scripting

- Data goes to stdout; errors and prompts to stderr. On a terminal, help and
  output are colored: issue IDs, commands and flags in an accent color,
  summaries and headings bold, states and priorities by meaning (resolved
  green, open or in progress yellow, critical red), errors red with the
  command to run in bold. Color is off when the stream is not a terminal,
  under `NO_COLOR`, `TERM=dumb`, `--json` or the `--no-color` flag; no
  prompts when output is not a terminal.
- `--json <fields>` (where offered), `--jq` and `--template` follow `gh`.
- Exit codes: 0 success, 1 failure (including usage errors), 2 cancelled,
  4 authentication required.
- Environment: `YTRACK_HOST`, `YTRACK_TOKEN` (runtime override, never saved),
  `YTRACK_DEBUG=1` (request log with credentials redacted, same as `--debug`),
  `YTRACK_CONFIG_DIR`, `YTRACK_CACHE_DIR`, `YTRACK_BROWSER`, `YTRACK_EDITOR`,
  `YTRACK_NO_UPDATE_NOTIFIER=1` (no release check, notice or background
  upgrade), `YTRACK_NO_AUTO_UPGRADE=1` (no background upgrade).
- Host precedence: `--host`, the host of an issue URL argument,
  `YTRACK_HOST`, the host whose `auth paths` match the current folder (in any
  case on macOS and Windows, case-sensitively elsewhere), the default host.
  When the folder or the default host picked a host that does not have an
  issue key's project (`APP` in `APP-123`) and exactly one other logged-in
  host does, issue and work item commands use that host and print
  `using <host> for APP-* (the folder rule picked <other>)` on stderr. Reads
  try the picked host first and look the project up only after a 404; writes
  look it up first, so they never reach a host that does not own the key,
  and recheck the picked host before acting on a cached "it lacks the
  project". Answers are cached for 7 days per host URL and project in
  `~/.cache/ytrack/projects.json` (`$YTRACK_CACHE_DIR/projects.json`,
  `$XDG_CACHE_HOME/ytrack/projects.json`; `~/Library/Caches/ytrack` on macOS,
  `%LocalAppData%\ytrack` on Windows); delete that file to clear them. `--host`, `YTRACK_HOST`,
  an issue URL and `YTRACK_TOKEN` turn this off. With more than one logged-in
  host, HTTP errors name the host that answered and why it was chosen, and
  suggest `--host` for the others.
  Token
  precedence: `YTRACK_TOKEN`, then the stored token. `YTRACK_TOKEN` goes to
  a host named by `--host` or `YTRACK_HOST` without a warning (the CI setup),
  and to a host taken from a pasted issue URL only when that host is logged in.

Configuration lives in `~/.config/ytrack/` (`$XDG_CONFIG_HOME/ytrack`,
`%AppData%\ytrack` on Windows): `config.yml` (`browser`, `timezone`,
`check_update`, `auto_upgrade`) and `hosts.yml` (hosts, their `paths` and the default host),
which never holds tokens. The daily update
check keeps its state in `~/.local/state/ytrack/state.yml`
(`$XDG_STATE_HOME/ytrack`, `%LOCALAPPDATA%\ytrack` on Windows).

Shell completion: `install.sh` installs it for zsh, bash or fish (from
`$SHELL`) and prints how to enable it; `install.ps1` prints the PowerShell
profile line. Otherwise run `ytrack completion bash|zsh|fish|powershell`.
Completion suggests commands, flags, `--json` fields, issue IDs (with their
summaries) and `--project` names. See
[docs/install.md](docs/install.md#shell-completions).

## Agent skill

No coding agent was trained on ytrack, so agents guess `gh` habits
(`--hostname` instead of `--host`, `--title` instead of `--summary`) or
scrape the YouTrack web page. The `ytrack` [agent
skill](skills/ytrack/SKILL.md) teaches them the real commands: hosts and auth,
issue queries, create, edit and comment, YouTrack commands (`--dry-run` first,
locale-proof field names), work items, `--json`/`--jq`, `ytrack api` as the
escape hatch, and what to tell you on exit code 4. It never has an agent
handle your token.

With [skills.sh](https://skills.sh), for the agents it supports:

```sh
npx skills add BrainerVirus/youtrack-cli
```

Or from the binary, which installs the skill matching its own version
(`metadata.version` in `SKILL.md`) and keeps it there:

```sh
ytrack skill install                                 # ~/.agents/skills and ~/.claude/skills
ytrack skill install --dir ~/.cursor/skills          # one more agent dir
ytrack skill install --dir .claude/skills --json     # one project; prints {"path", "version"}
ytrack skill list                                    # every copy, its version and state
ytrack skill sync                                    # update the home copies now
ytrack skill uninstall --dir ~/.cursor/skills        # remove a copy and forget it
ytrack skill print                                   # SKILL.md on stdout
```

Without `--dir`, `ytrack skill install` writes `~/.agents/skills`, which
Codex, Cursor, OpenCode and Pi read, and `~/.claude/skills` for Claude Code
(with `--json`, one object per line). It writes `<dir>/ytrack/SKILL.md` and
`references/`, replacing an earlier ytrack skill there by renaming the new
copy into place; it refuses a `ytrack` directory that holds anything else and
leaves it untouched.

ytrack remembers every directory it installed the skill into (in
`~/.local/state/ytrack/skills.yml`), and on its first run also records
copies already in the agents' home skills directories. The first command a
newer ytrack release runs rewrites the copies in your home directory, so
after `ytrack upgrade` no agent reads an older skill; an older ytrack on
your PATH never rewrites a newer copy. A copy you deleted is
forgotten, not put back. A copy in a repository (a `--dir` inside a git work
tree) changes only when you install it there again: `ytrack skill list`
marks it `manual` and prints the command when it is stale. The refresh is
silent and never changes a command's output or exit code; where it cannot
write, `ytrack skill list` shows the copy as `stale`. All `skill` commands
work offline and logged out.

[Workit](https://github.com/BrainerVirus/workit) installs and updates the
skill for the agents it sets up, so with Workit you need neither command.

A test checks every `ytrack` command and flag the skill names against the
command tree, so the skill cannot drift from the binary.

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

See [CONTRIBUTING.md](CONTRIBUTING.md) to contribute, [AGENTS.md](AGENTS.md)
for layout and conventions, and [SECURITY.md](SECURITY.md) to report a
vulnerability.

## License

MIT. Parts are adapted from [GitHub CLI](https://github.com/cli/cli) (MIT);
see [NOTICE](NOTICE).
