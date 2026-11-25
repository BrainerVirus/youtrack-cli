# Installing and upgrading ytrack

## Install scripts

| Platform | Command | Installs to |
| --- | --- | --- |
| Linux, macOS | `curl -fsSL https://raw.githubusercontent.com/BrainerVirus/youtrack-cli/main/install.sh \| sh` | `~/.local/bin/ytrack` |
| Windows | `irm https://raw.githubusercontent.com/BrainerVirus/youtrack-cli/main/install.ps1 \| iex` | `%LOCALAPPDATA%\Programs\ytrack\ytrack.exe` |

Neither needs a package manager, sudo or administrator rights. Each script:

1. detects the OS (`uname -s`) and CPU (`uname -m`; on Windows the OS
   architecture), amd64 or arm64. A macOS shell running under Rosetta gets the
   native arm64 build;
2. finds the latest release (or `YTRACK_VERSION`) and downloads
   `ytrack_<version>_checksums.txt`;
3. when `cosign` is on `PATH`, downloads `ytrack_<version>_checksums.txt.sigstore.json`
   and runs `cosign verify-blob` with the release workflow identity (below).
   A failed check stops the install. Without cosign it says so and goes on
   with the checksum alone;
4. downloads `ytrack_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) and
   stops unless its SHA-256 matches the line in the checksums file;
5. copies the binary next to the target and renames it into place, then
   prints how to add the directory to `PATH` if it is missing;
6. installs shell completions (below) and prints how to enable them.

Environment:

| Variable | Meaning |
| --- | --- |
| `YTRACK_VERSION` | release tag to install, e.g. `v0.3.0` (default: latest) |
| `YTRACK_INSTALL_DIR` | target directory |
| `YTRACK_NO_COMPLETIONS` | `1` skips the shell completions |
| `YTRACK_RELEASES_URL` | download root, default `https://github.com/BrainerVirus/youtrack-cli/releases` (files under `download/<tag>/`) |
| `YTRACK_RELEASES_API_URL` | API root, default `https://api.github.com/repos/BrainerVirus/youtrack-cli` (reads `releases/latest`) |

The last two exist for mirrors and for the scripts' tests (`scripts/install_test.go`
runs both scripts against a fake release mirror; the PowerShell test runs where
`pwsh` is installed).

## Other ways to install

```sh
brew install brainervirus/tap/ytrack                              # Homebrew, macOS and Linux
go install github.com/BrainerVirus/youtrack-cli/cmd/ytrack@latest # Go
```

