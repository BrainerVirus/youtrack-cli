# Logging in and choosing a host

## Log in

```sh
ytrack auth login
```

ytrack asks for your YouTrack URL (`acme.youtrack.cloud`, or a Server URL
such as `https://tools.acme.com/youtrack`) and opens your profile's
**Account Security** tab. Create a permanent token named `ytrack` with the
scope `YouTrack` and paste it at the hidden prompt. If your Server uses an
external Hub, ytrack links to the
[token docs](https://www.jetbrains.com/help/youtrack/cloud/manage-permanent-token.html) instead.

Scripted, the token comes from standard input, never from an argument:

```sh
ytrack auth login --host acme.youtrack.cloud --with-token < token.txt
```

The token is checked against `/api/users/me` and saved in the OS keyring
(macOS Keychain, Windows Credential Manager, Secret Service on Linux). With no
keyring, login fails unless you pass `--insecure-storage`, which writes
`credentials.yml` (mode 0600) in the config directory. On Windows the mode
bits are not enforced; the file is protected by the ACL of your
`%AppData%\ytrack` folder, which by default only your account can read.
Changing a saved https host to plain http needs `--allow-insecure-http`.

```sh
ytrack auth status        # hosts, accounts, masked tokens; exit 4 if a token is missing or rejected
ytrack auth status --json host,active,resolvedBy,login,state
ytrack auth switch --host tools.acme.com/youtrack    # change the default host
ytrack auth token         # print the token (the only command that does)
ytrack auth logout        # forget the active host's token (it is not revoked in YouTrack)
```

`auth status` exits 0 when every token works, 4 when a host has no token or
rejects it (a 403 from `/api/users/me` means the token lacks the `YouTrack`
scope), and 1 when a check fails for another reason.

## Tie hosts to folders

With more than one host, tie each to the folders you work in; a command run
inside one of them talks to that host:

```sh
ytrack auth paths --host work.youtrack.cloud --add '~/work/**'
ytrack auth paths --host acme.youtrack.cloud --add '~/oss/acme/**'
ytrack auth paths --host acme.youtrack.cloud --remove '~/oss/acme/**'
ytrack auth paths         # every host's paths
ytrack auth status        # shows which host this folder uses and why
```

A path is absolute or starts with `~/`; `*` matches within one folder name
and `**` any number of folders (`~/work/**` also matches `~/work`). When
paths of several hosts match, the one with the longest text before its first
wildcard wins. The paths live in `hosts.yml` next to each host. With only one
logged-in host, `--host` may be left out of `--add` and `--remove`.

On macOS and Windows, whose file systems ignore case by default, paths match
folders in any case: `~/Work/**` and `~/work/**` are one path, and adding the
second reports it as already present. Windows paths may use `\` and a drive
letter (`C:/Code/**` matches `c:\code\app`). On Linux and other systems case
matters. Two paths of different hosts that differ only by case are an error
when they are the most specific paths a folder matches: ytrack names both
and picks neither. A narrower path that also matches still wins.

## Which host a command uses

1. `--host`;
2. the host of an issue URL argument (an issue URL on another host than
   `--host` or `YTRACK_HOST` is an error);
3. `YTRACK_HOST`;
4. the host whose `auth paths` match the current folder;
5. in a linked git worktree, the host whose `auth paths` match the same
   folder in the repository's main checkout, so a worktree kept outside the
   repository's folder still uses its host (`auth status --json resolvedBy`
   reports `path (main checkout)`);
6. the default host (`ytrack auth switch`).

When the folder rule or the default host picked a host that does not have an
issue key's project (`APP` in `APP-123`) and exactly one other logged-in host
does, issue and work item commands use that host and say so on stderr:

```text
using acme.youtrack.cloud for APP-* (the folder rule picked work.youtrack.cloud)
```

Reads try the picked host first and look the project up only after a 404;
writes look it up first, so they never reach a host that does not own the
key, and recheck the picked host before acting on a cached "it lacks the
project". When two or more other hosts have the project, the not-found error
ends with `try --host <a> or --host <b>`; no other error suggests a host. A
host whose `auth paths` tie it to other folders is never probed, switched to
or named. `--host`, `YTRACK_HOST`, an issue URL and `YTRACK_TOKEN` turn the
switch off. With more than one logged-in host, HTTP errors name the host that
answered and why it was chosen.

The answers are cached for 7 days per host URL and project in
`projects.json` in the cache directory: `$YTRACK_CACHE_DIR`, else
`$XDG_CACHE_HOME/ytrack`, else `~/.cache/ytrack` on Linux,
`~/Library/Caches/ytrack` on macOS and `%LocalAppData%\ytrack` on Windows.
Delete that file after creating or moving a project.

## Tokens in CI

`YTRACK_TOKEN` overrides the stored token and is never saved. It goes to a
host named by `--host` or `YTRACK_HOST` without a warning (the CI setup), and
to a host taken from a pasted issue URL only when that host is logged in.
Without either, it goes to the active host.

```sh
YTRACK_HOST=acme.youtrack.cloud YTRACK_TOKEN="$TOKEN" ytrack issue list --project APP
```
