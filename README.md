# ytrack

An unofficial command-line interface for JetBrains YouTrack, in the style of
`gh` and `glab`. Not affiliated with or endorsed by JetBrains.

Status: early. This release covers authentication, the raw `ytrack api`
command, reading and commenting on issues, and time tracking (work items).

## Install

```sh
go install github.com/BrainerVirus/youtrack-cli/cmd/ytrack@latest
```

Or build from a clone with `make build` (binary in `bin/ytrack`).

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
  `YTRACK_CONFIG_DIR`, `YTRACK_BROWSER`, `YTRACK_EDITOR`.
- Host precedence: `--host`, `YTRACK_HOST`, the default host. Token
  precedence: `YTRACK_TOKEN`, then the stored token. `YTRACK_TOKEN` goes to
  a host named by `--host` or `YTRACK_HOST` without a warning (the CI setup),
  and to a host taken from a pasted issue URL only when that host is logged in.

Configuration lives in `~/.config/ytrack/` (`$XDG_CONFIG_HOME/ytrack`,
`%AppData%\ytrack` on Windows): `config.yml` (`browser`, `timezone`) and
`hosts.yml`, which never holds tokens.

Shell completion: `ytrack completion bash|zsh|fish|powershell`.

## Development

```sh
make build test lint
```

The REST surface ytrack relies on is listed by hand in
`api/contract/youtrack-contract.json`, derived from JetBrains' public
[YouTrack REST API docs](https://www.jetbrains.com/help/youtrack/devportal/youtrack-rest-api.html).
Tests check every request against it, and a weekly workflow checks it against
the live API description.

See [AGENTS.md](AGENTS.md) for layout and conventions.

## License

MIT. Parts are adapted from [GitHub CLI](https://github.com/cli/cli) (MIT);
see [NOTICE](NOTICE).
