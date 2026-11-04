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
   prints how to add the directory to `PATH` if it is missing.

Environment:

| Variable | Meaning |
| --- | --- |
| `YTRACK_VERSION` | release tag to install, e.g. `v0.3.0` (default: latest) |
| `YTRACK_INSTALL_DIR` | target directory |
| `YTRACK_RELEASES_URL` | download root, default `https://github.com/BrainerVirus/youtrack-cli/releases` (files under `download/<tag>/`) |
| `YTRACK_RELEASES_API_URL` | API root, default `https://api.github.com/repos/BrainerVirus/youtrack-cli` (reads `releases/latest`) |

The last two exist for mirrors and for the scripts' tests (`scripts/install_test.go`
runs both scripts against a fake release mirror; the PowerShell test runs where
`pwsh` is installed).

## Verifying a download

Every release from the signing workflow on has `ytrack_<version>_checksums.txt` and a
Sigstore bundle `ytrack_<version>_checksums.txt.sigstore.json`. The bundle is a
cosign keyless signature made in GitHub Actions by `.github/workflows/release.yml`:

- OIDC issuer: `https://token.actions.githubusercontent.com`
- certificate identity: `https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@refs/tags/<tag>`
  (`@refs/heads/main` for a manual rerun of the workflow)

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
verifies the signature without cosign installed: the certificate must chain
to Sigstore's public Fulcio CA (embedded in ytrack) at the time the public
Rekor log recorded the signature, that log's signed entry timestamp must
verify and match this signature, certificate and file, and the certificate
must name the issuer and workflow identity above (a tag `vX.Y.Z` or `main`).
Then it downloads the archive, checks its SHA-256 against the verified
checksums, writes the new binary next to the running one and renames it over
it. On Windows the running `ytrack.exe` is first renamed to `ytrack.exe.old`,
which the next upgrade deletes. Any failed check refuses the upgrade and
leaves the installed binary as it was.

If the binary lives in a package manager's directory, `ytrack upgrade` prints
that manager's command and changes nothing:

| Installed by | Detected path | Command |
| --- | --- | --- |
| Homebrew | `Cellar/`, `Caskroom/`, `/opt/homebrew/`, `/home/linuxbrew/.linuxbrew/` | `brew upgrade brainervirus/tap/ytrack` |
| Scoop | `scoop\apps\`, `scoop\shims\` | `scoop update ytrack` |
| WinGet | `Microsoft\WinGet\Packages\`, `Microsoft\WinGet\Links\` | `winget upgrade ytrack` |

### New-release notice

After a command, ytrack prints one line on stderr when a newer release (not a
prerelease) exists, for example:

```text
A new release of ytrack is available: 0.2.0 → 0.3.0; run `ytrack upgrade` (https://github.com/BrainerVirus/youtrack-cli/releases/tag/v0.3.0)
```

It asks GitHub at most once every 24 hours, in the background while the
command runs (a slow answer delays exit by at most 3 seconds), and records
the check in `state.yml` in the state directory: `$XDG_STATE_HOME/ytrack`,
else `%LOCALAPPDATA%\ytrack` on Windows, else `~/.local/state/ytrack`. A failed
check also counts, so an offline machine asks once a day. It makes no
request and prints nothing:

- in CI (`CI`, `BUILD_NUMBER` or `RUN_ID` set);
- when stderr is not a terminal;
- under `--json`, and for `ytrack upgrade` and `ytrack completion`;
- for development builds (a version that is not `X.Y.Z`);
- with `YTRACK_NO_UPDATE_NOTIFIER=1`, or `check_update: false` in `config.yml`.
