---
name: ytrack
description: Work with JetBrains YouTrack through the ytrack CLI - read, search, create and edit issues, add comments, apply YouTrack commands, log and edit spent time (work items, time tracking), and call the YouTrack REST API. Use for any YouTrack issue ID (APP-123), a youtrack.cloud or YouTrack Server URL, YouTrack queries, or YouTrack time tracking, instead of scraping the web UI or calling the API with curl.
metadata:
  version: "dev"
---

# ytrack

`ytrack` is a YouTrack CLI modelled on `gh`, but it is not `gh`: its flags and
nouns differ. Check `ytrack <command> --help` before guessing a flag. Never
scrape the YouTrack web page and never call the REST API with curl; use the
commands below, and `ytrack api` for anything they do not cover.

## Hosts and auth

```sh
ytrack auth status                               # logged-in hosts; the active one is marked
ytrack auth status --json host,url,active,login,state
ytrack auth switch --host acme.youtrack.cloud    # change the default host
```

- Pick the host with the global `--host` flag on any command. There is no
  hostname flag: `gh auth status --hostname` habits fail here.
- A command uses `--host`, else `YTRACK_HOST`, else the default host from
  `ytrack auth switch`. With more than one logged-in host, run
  `ytrack auth status` first and pass `--host` so you never read the wrong
  instance.
