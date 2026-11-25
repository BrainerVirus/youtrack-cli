# Issues

Every command takes an issue as its ID (`APP-123`) or its web URL. A URL may
be `/issue/APP-123`, with or without the title slug, or
`/projects/APP/issues/APP-123`; it selects its host (see
[auth.md](auth.md#which-host-a-command-uses)).

## List and view

```sh
ytrack issue list -q 'project: APP Assignee: me #Unresolved'   # YouTrack's own query syntax
ytrack issue list --project APP --assignee me --state Open --sort 'updated desc'
ytrack issue list --project APP --all --limit 0                  # resolved too, every match
ytrack issue list -q '#Unresolved' --json idReadable,summary,state,url
ytrack issue view APP-123 --comments                             # with the latest 10 comments
ytrack issue view APP-123 --comments --comments-limit 25
ytrack issue view https://acme.youtrack.cloud/issue/APP-123 --web
```

On a terminal, `issue list` prints a table; through a pipe, tab-separated
lines with RFC 3339 times:

```text
ID       SUMMARY                STATE        PRIORITY  ASSIGNEE  UPDATED
APP-124  Login fails on Safari  In Progress  Critical  jdoe      about 2 hours ago
APP-118  Export to CSV          Open         Normal              about 3 days ago
```

- `-q`/`--query` is sent unchanged. `--project`, `--assignee`, `--state`
  and `--sort` add `project: APP`, `Assignee: <login>`,
  `State: {In Progress}` and `sort by: updated desc`. These are field names,
  which work on every instance language, unlike keywords such as `for:`.
  `--assignee me` sends your own login; when YouTrack rejects the search,
  the error asks whether the `--assignee` value is a login.
- Without `--query` or `--state`, only unresolved issues are listed
  (`#Unresolved` is added); `--all` includes resolved ones.
- `--limit` defaults to 30; `--limit 0` lists every match.
- In `issue list`, `-q` is `--query`; spell out `--jq` for jq. With
  `--json`, a `-q` that looks like a jq expression is refused with that hint.
- `--json` with no fields lists the available ones. Only the attributes the
  table or the requested fields need are fetched.

## Create and edit

```sh
ytrack issue create --project APP --summary 'Login fails on Safari'
ytrack issue create -p APP -s 'Crash on start' --field Priority=Critical --field Type=Bug --assignee me
ytrack issue create -p APP -s 'Spike' --description-file notes.md --tag backend --json idReadable,url
ytrack issue edit APP-123 --summary 'Login fails on Safari 18' --field 'Priority=Critical'
ytrack issue edit APP-123 --field 'Fix versions=2026.3,2026.4' --add-tag backend --remove-tag triage
ytrack issue edit APP-123 --field 'Estimation=2d'
ytrack issue edit APP-123 --assignee ''      # unassign
```

`issue create` asks for a missing project or summary on a terminal;
otherwise `--project` and `--summary` are required. The description comes
from `--description`, `--description-file` (`-` for standard input) or
`--editor`.

`--field 'Name=Value'` (repeatable) sets a custom field by its project field
name. The value is checked against the project's field definitions:

| Field kind | Value |
| --- | --- |
| enum, state, version, build, owned, group | an element name (case is ignored) |
| user | a login or `me` |
| date | `YYYY-MM-DD` |
| date-time | `2026-10-07T14:30` (local time) or RFC 3339 |
| number | a number |
| text, string | any text |
| period, such as Estimation | a duration such as `1w 2d 4h`, set with a YouTrack command so the work schedule applies |

A multi-value field takes a comma-separated list, which replaces its values;
to add or remove one value, use `issue command` with `add` or `remove`.
`Name=` clears a field unless the project requires one. A version, build,
enum or owned field whose value set is still empty is refused before
anything is sent, naming the field. Without access to the project settings,
the definitions come from the issue (edit) or from an issue in the project
(create). `issue edit` sends only what you pass, reads the issue back, and
exits 1 naming any field YouTrack did not change.

`--tag` (create) and `--add-tag`/`--remove-tag` (edit) take existing tag
names.

## Comment

```sh
ytrack issue comment APP-123 --body 'Reproduced on 2026.2'
git log -1 --format=%B | ytrack issue comment APP-123 --body-file -
ytrack issue comment APP-123 --body 'Fixed: ![](after.png)' --attach after.png --attach report.html
```

With no `--body`, `--body-file` or `--editor`, the editor opens on a
terminal. The editor is `$YTRACK_EDITOR`, `$GIT_EDITOR`, `$VISUAL` or
`$EDITOR`.

`--attach` (repeatable) posts the comment, then uploads the files to it. The
text shows a file inline when it names it, as `![](after.png)` or
`[report](report.html)` (a path you gave, such as `shots/after.png`, is
rewritten to the name); the others are listed under the comment. If a file
ends up on the issue instead, the command warns and exits 0 (the comment
still shows a file it names); it exits 1 naming each file that is neither on
the comment nor shown by it. After either, do not run it again, since the
comment and the uploads exist: check with `issue attachments` and add a
missing file with `issue attach`. `--json attachments` reports each file's
`state`: `comment`, `issue` or `failed`. A missing file stops the command
before anything is posted.

## Attachments

```sh
ytrack issue attach APP-123 dump.sql repro.sh     # prints name<TAB>id per file
ytrack issue attachments APP-123                  # name, id, size, type, created, author, comment
ytrack issue download APP-123 -o out/             # prints name<TAB>bytes per file
ytrack issue download APP-123 trace.log 8-123 --clobber
ytrack issue download APP-123 --comment 4-17
```

`issue attach` attaches files to the issue itself in one request, streaming
each regular file from disk, so a large file is not held in memory; a file
that changes size during the upload fails it. A pipe such as `<(cmd)` is read
into memory first. A missing file, a directory or a device stops it before
anything is sent.

`issue attachments` lists the files on the issue and on its comments
(`--json name,id,size,mimeType,created,author,commentId`). `issue download`
saves all of them, or those named by name or attachment ID, or with
`--comment <id>` the files of one comment, into `-o`/`--dir` (created when
missing; the current folder by default), using the same host as `issue view`.
It never overwrites a file unless `--clobber` is given, saves a name holding
a path such as `../x` as `.._x`, saves a name Windows cannot hold under a safe
one (`NUL.txt` as `_NUL.txt`, `a. ` as `a__`, `a?b` as `a_b`), adds the
attachment ID to a name used twice (and says so on stderr), and exits 1
naming any file that failed while the others still download. Ctrl-C removes
the partly written file and exits 2. A file URL outside the service URL's
origin, or one holding a user name or password, is refused, and so is a
redirect to another host or from https to http.

## YouTrack commands

```sh
ytrack issue command APP-123 'State In Progress'
ytrack issue command APP-123 APP-124 APP-130 'Assignee me' --dry-run   # preview only
ytrack issue command APP-123 'State Fixed' --comment 'Fixed in 2026.3' --silent
ytrack issue command APP-123 APP-124 'tag regression' --json issues,commands
```

`issue command` takes YouTrack's own [command
language](https://www.jetbrains.com/help/youtrack/cloud/commands.html). The
last argument is the command and every argument before it is an issue; all of
them change in one request. Issue URLs in one call must share a host.

- `--dry-run` asks YouTrack how it parses the command and changes nothing; it
  exits 1 when a part is not understood.
- `--comment` adds a comment with the change; `--silent` skips notifications
  and needs the *Apply Commands Silently* permission (on a 403, retry
  without it).
- When YouTrack rejects the command (HTTP 400), the error says nothing was
  changed and prints the `--dry-run` line that shows how each part parses.
- Command keywords (`for`, `tag`, `add`, ...) follow the instance's
  language: write `<Field> <value>`, such as `Assignee me` or
  `State Fixed`, which works everywhere.
- `--json` reports `issue`, `url`, `issues` (every issue with its URL),
  `command`, `dryRun` and `commands` (the parsed parts).

## Links

```sh
ytrack issue link --list-types                        # name and both phrases of each link type
ytrack issue link APP-1 APP-2 --type 'depends on'     # APP-1 depends on APP-2
ytrack issue link APP-1 APP-5 APP-6 --type 'parent for'
ytrack issue link APP-1 APP-9 --type relates
ytrack issue link APP-1 --list                        # phrase<TAB>issue per linked issue
ytrack issue link APP-1 --list --json phrase,direction,issues
ytrack issue unlink APP-1 APP-2 --type 'depends on'
```

```text
$ ytrack issue link --list-types
NAME       OUTWARD           INWARD      DIRECTED
Relates    relates to                    false
Depend     is required for   depends on  true
Duplicate  is duplicated by  duplicates  true
Subtask    parent for        subtask of  true
```

A link reads "APP-1 *phrase* APP-2". `--type` takes a link type's phrase as
the instance defines it (`depends on`, `is required for`, `subtask of`,
`relates to`, ...; case is ignored), and the phrase picks the direction:
"A depends on B" and "B is required for A" are the same link. An undirected
type also answers to its name (`--type Relates`); a directed type's name
(`Depend`) says no direction and is refused with the valid phrases. These
phrases are the same in every UI language, unlike link words in
`issue command`, which YouTrack localizes.

Linking issues that are already linked that way changes nothing. A target
issue that does not exist is named (`issue APP-999 not found on <host>`).
`issue unlink` refuses, and removes nothing, when a target is not linked that
way.

`--json` reports `issue`, `type`, `phrase`, `direction` (`OUTWARD`, `INWARD`
or `BOTH`) and `issues`; `--list-types --json` reports `id`, `name`,
`sourceToTarget`, `targetToSource` and `directed`.
