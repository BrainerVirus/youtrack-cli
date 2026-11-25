# Real-instance test suite

`test/integration` proves ytrack against a real YouTrack instance by driving
the built `ytrack` binary the way a user does. It is opt-in: without
`YTRACK_IT=1` it is skipped, so `go test ./...`, `make test` and CI never
contact a YouTrack server.

## Run it

1. Create a free personal YouTrack Cloud instance (`<name>.youtrack.cloud`)
   with a personal account, and a scratch project in it. The project needs an
   Assignee field and **time tracking enabled** (project settings, Time
   Tracking). A project created through the REST API starts with time
   tracking off, and the work-item steps then fail.
2. Log in to it once: `ytrack auth login --host <name>.youtrack.cloud`. The
   token goes to the OS keyring. If you have other hosts, make the personal
   one the default (`ytrack auth switch`) or name it with `YTRACK_IT_HOST`.
3. Run the suite with the scratch project's short name:

   ```bash
   make it YTRACK_IT_PROJECT=SCR
   ```

   `make it` builds `bin/ytrack` and runs `TestIntegration` with that binary.

| Variable | Required | Meaning |
| --- | --- | --- |
| `YTRACK_IT` | yes (`make it` sets it) | `1` runs the suite; anything else skips it |
| `YTRACK_IT_PROJECT` | yes | short name of the scratch project |
| `YTRACK_IT_HOST` | no | host to test instead of ytrack's default host |
| `YTRACK_IT_TOKEN` | no | token to use instead of the one `ytrack auth login` stored |
| `YTRACK_IT_ALLOW_HOST` | no | allows one host outside `*.youtrack.cloud` (see below) |
| `YTRACK_IT_BIN` | no (`make it` sets it) | prebuilt binary; without it the suite builds one |

The token never has to be in the environment: by default the binary reads it
from the keyring, like any ytrack command. The suite strips every other
`YTRACK_*` variable from the binary's environment (except
`YTRACK_CONFIG_DIR`) and passes the target host with `--host` on every call,
so a stray `YTRACK_HOST` or `YTRACK_TOKEN` in your shell cannot redirect it.

## Hosts it refuses

The suite checks the host before it runs anything, and fails (it does not
skip) with the reason:

- **The work instance**: any host containing `enghouse` (for example
  `enghouseamg`). Always refused; `YTRACK_IT_ALLOW_HOST` cannot allow it.
- **JetBrains' public tracker**: `youtrack.jetbrains.com`, or any other
  `jetbrains.com` host. Always refused, even when allowed.
- **Anything that is not YouTrack Cloud**: a host whose name does not end in
  `.youtrack.cloud` is refused unless `YTRACK_IT_ALLOW_HOST` names exactly
  that host, e.g. `YTRACK_IT_ALLOW_HOST=yt.example.org` for a personal
  self-hosted instance.

A missing `YTRACK_IT_PROJECT`, or no logged-in host, also fails with the
variable or command to set.

## What it does

Every call goes to the target host only (`auth status` is limited to it
with `--host`, so other logged-in hosts are not contacted). Steps 1-4 run
in order and stop the run on the first failure, since everything after them
works on the run's issue. The families in steps 5-12 are independent: all
of them run, and the test reports every one that failed.

1. `auth status`: the token works; the login is used below.
2. `tag create` makes two tags, `ytrack-it-<run id>-a` and `-b`;
   `tag list` finds both.
3. `issue create --field Priority=Critical --tag <tag a>` in the project,
   summary `ytrack-it <run id> create`; `issue view` reads the field and the
   tag back, and `issue list` finds the issue. The project needs a Priority
   field with the values Critical and Minor, as every new YouTrack project
   has (value names are not localized).
4. `issue edit` changes summary and description, then
   `--field Priority=Minor --add-tag <tag b> --remove-tag <tag a>`;
   `issue view` confirms each change.
5. Comments: `issue comment` adds one and `issue view --json comments`
   shows it; `issue comment list`, `edit` and `delete` each read back with
   `comment list`. `issue comment --attach` then posts a comment with a PNG:
   `ytrack api` reads the comment's `attachments(id,name)`, and
   `issue attachments` shows the file with that comment's `commentId`, so
   the file must be on the comment itself.
