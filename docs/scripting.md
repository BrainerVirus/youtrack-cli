# Scripting, output and configuration

## Output

Data goes to stdout; errors, notices and prompts to stderr. ytrack prompts
only on a terminal: without one, a command that needs an answer fails and
names the flag to pass instead (`--yes`, `--project`, ...).

On a terminal, help and output are colored: issue IDs, commands and flags in
an accent color, summaries and headings bold, states and priorities by
meaning (resolved green, open or in progress yellow, critical red), errors
red with the command to run in bold. Color is off when the stream is not a
terminal, under `NO_COLOR`, `CLICOLOR=0`, `TERM=dumb`, `--json` or
`--no-color`; `CLICOLOR_FORCE=1` turns it on through a pipe (not under
`NO_COLOR`). Tables become tab-separated lines through a pipe.

## JSON, jq and templates

Most commands take the `gh` trio:

```sh
ytrack issue view APP-123 --json summary,state,comments
ytrack issue list -q '#Unresolved' --json idReadable,summary --jq '.[].idReadable'
ytrack issue list --project APP --jq '.[] | select(.priority == "Critical") | .idReadable'
ytrack work-item list APP-123 --template '{{range .}}{{.date}} {{.duration.presentation}}{{"\n"}}{{end}}'
```

- `--json <fields>` prints only those fields; field names are listed in each
  command's `--help`, and `--json` with no fields lists them too.
- `--jq <expr>` filters the JSON with a built-in jq (no `jq` install
  needed); `--template` formats it with a Go template. Without `--json`,
  both use every field.
- In `issue list`, `-q` is `--query`; elsewhere `-q` is `--jq`.
- `ytrack skill install --json` and `ytrack skill list --json` take no field
  list; `issue download` has no JSON output.

## Usage errors

A mistyped or guessed flag fails with exit 1 and a hint:

```text
$ ytrack issue list --limt 5
ytrack: unknown flag: --limt
did you mean --limit?
Run 'ytrack issue list --help' for usage.

$ ytrack issue view APP-1 --comments 12
ytrack: expected one issue ID or URL, e.g. APP-123
--comments takes no value; for a number use --comments-limit 12
```

An unknown flag suggests close ones, also a flag of the same idea under
another name (`--limit` on `issue view` suggests `--comments-limit`). Help
shows bool flags as `--flag[=true|false]`, and a bad typed value says what is
expected (`must be an integer`).

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | failure, including usage errors |
| 2 | cancelled (a declined or interrupted prompt) |
| 4 | authentication required: no host, no token, or a rejected token |

## Environment

| Variable | Meaning |
| --- | --- |
| `YTRACK_HOST` | host or service URL to use (see [auth.md](auth.md#which-host-a-command-uses)) |
| `YTRACK_TOKEN` | token to use instead of the stored one; never saved |
| `YTRACK_DEBUG=1` | log HTTP requests to stderr with credentials redacted (same as `--debug`) |
| `YTRACK_CONFIG_DIR` | configuration directory |
| `YTRACK_CACHE_DIR` | cache directory (the project cache) |
| `YTRACK_BROWSER` | command that opens URLs (then `browser` in `config.yml`, `BROWSER`, the system's) |
| `YTRACK_EDITOR` | editor for descriptions and comments (then `GIT_EDITOR`, `VISUAL`, `EDITOR`) |
| `YTRACK_NO_UPDATE_NOTIFIER=1` | no release check, notice or background upgrade |
| `YTRACK_NO_AUTO_UPGRADE=1` | no background upgrade (the notice stays) |
| `NO_COLOR`, `CLICOLOR`, `CLICOLOR_FORCE` | color, as above |

In CI (`CI`, `BUILD_NUMBER` or `RUN_ID` set) ytrack never checks for or
installs releases.

## Files

| What | Where |
| --- | --- |
| `config.yml`: `browser`, `timezone`, `check_update`, `auto_upgrade` | config directory: `$YTRACK_CONFIG_DIR`, else `$XDG_CONFIG_HOME/ytrack`, else `%AppData%\ytrack` on Windows, else `~/.config/ytrack` |
| `hosts.yml`: hosts, their `paths`, the default host (never tokens) | config directory |
| `credentials.yml`: tokens, only with `auth login --insecure-storage` | config directory |
| `state.yml` (release check, background upgrade), `skills.yml` (installed skill copies), `auto-upgrade.log`, `upgrade.lock` | state directory: `$XDG_STATE_HOME/ytrack`, else `%LOCALAPPDATA%\ytrack` on Windows, else `~/.local/state/ytrack` |
| `projects.json` (which host owns a project, 7 days) | cache directory: `$YTRACK_CACHE_DIR`, else `$XDG_CACHE_HOME/ytrack`, else `~/.cache/ytrack`, `~/Library/Caches/ytrack` on macOS, `%LocalAppData%\ytrack` on Windows |

A `config.yml` with every key:

```yaml
browser: firefox            # command that opens URLs
timezone: Europe/Madrid     # IANA zone for work item dates; default: the system's
check_update: true          # false: no release check, notice or background upgrade
auto_upgrade: true          # false: no background upgrade
```

## Shell completion

`install.sh` installs completion for zsh, bash or fish (from `$SHELL`) and
prints how to enable it; `install.ps1` prints the PowerShell profile line.
Otherwise run `ytrack completion bash|zsh|fish|powershell`; see
[install.md](install.md#shell-completions). Completion suggests commands,
flags, `--json` fields, issue IDs (with their summaries) and `--project`
names.
