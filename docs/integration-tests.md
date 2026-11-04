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
with `--host`, so other logged-in hosts are not contacted):

1. `auth status`: the token works; the login is used below.
2. `issue create` in the project, summary `ytrack-it <run id> create`.
3. `issue view` and `issue list` find it.
4. `issue edit` changes summary and description; `issue view` confirms.
5. `issue comment` adds a comment; `issue view --json comments` shows it.
6. `issue command <id> 'Assignee me' --dry-run`, then applied with
   `--silent`; the assignee becomes you. The command uses the field name, not
   a keyword such as `for`: YouTrack localizes command keywords with the
   instance's language (a Spanish instance answers "Comando desconocido:
   for"), while field names work on every locale.
7. `work-item add` (30m), `list`, `edit` (45m) and `delete`.

The run id is a UTC timestamp plus random hex, e.g. `20261104-153000-a1b2c3`.

## What it creates and cleans up

One issue per run, with one comment and one work item on it. Cleanup runs
whether the steps passed or failed:

- it deletes each issue the run created (`ytrack api -X DELETE /issues/<id>`;
  YouTrack deletes its comments and work items with it), then
- it sweeps the 50 newest issues of the project and deletes any whose summary
  starts with this run's `ytrack-it <run id>` prefix, which catches an issue
  created by a step that failed before it reported the ID.

If cleanup itself fails, the test fails with the prefix to search for, so you
can delete leftovers by hand (search the project for `ytrack-it`).

## Tests of the suite

`go test ./test/integration/` (part of `go test ./...`) runs the suite
against a fake YouTrack on loopback with no `YTRACK_IT`: every command path
runs, and cleanup leaves nothing behind, also when a step fails, when a
create response is lost, and it reports a failed cleanup. The host checks
are unit-tested there too. `-short` skips the fake-server runs, which build
the binary.
