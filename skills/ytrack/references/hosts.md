# Hosts and auth

```sh
ytrack auth status                               # logged-in hosts; the active one is marked
ytrack auth status --json host,url,active,login,state
ytrack auth switch --host acme.youtrack.cloud    # change the default host
ytrack auth paths                                # folders tied to each host
```

## Which host a command uses

`--host`, else the host of an issue URL, else `YTRACK_HOST`, else the host
whose `ytrack auth paths` match the current folder, else the default host.
With more than one logged-in host, run `ytrack auth status` in the folder you
work in: it marks the active host and says why (`--host`, `YTRACK_HOST`, a
matching path or the default). Pass `--host` when that is not the instance
you need. Change paths only when the user asks
(`ytrack auth paths --host <h> --add '<dir>/**'`).

- When the folder or the default host picks a host without the issue's
  project and exactly one other logged-in host has it, ytrack uses that host
  and prints `using <host> for APP-* (...)` on stderr; no `--host` needed. If
  that pick is stale (a project was just created or moved), delete the cache
  file `projects.json` in `~/.cache/ytrack/` (or `$YTRACK_CACHE_DIR`).

## A key is not found

Run `ytrack auth status` in this folder first. The error names the host that
answered and why it was chosen; it suggests `try --host <a> or --host <b>`
only when two or more other logged-in hosts own the key's project (with one,
ytrack switches to it, as above). Never switch hosts on any other error,
and never read `hosts.yml` by hand.

## Logins and tokens

If no host is logged in, ask the user to run `ytrack auth login` themselves.
`ytrack auth status` exits 4 when any logged-in host has no token or a
rejected one, even if the active host works.
