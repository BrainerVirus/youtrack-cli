# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `ytrack skill install` with no `--dir` installs the agent skill into `~/.agents/skills` and `~/.claude/skills`, and ytrack remembers every directory it installed into. `ytrack skill list`, `ytrack skill sync` and `ytrack skill uninstall --dir` manage them. Existing copies are adopted on first run.
- The first run of a newer ytrack refreshes every remembered home-level skill copy, so no copy is older than the binary; an older ytrack never rewrites a newer copy, and copies inside a repository stay manual.
### Fixed

- Concurrent `ytrack skill install` runs into one directory no longer fail or leave temporary directories behind.
