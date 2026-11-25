# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.7.1] - 2026-11-25

### Changed

- The README is now a short start (install, login, a five-minute tour, the agent skill) and the reference lives in `docs/`: auth and hosts, issues, time tracking, the REST API, scripting (output, JSON, exit codes, environment, files), install and upgrades, and the agent skill. Every page covers the 0.6 and 0.7 features, and a test checks every `ytrack` command and flag quoted in the README and `docs/` against the command tree.
- `ytrack --help` lists every environment variable ytrack reads (`YTRACK_CACHE_DIR`, `YTRACK_NO_AUTO_UPGRADE`, `CLICOLOR_FORCE`), and says `YTRACK_NO_UPDATE_NOTIFIER` also stops the release check and the background upgrade.

### Fixed

- `work-item edit --help` no longer calls `--date auto` the default; an edit without `--date` keeps the day.

## [0.7.0] - 2026-11-21

### Added

- `issue link` and `issue unlink` add, list (`--list`) and remove links between issues by the link type's own phrase (`--type 'depends on'`), the same in every UI language, so agents no longer need the localized `issue command 'link …'`; `issue link --list-types` lists each type's phrases (#48).

### Changed

- Dependencies updated: spf13/pflag 1.1.0, sigstore timestamp-authority 2.2.1 and rekor 1.6.1 (stricter release-signature verification), x/crypto 0.59.0, grpc 1.85.0; release tooling goreleaser 2.20.2 and cosign-installer 4.1.3. Help now shows bool flags as `--flag[=true|false]`, and a bad typed flag value says e.g. `must be an integer`.
- A usage error names the flag you were probably after: an unknown flag suggests close ones (`--limt` -> `did you mean --limit?`, `--limit` on `issue view` -> `--comments-limit`), and a number after a flag that takes no value says so (`--comments 12` -> `for a number use --comments-limit 12`). Found with the skill bench: weak models guess flags.
- `work-item add` and `work-item edit` take a work item type's id in `--type` as well as its name, so the same command works on instances in any language; an unknown type lists each valid name with its id (#53).
- `--jq` or `--template` without `--json` now uses every `--json` field instead of failing, as `ytrack api --jq` already did (#53).
- The agent skill's SKILL.md is 34% smaller and leads with "never guess a flag, run --help first"; rare host and attachment detail moved to references. Measured with a new local weak-model bench (`go run ./bench`, never in CI): Haiku 4.5 and Haiku 5.5 now pass all 10 cases (from 87% and 93%) with about 7% fewer input tokens.

### Fixed

- `ytrack skill sync` keeps a copy installed by a newer ytrack (reported as `kept`) instead of downgrading it; `--force` rewrites it at this version (#53).
- `ytrack skill list` reports a copy installed by a newer ytrack as `newer` (this ytrack leaves it alone) instead of `stale`, and no longer suggests a `skill sync` that would downgrade it (#53).
- `issue link` names a missing target issue (`issue DEMO-999 not found on <host>`) instead of YouTrack's raw HTTP 400 in the instance's language (#62).
- `ytrack api` says on stderr when YouTrack returned only `$type` and `id` because the request named no `--fields`, instead of printing an empty `--jq` result silently (#53).
- A command YouTrack rejects with HTTP 400 in `ytrack issue command` now says nothing was changed and prints the `--dry-run` line that shows how YouTrack parses each part (#53).
- `--field` on `issue create` and `issue edit` refuses a value for a version, build, enum or owned field whose bundle has no values yet, naming the field, instead of sending it and showing YouTrack's raw localized HTTP 400 (#50).
- `ytrack api` refuses `-f`/`-F` without `-X` when every parameter only reads (`query`, `fields`, `customFields`, `$top`, `$skip`) instead of sending a POST, which on `/issues` creates an issue; add `-X GET` to read or `-X POST` to send the POST (#51).

## [0.6.2] - 2026-11-19

### Fixed

- `issue list --assignee me` lists your issues on a non-English instance: ytrack sends `Assignee: <your login>` (the login `auth login` saved, or `/api/users/me` with `YTRACK_TOKEN`) instead of the `for:` keyword, which YouTrack translates and which then matched nothing. Every `--assignee` value now uses the `Assignee` field. `me` matches in any case, and when YouTrack rejects the search (400) the error asks whether the `--assignee` value is a login.
- Errors no longer end with `try --host <other host>`. A not-found issue key suggests other hosts only when the folder rule or the default host picked the host (no `--host`, issue URL, `YTRACK_HOST` or `YTRACK_TOKEN`), the picked host lacks the key's project and two or more other logged-in hosts have it; when exactly one has it, ytrack still switches to it and says so. A host whose `auth paths` fence it to other folders is never probed, switched to or named. A 400, 401, 403 or 5xx never suggests a host.

## [0.6.1] - 2026-11-18

### Fixed

- `issue comment --attach` uploads each file straight to the comment, so YouTrack Cloud shows the files on the comment (linking issue attachments to a comment by id is silently ignored). When YouTrack keeps a file on the issue but the comment still shows it, the command exits 0 with a warning; it exits 1 only for a file that is neither attached nor shown, and says never to run the command again because the comment already exists. `--json` reports each file's state (comment, issue or failed).

## [0.6.0] - 2026-11-18

### Added

- ytrack upgrades itself in the background once a newer release is a day old, also when only agents run it (no terminal, `--json`), with the same signature and checksum checks plus a `ytrack version` smoke test, then refreshes its agent skills. A failed background upgrade backs off (1, 2, 4, then 8 days) and is reported on the next terminal run. Turn it off with `auto_upgrade: false` or `YTRACK_NO_AUTO_UPGRADE=1`; it never runs in CI, for dev builds or for Homebrew, Scoop and WinGet installs.
- `ytrack skill install` with no `--dir` installs the agent skill into `~/.agents/skills` and `~/.claude/skills`, and ytrack remembers every directory it installed into. `ytrack skill list`, `ytrack skill sync` and `ytrack skill uninstall --dir` manage them. Existing copies are adopted on first run.
- The first run of a newer ytrack refreshes every remembered home-level skill copy, so no copy is older than the binary; an older ytrack never rewrites a newer copy, and copies inside a repository stay manual.

### Changed

- The daily release check now also runs without a terminal and under `--json`, so agent-only use learns about new releases.
- Each GitHub release's notes now start with its section of this changelog.

### Fixed

- Concurrent `ytrack skill install` runs into one directory no longer fail or leave temporary directories behind, on Windows too.

## [0.5.4] - 2026-11-17

### Added

- `ytrack issue attachments <ID>` lists an issue's attachments, including those on comments, and `ytrack issue download <ID> [<name|id>...] [-o <dir>]` downloads them through the issue's host, so agents no longer fetch signed URLs with a raw token. It never overwrites without `--clobber` and exits 1 naming each file that failed.

## [0.5.3] - 2026-11-17

### Fixed

- `--field Type=Task` and other custom fields resolve when the project's settings are hidden from your account, and `issue edit` reads the issue back and exits 1 naming any field YouTrack did not change.

## [0.5.2] - 2026-11-17

### Fixed

- ytrack uses the logged-in host that owns an issue key when the folder or the default host picks one without that project, and says so on stderr; API errors name the host that answered and the other hosts to try.

## [0.5.1] - 2026-11-17

### Fixed

- `issue comment --attach` uploads each file to the issue and links it to the comment, so attached images show; it exits 1 naming any file that did not attach.

Releases before 0.5.1 are described in their [GitHub release notes](https://github.com/BrainerVirus/youtrack-cli/releases).