6. Commands: `issue command <id> 'Assignee me'` with `--dry-run`, with
   `--assist` (parsed with no error), then applied with `--silent`; the
   assignee becomes you. The command uses the field name, not a keyword such
   as `for`: YouTrack localizes command keywords with the instance's
   language (a Spanish instance answers "Comando desconocido: for"), while
   field names work on every locale. `issue list --project <P> --assignee me`
   then lists the issue (ytrack sends `Assignee: <your login>`, never
   `for: me`), and `issue view <P>-999999999` fails with a not-found that
   names the target host and suggests no other host (`try --host` appears
   only when two or more other logged-in hosts own the key's project, and
   never with `--host`).
7. Attachments: `issue attach` uploads two text files and
   `issue attachments` lists them on the issue. `issue download <id> <name>`
   saves only that file, with its content; a second download refuses to
   overwrite it and names `--clobber`; `--clobber` overwrites a stale copy.
8. Reads: `project fields <P>` lists Priority, `issue count 'project: <P>'`
   counts at least the run's issue, `issue activities` shows its history,
   `issue watchers` answers, `query list` lists saved searches, and
   `issue list -q 'project: ' --query-assist` prints completions.
9. Votes and stars: `issue vote` then `unvote`, and `issue watch`, then
   `issue watchers` lists you, then `unwatch`, each read back with `--json`.
   YouTrack refuses a vote on an issue you reported, which the run's issue
   is: that refusal is the expected outcome, and `unvote` is then not run.
10. `work-item add` (30m) and `list`; `work-item report --project <P>` from
    yesterday to tomorrow (so the item's local day is inside whatever the
    time zone) includes it; then `edit` (45m) and `delete`.
11. A second issue, `ytrack-it <run id> delete`: `ytrack api
    '/issues?$top=1' --paginate --jq '.[].idReadable'` pages through the
    project one issue per request and must return both run issues, as many
    as `issue count` counts; then `issue delete --yes` removes the second
    issue and `issue view` reports it not found.
12. Local commands: `skill print` prints the skill's front matter,
    `skill list --json` answers, and `upgrade --check` reports the latest
    release (it calls GitHub; the fake-server tests skip it).

The run id is a UTC timestamp plus random hex, e.g. `20261104-153000-a1b2c3`.

The binary runs with `YTRACK_NO_UPDATE_NOTIFIER=1`, so a release binary
neither checks for nor installs a newer release during the run. When the
suite builds ytrack itself (no `YTRACK_IT_BIN`), and in the fake-server
tests, it builds with `-buildvcs=false`, which stamps version `dev`: a
plain `go build` stamps a Go pseudo-version, which ytrack treats as a release
and which would rewrite your installed agent skill copies. `make it` stamps
a `git describe` version, which is not a release either.

## What it creates and cleans up

Per run: two issues (the second is deleted by step 11), two tags, three
comments on the first issue (one deleted by step 5, one with an attached
PNG), two attached text files and one work item (deleted by step 10).
Cleanup runs whether the steps passed or failed:

- it deletes each issue the run created and has not deleted yet
  (`ytrack api -X DELETE /issues/<id>`; YouTrack deletes its comments,
  attachments and work items with it), then
- it sweeps the 50 newest issues of the project and deletes any whose summary
  starts with this run's `ytrack-it <run id>` prefix, which catches an issue
  created by a step that failed before it reported the ID, then
- it deletes each tag the run created (`ytrack api -X DELETE /tags/<id>`),
  and sweeps `tag list` for any tag whose name starts with
  `ytrack-it-<run id>`.

If cleanup itself fails, the test fails with the prefix to search for, so you
can delete leftovers by hand (search the project for `ytrack-it`, and
`ytrack tag list` for tags named `ytrack-it-`).

## Tests of the suite

`go test ./test/integration/` (part of `go test ./...`) runs the suite
against a fake YouTrack on loopback with no `YTRACK_IT`: every command path
runs, and cleanup leaves nothing behind, also when a step fails, when a
create response is lost, and it reports a failed cleanup. The host checks
are unit-tested there too. `-short` skips the fake-server runs, which build
the binary.
