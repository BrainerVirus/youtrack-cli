# ytrack

[![CI](https://github.com/BrainerVirus/youtrack-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/BrainerVirus/youtrack-cli/actions/workflows/ci.yml)
[![Vulnerabilities](https://github.com/BrainerVirus/youtrack-cli/actions/workflows/vulncheck.yml/badge.svg)](https://github.com/BrainerVirus/youtrack-cli/actions/workflows/vulncheck.yml)
[![Release](https://img.shields.io/github/v/release/BrainerVirus/youtrack-cli?sort=semver)](https://github.com/BrainerVirus/youtrack-cli/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/BrainerVirus/youtrack-cli)](go.mod)
[![license: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

An unofficial command-line interface for JetBrains YouTrack (Cloud and
Server), in the style of `gh` and `glab`. Not affiliated with or endorsed by
JetBrains.

It reads, searches, creates and edits issues, comments with attachments,
links issues, applies YouTrack commands, tracks time, calls the raw REST API,
and ships an [agent skill](#agent-skill) that teaches coding agents to do the
same. It keeps itself up to date with signed releases.

## Install

Linux and macOS, no package manager or sudo needed (installs to `~/.local/bin`):

```sh
curl -fsSL https://raw.githubusercontent.com/BrainerVirus/youtrack-cli/main/install.sh | sh
```

Windows (PowerShell; installs to `%LOCALAPPDATA%\Programs\ytrack`):

```powershell
irm https://raw.githubusercontent.com/BrainerVirus/youtrack-cli/main/install.ps1 | iex
```

Or `brew install brainervirus/tap/ytrack`, or
`go install github.com/BrainerVirus/youtrack-cli/cmd/ytrack@latest`.

The scripts check the archive's SHA-256 against the release's checksums file
(and that file's cosign signature when `cosign` is installed) and install
shell completions. Options, archives, verifying a download and upgrades:
[docs/install.md](docs/install.md).

## Log in

```sh
ytrack auth login
```

ytrack asks for your YouTrack URL (`acme.youtrack.cloud`, or a Server URL
such as `https://tools.acme.com/youtrack`) and opens your profile's
**Account Security** tab. Create a permanent token with the scope `YouTrack`
and paste it at the hidden prompt. The token goes to the OS keyring, never to
a command line. Scripted:
`ytrack auth login --host acme.youtrack.cloud --with-token < token.txt`.

Several instances? Tie each to your folders with `ytrack auth paths`; see
[docs/auth.md](docs/auth.md).

## Five-minute tour

```sh
ytrack auth status                                        # who you are, on which host
ytrack issue list --project APP --assignee me             # your open issues
ytrack issue list -q 'project: APP #Unresolved tag: regression'   # any YouTrack query
ytrack issue view APP-123 --comments
ytrack issue create --project APP --summary 'Login fails on Safari' --field Type=Bug
ytrack issue edit APP-123 --field 'Priority=Critical' --add-tag regression
ytrack issue comment APP-123 --body 'Fixed: ![](after.png)' --attach after.png
ytrack issue command APP-123 'State Fixed' --dry-run      # preview a YouTrack command
ytrack issue link APP-123 APP-7 --type 'depends on'
ytrack work-item add APP-123 --duration 1h30m --text 'Code review'
ytrack work-item report --since 2026-10-01                # your time per issue and per day
ytrack issue count 'project: APP #Unresolved'
ytrack api /users/me --fields login,fullName              # anything else
```

```text
$ ytrack issue list --project APP --assignee me
ID       SUMMARY                STATE        PRIORITY  ASSIGNEE  UPDATED
APP-124  Login fails on Safari  In Progress  Critical  jdoe      about 2 hours ago
APP-118  Export to CSV          Open         Normal    jdoe      about 3 days ago
```

For scripts and agents, commands take `--json <fields>`, `--jq` and
`--template` like `gh`:

```sh
ytrack issue list -q '#Unresolved' --json idReadable,summary --jq '.[].idReadable'
```

Every command's `--help` lists its flags and JSON fields, and a guessed flag
fails with a hint:

```text
$ ytrack issue list --limt 5
ytrack: unknown flag: --limt
did you mean --limit?
```

## Agent skill

The `ytrack` [agent skill](skills/ytrack/SKILL.md) stops coding agents from
guessing `gh` flags or scraping the web page, and never has them handle your
token. Install it for Claude Code, Codex, Cursor, OpenCode and Pi:

```sh
ytrack skill install          # ~/.agents/skills and ~/.claude/skills, kept at ytrack's version
npx skills add BrainerVirus/youtrack-cli                  # or with skills.sh
```

[Workit](https://github.com/BrainerVirus/workit) installs it for the agents
it sets up. Details: [docs/agent-skill.md](docs/agent-skill.md).

## Upgrade

```sh
ytrack upgrade                 # to the latest release
ytrack upgrade --check         # only say whether a newer release exists
```

Upgrades check a cosign signature pinned to this repository's release
workflow and the archive's SHA-256 before replacing anything. ytrack also
upgrades itself in the background once a newer release is a day old; turn
that off with `auto_upgrade: false` in `config.yml` or
`YTRACK_NO_AUTO_UPGRADE=1`. Homebrew, Scoop and WinGet installs are left to
their package manager. See [docs/install.md](docs/install.md#upgrades).

## Reference

| Topic | |
| --- | --- |
| Login, tokens, several hosts, folder rules, CI | [docs/auth.md](docs/auth.md) |
| Issues: list, view, count, history, watchers, votes, saved searches, create, edit, delete, comments, attachments, commands, links, tags, project fields | [docs/issues.md](docs/issues.md) |
| Time tracking (work items, time reports) | [docs/time-tracking.md](docs/time-tracking.md) |
| Raw REST API (`ytrack api`) | [docs/api.md](docs/api.md) |
| Output, JSON, exit codes, environment, config files, completion | [docs/scripting.md](docs/scripting.md) |
| Install, verify, upgrade, background upgrade | [docs/install.md](docs/install.md) |
| Agent skill | [docs/agent-skill.md](docs/agent-skill.md) |

Exit codes: 0 success, 1 failure (including usage errors), 2 cancelled,
4 authentication required.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) to build, test and release,
[AGENTS.md](AGENTS.md) for layout and conventions, and
[SECURITY.md](SECURITY.md) to report a vulnerability.

## License

MIT. Parts are adapted from [GitHub CLI](https://github.com/cli/cli) (MIT);
see [NOTICE](NOTICE).
