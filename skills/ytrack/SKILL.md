---
name: ytrack
description: Work with JetBrains YouTrack through the ytrack CLI - read, search, count, create, edit and delete issues, read issue history, watchers and votes, vote and star issues, list saved searches, add, list, edit and delete comments, apply YouTrack commands, list and create tags, list a project's custom fields, log, edit and report spent time (work items, time tracking), and call the YouTrack REST API. Use for any YouTrack issue ID (APP-123), a youtrack.cloud or YouTrack Server URL, YouTrack queries, or YouTrack time tracking, instead of scraping the web UI or calling the API with curl.
metadata:
  version: "dev"
---

# ytrack

**Never guess a flag: if this page does not show the flag or value you need
(a count, a limit, a filter), run `ytrack <command> --help` first.**

`ytrack` is a YouTrack CLI on your PATH: run the commands below yourself with
your shell tool (this skill only explains them). It is modelled on `gh`, but
its flags and nouns differ. Never scrape the YouTrack web page or call the
REST API with curl: use the commands below, and `ytrack api` for anything
they do not cover.

## Hosts, auth and exit codes

- Pick a host with the global `--host` flag (`gh auth status --hostname`
  habits fail). `ytrack auth status` lists the logged-in hosts and says which
  one is active and why.
- An issue URL selects its own host; when `--host` or `YTRACK_HOST` names
  another, it exits 1 ("the issue URL is on X but --host is Y"): drop
  `--host` and unset `YTRACK_HOST`, or pass the URL's host.
- Exit 0 ok, 1 failure (stderr names the bad flag or value and what to run),
  2 cancelled, 4 not logged in or token rejected: stop and tell the user to
  run `ytrack auth login --host <host>` themselves. Do not retry with another
  host and do not fall back to the web page.
- Tokens are the user's: never ask for one, never put one in a command line
  or an environment variable, never run `ytrack auth token`.
- Several hosts, or a key not found:
  [references/hosts.md](references/hosts.md).

## Read issues

```sh
ytrack issue view APP-123 --json summary,state,assignee,customFields,comments
ytrack issue view https://acme.youtrack.cloud/issue/APP-123 --comments   # latest 10; for more, see --help
ytrack issue list -q 'project: APP Assignee: me #Unresolved'
ytrack issue list --project APP --assignee me --state 'In Progress' --sort 'updated desc'
ytrack issue list --project APP --all --limit 0 --json idReadable,summary,state
ytrack issue count 'project: APP #Unresolved'          # a number, no listing
ytrack issue activities APP-123 --category fields,comments --json timestamp,author,field,added
ytrack issue watchers APP-123 --json login
ytrack query list --json name,query                    # saved searches
```

- `--json <fields>` prints only those fields (`--help` lists them); ask for
  the ones you need. `--jq <expr>` filters it; spell `--jq` out, since in
  `issue list` `-q` is `--query`.
- `-q` takes YouTrack search syntax, sent as is:
  [references/queries.md](references/queries.md). Without `-q` or `--state`
  only unresolved issues are listed; `--all` adds resolved ones. `--limit`
  defaults to 30, `--limit 0` is all.
- Query keywords such as `for:` are localized; `Assignee: me` (or
  `--assignee me`), `project:` and `#Unresolved` work on every locale.
  Unresolved is `#Unresolved`, never `State: -Resolved`.
- Unsure of a field name or value? `ytrack issue list -q 'State: ' --query-assist`
  prints YouTrack's completions instead of issues.
- `ytrack issue vote APP-123` / `issue unvote` change your vote and
  `issue watch` / `issue unwatch` your star; ask first. Exit 1 means nothing
  changed (see the error).

## Create, edit, comment, attach

```sh
ytrack issue create -p APP -s 'Crash on start' --description-file notes.md --field Type=Bug --assignee me --tag backend
ytrack issue edit APP-123 --field 'Priority=Critical' --field 'State=In Progress' --add-tag backend
ytrack issue edit APP-123 --field 'Estimation=2d'     # period field: a duration such as 1w 2d 4h
ytrack issue edit APP-123 --assignee ''               # unassign
ytrack issue comment APP-123 --body 'Reproduced on 2026.2'
ytrack issue comment APP-123 --body-file reply.md     # long text
ytrack issue comment list APP-123 --json id,author,text   # comment IDs
ytrack issue comment edit APP-123 4-17 --body 'Reproduced on 2026.3'
ytrack issue comment delete APP-123 4-17 --yes
ytrack issue delete APP-123 --yes
cmd | ytrack issue create -p APP -s 'Title' --description-file -   # - reads stdin (also --body-file -)
ytrack issue attach APP-123 dump.sql repro.sh
ytrack issue attachments APP-123                    # files on the issue and its comments
```