- An issue URL selects its own host, but when `--host` or `YTRACK_HOST`
  names another host the command fails with exit 1 ("the issue URL is on X
  but --host is Y"). For a URL from another host, drop `--host` and unset
  `YTRACK_HOST`, or pass the URL's host as `--host`.
- Tokens are the user's. Never ask for one, never put one in a command line
  or an environment variable, never run `ytrack auth token`. If no host is
  logged in, ask the user to run `ytrack auth login` themselves.

## Exit codes

0 ok, 1 failure (including usage errors), 2 cancelled, 4 authentication
required. On exit 4, stop and tell the user that ytrack is not logged in to
that host or its token was rejected, and that they should run
`ytrack auth login --host <host>` themselves. Do not retry with another host
and do not fall back to the web page. On exit 1, read stderr: it names the
bad flag or value and the command to run. `ytrack auth status` itself exits
4 when any logged-in host has no token or a rejected one, even if the active
host works.

## Read issues

```sh
ytrack issue view APP-123
ytrack issue view https://acme.youtrack.cloud/issue/APP-123 --comments
ytrack issue view APP-123 --json summary,state,assignee,customFields,comments
ytrack issue list -q 'project: APP for: me #Unresolved'
ytrack issue list --project APP --assignee me --state 'In Progress' --sort 'updated desc'
ytrack issue list --project APP --all --limit 0 --json idReadable,summary,state
```

- `-q`/`--query` takes YouTrack search syntax, sent as is. Without
  `--query` or `--state` only unresolved issues are listed; `--all` adds
  resolved ones. `--limit` defaults to 30, `--limit 0` is all.
- Query syntax: [references/queries.md](references/queries.md).

## Create, edit, comment

```sh
ytrack issue create --project APP --summary 'Login fails on Safari' --description-file notes.md
ytrack issue create -p APP -s 'Crash on start' --field Type=Bug --field Priority=Critical --assignee me --tag backend
ytrack issue edit APP-123 --field 'Estimation=2d'     # period field: a duration such as 1w 2d 4h
ytrack issue edit APP-123 --summary 'New title' --field 'State=In Progress' --add-tag backend
ytrack issue edit APP-123 --assignee ''             # unassign
ytrack issue comment APP-123 --body 'Reproduced on 2026.2'
ytrack issue comment APP-123 --body-file reply.md   # long or multi-line text
```

- An issue has a `--summary` and a `--description`; a comment has a `--body`.
- `--field 'Name=Value'` sets a custom field by its project field name;
  `ytrack issue view APP-123 --json customFields` lists the names and values.
  Period fields (Estimation, Spent time) take a duration like `1w 2d 4h`.
- `--tag` (create) and `--add-tag`/`--remove-tag` (edit) take existing tags.
- `--body-file -` (comment) and `--description-file -` (create, edit) read
  standard input, e.g. text you pipe in.
- Without a terminal, `issue create` needs `--project` and `--summary`. Never
  pass `--editor` (it waits for a human).

## YouTrack commands

```sh
ytrack issue command APP-123 'State Fixed' --dry-run    # preview, changes nothing
ytrack issue command APP-123 'State Fixed' --comment 'Fixed in 2026.3'
ytrack issue command APP-123 APP-124 'Assignee me' --silent
```

- Always `--dry-run` first: it shows how YouTrack parses each part and exits
  1 when a part is not understood.
- Command keywords are localized on non-English instances (a Spanish
  instance rejects `for me`); write `<Field name> <value>`, such as
  `Assignee me` or `State Fixed`, which works on every locale.
- Prefer `ytrack issue edit` for plain field, tag and assignee changes.
  More: [references/commands.md](references/commands.md).

## Time tracking (work items)

```sh
ytrack work-item list APP-123 --author me --since 2026-10-01 --json id,date,duration,text
ytrack work-item add APP-123 --duration 1h30m --text 'Code review'
ytrack work-item add APP-123 --duration 45m --date 2026-10-06 --type Development
ytrack work-item add APP-123 --duration 30m --meeting
ytrack work-item edit APP-123 115-3 --duration 2h
ytrack work-item delete APP-123 115-3 --yes
```

- `--duration` takes `1h30m`, `1h 30m`, `90m`, `1.5h`, `2 hours 15 minutes`
  or bare minutes (`90`). Days and weeks are refused.
- `--date` is `auto` (today, the default) or `YYYY-MM-DD`.
- `edit` and `delete` take the work item ID (`115-3`) from
  `ytrack work-item list APP-123 --json id`. Without a terminal, `delete`
  needs `--yes`; confirm with the user before deleting.

## JSON output

- `--json <fields>` prints JSON with those fields; `--help` lists them.
  `--jq <expr>` filters it and needs `--json`.
- Spell out `--jq`: in `ytrack issue list`, `-q` is `--query`.

```sh
ytrack issue list -q 'project: APP #Unresolved' --json idReadable,summary --jq '.[].idReadable'
ytrack work-item list APP-123 --json duration --jq 'map(.duration.minutes) | add'
```

## REST API escape hatch

```sh
ytrack api /users/me --fields login,fullName
ytrack api /issues -X GET -f query='project: APP #Unresolved' --fields idReadable,summary --paginate
ytrack api /issues/APP-123/links --fields 'linkType(name),direction,issues(idReadable)'
```

`ytrack api` sends the stored token for you. YouTrack returns only the
attributes named in `--fields`. Any `-f`, `-F` or `--input` makes the
request a POST with a JSON body unless you pass `-X GET`; a POST to `/issues`
creates an issue. Every read with parameters needs `-X GET` (`--paginate`
refuses POST). Details: [references/api.md](references/api.md).

## gh habits that are wrong here

- `gh issue list --search` → `ytrack issue list -q '<YouTrack query>'`
- `gh issue create --title --body` → `ytrack issue create --summary --description`
- `gh issue close` → `ytrack issue command APP-123 'State Fixed'`
- `gh issue edit --add-label` → `ytrack issue edit APP-123 --add-tag <name>`
- `gh issue list --repo` → `ytrack issue list --project APP`
- `gh auth status --hostname` → `ytrack auth status --host <host>`
- Issue IDs are `APP-123`, never `#123`; `#` starts a query tag such as `#Unresolved`.
- `--state open|closed` does not exist: states are project values (`Open`,
  `In Progress`, `Fixed`); use `#Unresolved` or `--all`.