Or download an archive for your platform (Linux, macOS, Windows; amd64 or
arm64) from the [releases page](https://github.com/BrainerVirus/youtrack-cli/releases),
unpack it and put `ytrack` on your `PATH`. Archives also contain shell
completions for bash, zsh and fish; check the download as
[below](#verifying-a-download). From a clone, `make build` writes
`bin/ytrack`.

A `go install …@latest` binary carries its release version and upgrades like
any other install. A `make build` binary from a commit that is not a release
tag (`0.7.0-3-gabc1234`, `-dirty`) is a development build: it never checks
for or installs releases.

## Shell completions

`install.sh` reads your login shell from `$SHELL` and copies the matching
script from the release archive (no root needed):

| Shell | Installed to | Enabling it |
| --- | --- | --- |
| zsh | the first writable `$fpath` directory under your home, else `${XDG_DATA_HOME:-~/.local/share}/zsh/site-functions/_ytrack` | in a `$fpath` dir: nothing. Otherwise add `fpath=(~/.local/share/zsh/site-functions $fpath)` and `autoload -Uz compinit && compinit` to `~/.zshrc`, before any other `compinit` |
| bash | `${XDG_DATA_HOME:-~/.local/share}/bash-completion/completions/ytrack` | loaded by bash-completion (v2) in new shells; without it, add `. ~/.local/share/bash-completion/completions/ytrack` to `~/.bashrc` |
| fish | `${XDG_CONFIG_HOME:-~/.config}/fish/completions/ytrack.fish` | nothing: fish loads it in new shells |

The script prints the path it wrote and the enabling step. For another shell,
or to install by hand, generate the script with `ytrack completion`:

```sh
ytrack completion zsh > ~/.local/share/zsh/site-functions/_ytrack
ytrack completion bash > ~/.local/share/bash-completion/completions/ytrack
ytrack completion fish > ~/.config/fish/completions/ytrack.fish
```

On Windows, `install.ps1` prints the line to add to your PowerShell profile
(`$PROFILE`; create it with `New-Item -ItemType File -Force $PROFILE`):

```powershell
ytrack completion powershell | Out-String | Invoke-Expression
```

Completion covers commands and flags, `--json` field names, issue IDs (the 50
most recently updated unresolved issues, narrowed by project once you type
`APP-`, shown with their summaries) and `--project` short names. Issue and
project suggestions ask the logged-in host and give up after 2 seconds; with
no login they suggest nothing.

## Verifying a download

Every release from the signing workflow on has `ytrack_<version>_checksums.txt` and a
Sigstore bundle `ytrack_<version>_checksums.txt.sigstore.json`. The bundle is a
cosign keyless signature made in GitHub Actions by `.github/workflows/release.yml`:

- OIDC issuer: `https://token.actions.githubusercontent.com`
- certificate identity: `https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@refs/tags/<tag>`
  (only release tags: a manual rerun of the workflow is dispatched from the tag, so it signs
  with the same identity; `ytrack upgrade` and the install scripts refuse any branch ref)

```sh
cosign verify-blob \
  --bundle ytrack_0.3.0_checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@refs/tags/v0.3.0 \
  ytrack_0.3.0_checksums.txt
sha256sum --check --ignore-missing ytrack_0.3.0_checksums.txt      # Linux
shasum -a 256 --check --ignore-missing ytrack_0.3.0_checksums.txt  # macOS
```

On Windows, compare `Get-FileHash ytrack_0.3.0_windows_amd64.zip` with the
line in the checksums file.

## Upgrades

```sh
ytrack upgrade                  # latest release
ytrack upgrade --check          # report only; changes nothing
ytrack upgrade --version v0.3.0 # a given release (also older ones)
```

`ytrack upgrade` downloads the checksums file and its bundle first and
verifies the signature with [sigstore-go](https://github.com/sigstore/sigstore-go),
without cosign installed: the certificate must chain to Sigstore's public
Fulcio CA at a time attested by the public Rekor log (v1 or v2) or a signed
timestamp, carry a certificate transparency SCT, match the log entry, sign
the checksums file's SHA-256, and name the issuer and workflow identity above
(a release tag `vX.Y.Z`, never a branch). The trusted root comes from Sigstore's TUF
repository, so key rotations need no ytrack release; its metadata is cached
in `ytrack/sigstore-tuf` under the user cache directory (`~/.cache` on Linux,
`~/Library/Caches` on macOS, `%LOCALAPPDATA%` on Windows).
Then it downloads the archive, checks its SHA-256 against the verified
checksums, writes the new binary next to the running one, runs its
`ytrack version` (it must exit 0 and name the release) and renames it over
the running one. On Windows the running `ytrack.exe` is first renamed to
`ytrack.exe.old`, which the next upgrade deletes. Any failed check refuses the
upgrade and leaves the installed binary as it was. After an upgrade, the new
binary runs `ytrack skill sync` to refresh the agent skill copies it
installed; a release without that command skips it silently. One upgrade runs
at a time: each holds `upgrade.lock` in the state directory (a lock older than
30 minutes is taken to be left by a crash).

If the Sigstore trusted root cannot be fetched (often a proxy blocking
`tuf-repo-cdn.sigstore.dev`), the upgrade is refused with an error naming the
TUF repository; set `HTTPS_PROXY` so ytrack can reach it.

Each file is fetched from its release download URL. A network error, HTTP 5xx
or HTTP 429 is retried twice, after 1 s and 2 s; other HTTP errors are not
retried. If the download URL still fails, the file is fetched once more
through the GitHub REST API (the asset's API URL with
`Accept: application/octet-stream`, with the same retries), because GitHub's
download CDN can serve a cached error for one asset while the API serves it.
ytrack then prints `downloaded <file> through the GitHub API instead`. Both
paths accept only https (no redirect off https) and the same size limits, and
the signature and checksum checks run on the bytes either way.

If the binary lives in a package manager's directory, `ytrack upgrade` prints
that manager's command and changes nothing:

| Installed by | Detected path | Command |
| --- | --- | --- |
| Homebrew | `Cellar/`, `Caskroom/`, `/opt/homebrew/`, `/home/linuxbrew/.linuxbrew/` | `brew upgrade brainervirus/tap/ytrack` |
| Scoop | `scoop\apps\`, `scoop\shims\` | `scoop update ytrack` |
| WinGet | `Microsoft\WinGet\Packages\`, `Microsoft\WinGet\Links\` | `winget upgrade ytrack` |

### Background upgrade

ytrack upgrades itself: once a day it asks GitHub for the latest release (see
below), and when that release is newer and was published at least 24 hours
ago, a command starts `ytrack upgrade --auto` in the background and exits
without waiting for it. The child runs in its own session (on Windows, its own
process group without a console) with no standard input, so a program reading
ytrack's output, such as an agent, never waits on it. It makes the same
signature, checksum and `ytrack version` checks as `ytrack upgrade`, then runs
the new binary's `ytrack skill sync`, and writes its output to
`auto-upgrade.log` in the state directory.

The 24-hour wait lets a broken release be pulled before it reaches you; a
manual `ytrack upgrade` installs a new release at once. The background upgrade
does not start:

- when the release check does not run (see the list below);
- with `auto_upgrade: false` in `config.yml`, or `YTRACK_NO_AUTO_UPGRADE=1`
  (`check_update: false` turns it off too);
- when Homebrew, Scoop or WinGet installed ytrack, or its directory is not
  writable;
- while another upgrade holds the lock, or within an hour of the last start;
- after a failure, for 1 day, then 2, 4 and at most 8 days after each failure
  in a row; a successful upgrade, manual or not, resets it.

The result is recorded in `state.yml`. The next command run on a terminal
prints this once, or, after a failure, the new-release notice followed by
the error and when it retries:

```text
ytrack upgraded 0.6.2 → 0.7.0 in the background
```

### New-release notice

After a command run on a terminal, ytrack prints one line on stderr when a
newer release (not a prerelease) exists, for example:

```text
A new release of ytrack is available: 0.2.0 → 0.3.0; run `ytrack upgrade` (https://github.com/BrainerVirus/youtrack-cli/releases/tag/v0.3.0)
```

It keeps printing it, at most once every 24 hours per release, while the
installed binary is behind, also when the background upgrade is on, and adds
the last background upgrade failure if there is one. It prints nothing when
stderr is not a terminal or under `--json`.

The notice comes from the release cached in `state.yml` in the state
directory (`$XDG_STATE_HOME/ytrack`, else `%LOCALAPPDATA%\ytrack` on Windows,
else `~/.local/state/ytrack`), so printing it costs nothing. ytrack refreshes
that cache from GitHub at most once every 24 hours, in the background while
the command runs, with or without a terminal or `--json`, so agents that run
ytrack keep it current. A refresh still in flight 300 ms after the command
finishes is abandoned (and retried next run), so a slow or unreachable server
never delays the exit by more than that. A failed check also counts, so an
offline machine asks once a day; it keeps the release cached before it. The
check makes no request, and nothing is printed or upgraded:

- in CI (`CI`, `BUILD_NUMBER` or `RUN_ID` set);
- for `ytrack skill`, `ytrack upgrade` and `ytrack completion`;
- for development builds (a version that is not `X.Y.Z`);
- with `YTRACK_NO_UPDATE_NOTIFIER=1`, or `check_update: false` in `config.yml`.