- An issue has a `--summary` and a `--description`; a comment has a `--body`.
  Without a terminal, `issue create` needs `--project` and `--summary`.
  Never pass `--editor` (it waits for a human).
- `--field 'Name=Value'` sets a custom field by its project field name
  (`ytrack project fields APP` lists names, types, defaults and allowed
  values). `issue edit` exits 1 naming a field YouTrack did not change; then
  use `issue command`.
- `--tag` (create) and `--add-tag`/`--remove-tag` (edit) take existing tags
  (`ytrack tag list`); `ytrack tag create <name>` adds a missing one.
- Once a comment is posted, never run the command again, even after a
  warning or exit 1: a rerun posts a duplicate. Fix it with `comment edit`.
- `issue delete` and `comment delete` cannot be undone: confirm with the
  user first. Without a terminal they need `--yes`.
- Files go up with `issue attach` (issue) or `issue comment --attach`
  (comment) and come down with `issue download`, never through `ytrack api`
  or curl: [references/attachments.md](references/attachments.md).

## YouTrack commands

```sh
ytrack issue command APP-123 'State Fixed' --dry-run    # preview, changes nothing
ytrack issue command APP-123 'State Fi' --assist        # preview plus completions
ytrack issue command APP-123 APP-124 'State Fixed' --comment 'Fixed in 2026.3'
```

- Prefer `issue edit` for fields, tags and the assignee, and `issue link` for
  links. Use a command for several issues at once or what edit cannot do,
  always with `--dry-run` first. Unsure of a value (a state that starts
  with "Ver")? Preview with `--assist` and apply a full command it prints
  after `>`; never apply a guess.
- Command keywords are localized (a Spanish instance rejects `for me`);
  write `<Field name> <value>`, such as `Assignee me` or `State Fixed`.
  More: [references/commands.md](references/commands.md).

## Links between issues

```sh
ytrack issue link --list-types                          # each type's two phrases
ytrack issue link APP-1 APP-2 --type 'depends on'       # APP-1 depends on APP-2
ytrack issue link APP-1 APP-5 APP-6 --type 'parent for'
ytrack issue link APP-1 --list --json phrase,issues
ytrack issue unlink APP-1 APP-2 --type 'depends on'
```

- Read "APP-1 <phrase> APP-2": `--type` is a phrase `--list-types` prints, and
  it sets the direction. A directed type's name (`Depend`) is refused.
- Never link with `issue command`: link words in commands are localized, the
  phrases are not.

## Time tracking (work items)

```sh
ytrack work-item add APP-123 --duration 1h30m --text 'Code review'
ytrack work-item add APP-123 --duration 45m --date 2026-10-06 --type Development
ytrack work-item add APP-123 --duration '1h 30m' --meeting      # meeting time
ytrack work-item list APP-123 --author me --since 2026-10-01 --json id,date,duration,text
ytrack work-item report --since 2026-10-01 --until 2026-10-31 --json minutes,issues,days   # across issues
ytrack work-item edit APP-123 115-3 --duration 2h
ytrack work-item delete APP-123 115-3 --yes
```

- `--duration` takes `1h30m`, `90m`, `1.5h` or bare minutes; days and weeks
  are refused. `--date` is today unless you pass `YYYY-MM-DD`.
- `edit` and `delete` take the work item ID from
  `ytrack work-item list APP-123 --json id`; confirm with the user before
  deleting.

## REST API escape hatch

```sh
ytrack api /users/me --fields login,fullName
ytrack api /issues -X GET -f query='project: APP #Unresolved' --fields idReadable,summary --paginate
```

`ytrack api` sends the stored token for you; YouTrack returns only the
attributes named in `--fields`. Any `-f`, `-F` or `--input` makes a POST (a
POST to `/issues` creates an issue) unless you pass `-X GET`, so every read
with parameters needs `-X GET`. More: [references/api.md](references/api.md).

## gh habits that are wrong here

- `gh issue list --search` → `ytrack issue list -q '<YouTrack query>'`
- `gh issue list --repo` → `ytrack issue list --project APP`
- `gh issue create --title --body` → `ytrack issue create --summary --description`
- `gh issue close` → `ytrack issue command APP-123 'State Fixed'`
- `gh issue edit --add-label` → `ytrack issue edit APP-123 --add-tag <name>`
- Issue IDs are `APP-123`, never `#123`; `#` starts a query tag such as `#Unresolved`.
- `--state open|closed` does not exist: use `#Unresolved` or `--all`.

## This skill's version

If `ytrack version` differs from `metadata.version` above and a command
fails, run `ytrack skill list`; update a `manual` copy with
`ytrack skill install --dir <dir>` only when the user asks.
