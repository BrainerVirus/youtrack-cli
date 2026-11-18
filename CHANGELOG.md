# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
