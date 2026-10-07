# ytrack

An unofficial command-line interface for JetBrains YouTrack, in the style of
`gh` and `glab`. Not affiliated with or endorsed by JetBrains.

Status: early. This release covers authentication and the raw `ytrack api`
command; issue and work-item commands come next.

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
`credentials.yml` (mode 0600) in the config directory.

```sh
ytrack auth status        # hosts, accounts, masked tokens; exit 4 if not logged in
ytrack auth switch --host tools.acme.com/youtrack
ytrack auth token         # print the token (only on request)
ytrack auth logout
```

## Raw API

```sh
ytrack api /users/me --fields login,fullName
ytrack api /issues -f query='project: APP #Unresolved' --fields idReadable,summary --paginate
ytrack api /issues --fields idReadable --jq '.[].idReadable'
ytrack api /issues -F 'project[id]=0-0' -f summary='New issue'
ytrack api /commands --input command.json
```

`/issues` and `/api/issues` are the same path. `--paginate` pages with
`$skip`/`$top` and prints one JSON array. See `ytrack api --help`.

## Scripting

- Data goes to stdout; errors and prompts to stderr. No color or prompts when
  output is not a terminal (`NO_COLOR` also disables color).
- `--json <fields>` (where offered), `--jq` and `--template` follow `gh`.
- Exit codes: 0 success, 1 failure (including usage errors), 2 cancelled,
  4 authentication required.
- Environment: `YTRACK_HOST`, `YTRACK_TOKEN` (runtime override, never saved),
  `YTRACK_DEBUG=1` (request log with credentials redacted, same as `--debug`),
  `YTRACK_CONFIG_DIR`, `YTRACK_BROWSER`.
- Host precedence: `--host`, `YTRACK_HOST`, the default host. Token
  precedence: `YTRACK_TOKEN`, then the stored token.

Configuration lives in `~/.config/ytrack/` (`$XDG_CONFIG_HOME/ytrack`,
`%AppData%\ytrack` on Windows): `config.yml` and `hosts.yml`, which never
holds tokens.

Shell completion: `ytrack completion bash|zsh|fish|powershell`.

## Development

```sh
make build test lint
```

See [AGENTS.md](AGENTS.md) for layout and conventions.

## License

MIT. Parts are adapted from [GitHub CLI](https://github.com/cli/cli) (MIT);
see [NOTICE](NOTICE).
