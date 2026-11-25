# Skill bench

The agent skill in `skills/ytrack/` has to work on small models while costing
as few tokens as possible. The bench measures both on real models: each case
gives Claude Code a YouTrack task in a throwaway project with the skill
installed, and grades what it did. It needs a Claude Code login, spends
subscription usage and never runs in CI (`make test` only runs its unit
tests, which grade canned calls and never start a model).

## Run it

```sh
go run ./bench --model claude-haiku-4-5 --repeat 3
go run ./bench --model claude-haiku-5-5 --repeat 3 --case comment,link-es
go run ./bench compare bench/results/baseline/claude-haiku-4-5 bench/results/claude-haiku-4-5
go run ./bench --model claude-haiku-4-5 --no-skill      # control: the same worlds without the skill
```

- `--model` takes any Claude Code model id or alias; results go to
  `bench/results/<model>/`: one `<case>.json` with every run, and
  `summary.json` with the totals.
- `--repeat n` runs each case n times, each in a fresh world. A case passes
  when at least two thirds of its graded runs pass (2 of 3).
- `--budget-runs n` (default 40) caps the `claude -p` runs one invocation
  starts; `--jobs n` (default 3) runs that many at once. A run is capped at
  $0.50 (`--max-budget-usd`).
- Stored runs are reused when their key matches: the case file, the skill's
  files, the model and the bench version. An edit to the skill re-runs every
  case; `--force` re-runs anyway.
- Exit 1 when a case fails, a run hits a harness error or runs were left over
  the budget.
- `--timeout` (default 6m) limits each run; `--keep` keeps each world under
  the temp dir for inspection. `--cases`, `--skill`, `--out` and `--claude`
  point at another cases dir, skill dir, results dir or `claude` binary.
- `--no-skill` runs the same worlds without `.claude/skills/ytrack/`, into
  `bench/results/no-skill/<model>/`: the control that shows what the skill
  adds (`compare` it against a with-skill dir).
- With the skill, a run whose model never loaded it (no Skill call and no read
  of `SKILL.md`) is an `error` counted under `notLoaded`, not graded: it says
  nothing about the skill's text.
- `regrade <dir>` grades the stored runs of a results dir again with the
  current cases, without a model (after fixing a rule).
- `compare <before> <after>` prints each case's passes and mean input tokens
  per run side by side, the input per model turn (the context size apart from
  how many turns a run took), and exits 1 when a case's pass rate dropped. Keep the
  results of the skill before a change in `bench/results/baseline/<model>/`
  to compare against.

Scrubbed transcripts of each run land in `bench/.transcripts/<model>/`
(ignored by git) for debugging a failure.

## What a run does

Each run gets a temp dir with two parts:

- `project/`, the model's working directory, with the skill copied to
  `.claude/skills/ytrack/` (where Claude Code loads project skills) and the
  case's `files`.
- `harness/`, outside the project: `bin/ytrack` (the fake), `case.json` (its
  fixtures) and `calls.jsonl` (its log).

Then, in `project/`, with the prompt on stdin:

```sh
claude -p --model <m> --output-format stream-json --verbose --max-turns <n> \
  --setting-sources project --strict-mcp-config --no-session-persistence \
  --permission-mode dontAsk --max-budget-usd 0.50 \
  --allowedTools Skill 'Read(./**)' 'Bash(ytrack *)' 'Bash(ls *)' 'Bash(jq *)' 'Bash(wc *)' 'Bash(echo *)' 'Bash(pwd)' \
  --disallowedTools WebFetch WebSearch Task Agent
```

`--setting-sources project` loads only the world's own settings: no user
settings, user skills or user plugins, so a host plugin's hooks (Workit asks
before raw `ytrack` writes, which a headless run turns into a denial) do not
gate the run, and the bench measures the skill rather than the host. A plain
`ytrack` command that is still denied came from a host policy (a managed
setting): that run is recorded as `blocked` and not graded. Anything else
outside the allow list is denied by `dontAsk` and listed under `denials`.

The child environment drops `WORKIT_*`, `YTRACK_*`, `CLAUDECODE`,
`ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN`, puts `harness/bin` first on
`PATH`, and points `YTRACK_CONFIG_DIR` and `YTRACK_CACHE_DIR` into the
harness, so even a real `ytrack` reached by mistake has no host and no token.

### The fake ytrack

