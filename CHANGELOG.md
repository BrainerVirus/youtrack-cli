# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- ytrack upgrades itself in the background once a newer release is a day old, also when only agents run it (no terminal, `--json`), with the same signature and checksum checks plus a `ytrack version` smoke test, then refreshes its agent skills. A failed background upgrade backs off (1, 2, 4, then 8 days) and is reported on the next terminal run. Turn it off with `auto_upgrade: false` or `YTRACK_NO_AUTO_UPGRADE=1`; it never runs in CI, for dev builds or for Homebrew, Scoop and WinGet installs.
- `ytrack skill install` with no `--dir` installs the agent skill into `~/.agents/skills` and `~/.claude/skills`, and ytrack remembers every directory it installed into. `ytrack skill list`, `ytrack skill sync` and `ytrack skill uninstall --dir` manage them. Existing copies are adopted on first run.
- The first run of a newer ytrack refreshes every remembered home-level skill copy, so no copy is older than the binary; an older ytrack never rewrites a newer copy, and copies inside a repository stay manual.
### Fixed

- Concurrent `ytrack skill install` runs into one directory no longer fail or leave temporary directories behind.
### Changed

- The daily release check now also runs without a terminal and under `--json`, so agent-only use learns about new releases.
