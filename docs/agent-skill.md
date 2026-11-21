# Agent skill

No coding agent was trained on ytrack, so agents guess `gh` habits
(`gh auth status --hostname` instead of `--host`, `gh issue create --title`
instead of `--summary`) or scrape the YouTrack web page. The `ytrack`
[agent skill](../skills/ytrack/SKILL.md) teaches them the real commands:
hosts and auth, issue queries, create, edit and comment, attachments, links,
YouTrack commands (`--dry-run` first, locale-proof field names), work items,
`--json`/`--jq`, `ytrack api` as the escape hatch, and what to tell you on
exit code 4. It leads with "never guess a flag: run `--help` first" and never
has an agent handle your token. Rare detail lives in `references/`, which the
agent reads only when it needs it.

## Install

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
ytrack skill sync --force                            # also rewrite copies a newer ytrack wrote
ytrack skill uninstall --dir ~/.cursor/skills        # remove a copy and forget it
ytrack skill print                                   # SKILL.md on stdout
```

Without `--dir`, `ytrack skill install` writes `~/.agents/skills`, which
Codex, Cursor, OpenCode and Pi read, and `~/.claude/skills` for Claude Code
(with `--json`, one object per line). It writes `<dir>/ytrack/SKILL.md` and
`references/`, replacing an earlier ytrack skill there by renaming the new
copy into place; it refuses a `ytrack` directory that holds anything else and
leaves it untouched. A symlink there, as `npx skills add` makes, is followed.

| Agent | Skills directory |
| --- | --- |
| Claude Code | `~/.claude/skills` (or `.claude/skills` in a project) |
| Codex | `~/.agents/skills` (older Codex: `~/.codex/skills`) |
| Cursor | `~/.agents/skills` or `~/.cursor/skills` |
| OpenCode | `~/.agents/skills` or `~/.config/opencode/skills` |
| Pi | `~/.agents/skills` or `~/.pi/agent/skills` |

[Workit](https://github.com/BrainerVirus/workit) installs and updates the
skill for the agents it sets up, so with Workit you need neither command.

## How copies stay current

ytrack remembers every directory it installed the skill into (in
`skills.yml` in the state directory), and on its first run also records
copies already in the agents' home skills directories. The first command a
newer ytrack release runs rewrites the copies in your home directory, so
after an upgrade no agent reads an older skill. The refresh is silent and
never changes a command's output or exit code. All `skill` commands work
offline and logged out.

`ytrack skill list` prints each copy's directory, version, state and how it
is updated:

| State | Meaning |
| --- | --- |
| `current` | this ytrack's version |
| `stale` | an older version (or a home copy ytrack could not rewrite) |
| `newer` | written by a newer ytrack; this ytrack leaves it alone |
| `missing` | deleted, or replaced by something that is not the ytrack skill |

A copy in your home directory updates `auto`. A copy in a repository (a
`--dir` inside a git work tree) is `manual`: it changes only when you run
`ytrack skill install --dir` there again, and `skill list` prints that command
when it is stale.

`ytrack skill sync` prints one line per recorded copy: `updated`, `kept` (a
`newer` copy, which it does not downgrade), `dropped` (a missing copy, which
it forgets rather than re-create), `manual` or `failed` (exit 1). An older
ytrack left on your `PATH` therefore never rewrites a newer copy;
`--force` rewrites such copies at this ytrack's version anyway.

## How it is kept honest

A test checks every `ytrack` command and flag the skill names against the
command tree, so the skill cannot drift from the binary; the same test covers
this repository's README and `docs/`. A local bench runs the skill on small
models against a fake ytrack and grades what they do; see
[bench/README.md](../bench/README.md).