`harness/bin/ytrack` is the bench binary itself (it acts as ytrack when run
under that name). It parses argv with ytrack's real command tree, so an
unknown command, flag or `--json` field fails with ytrack's own message and
exit 1, and `--help` prints ytrack's real help. `--jq` filters the reply
with the jq ytrack itself uses (`--jq` alone gives every field). It never runs a command: a
parsed call is answered from the case's `fixtures.rules` (regex on the argv
line, first match wins), then from built-in replies (issues and lists from
`fixtures.issues` and `fixtures.list`, a success line for writes, the
`ytrack api` refusal of read parameters without `-X GET`, link phrases, the
work item duration rules, comment list and delete, tag list and create). Its HTTP transport fails every request, and it
reads no config. Each call is appended to `calls.jsonl` with its exit code.

A run where the model ran `ytrack` but the fake logged nothing is a harness
error (a real `ytrack` was first on `PATH`).

## Cases

`bench/cases/<id>.json`:

| Field | Meaning |
| --- | --- |
| `prompt` | the user message |
| `maxTurns` | `--max-turns` (default 10) |
| `files` | files written into the project |
| `fixtures` | `issues` (by ID), `list` (what `issue list` returns), `tags` (the tags that exist: `--tag` and `--add-tag` refuse others until `tag create`), `rules` (`match`, `stdout`, `stderr`, `exit`) |
| `require` | rules that must hold |
| `forbid` | rules that must not hold |
| `answer` | regex the final message must match (case-insensitive) |

A rule matches lines of one source: `calls` (default; each fake ytrack call
as argv words joined by spaces, `--flag=value` written `--flag value`, no
shell quoting) or `commands` (each Bash command as the model wrote it). A
line matches when every `match` regex and no `not` regex matches. A
`require` rule needs one matching line, and for calls one that exited 0;
with `before`, its first match must come before the first other line
matching every `before` regex (help read before the first guess). With
`beforeFailed`, only failed calls count as that line (a guessed flag), and the
rule holds when no call failed. A `forbid` rule breaks on
`times` matching lines (default 1; 2 catches a repeated write).

| Case | What it checks |
| --- | --- |
| `view-json` | `issue view --json` with only the needed fields |
| `search-query` | `issue list -q` with a tag and project query |
| `my-issues-es` | `--assignee me` on a Spanish instance, never the localized `for: me` |
| `comment` | `issue comment --body`, posted once |
| `log-time` | `work-item add --duration 1h30m`, once |
| `set-field-es` | localized trap: `issue edit --field`, not `issue command` |
| `link-es` | localized trap: `issue link --type 'subtask of'`, not `issue command` |
| `attach` | `issue attach` with a local file |
| `api-read` | `ytrack api` read with `-X GET`, a query and `--fields` |
| `help-first` | 12 comments in a text view: `issue view --comments-limit 12` or `issue comment list --limit 12`, with `--help` before any guessed flag |
| `meeting-time` | `work-item add --meeting` with a `1h 30m` duration, today |
| `create-stdin` | `issue create --description-file -` with the description piped in |
| `comment-delete` | the comment the user names: its ID from `issue comment list`, then `issue comment delete --yes`; never `issue delete` |
| `time-report` | my time this week: one `work-item report --since`, with the total in the answer |
| `count-open` | `issue count` with a `project:` and `#Unresolved` query, the number in the answer |
| `create-field` | a value the prompt does not name: `project fields` before `issue create --field Severity=Show-stopper`, created once |
| `tag-create` | a missing tag: `tag create`, then `issue edit --add-tag`; never a tag command |
| `command-assist` | a half-named state: the value from `--assist`, `--dry-run`, `project fields` or `--query-assist` before one `issue command` on both issues, never a guess |

A new case gets a row in `TestGivenEachCaseWhenGradedItPassesTheTaughtCallsAndFailsTheTrap`
(`bench/bench_test.go`): the calls the skill teaches must pass and the trap
must fail, so a broken regex fails in `make test`, not in a model run.

## What a result holds

Each run records its status (`pass`, `fail`, `blocked`, `error`), the first
failed rule and all of them, turns, tokens (`input`, `output`,
`cacheCreation`, `cacheRead`; "total input" is their prompt-side sum),
`costUSD` (Claude Code's `total_cost_usd`), duration, whether the ytrack
skill was listed at start and loaded by the model, the Bash commands, the
fake's calls, denials and the final answer. `summary.json` has per-case
passes, mean turns, mean and standard deviation of total input, mean output
and duration, and the totals. A case whose stored runs were made with an
earlier skill or case file (a partial re-run with `--case`) is still summed
but marked `stale`, and `compare` marks it "(earlier skill)".

Before writing, the bench replaces the world's path with `<world>`, the home
directory with `~` and the login name with `<user>`, masks anything shaped
like a YouTrack token (`perm:`), an Anthropic key or a bearer header as
`<token>`, and refuses to write a file that still holds a home path or a
token.
