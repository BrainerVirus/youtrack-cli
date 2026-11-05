# Security policy

ytrack stores YouTrack credentials and replaces its own binary on
`ytrack upgrade`, so security reports are taken seriously.

## Supported versions

Only the latest release receives fixes. Upgrade with `ytrack upgrade`.

## Reporting a vulnerability

Report it privately through
[GitHub's private vulnerability reporting](https://github.com/BrainerVirus/youtrack-cli/security/advisories/new).
Do not open a public issue, and do not include a real token or private
instance data; describe the problem and how to reproduce it.

You can expect an acknowledgement within a few days. Once a fix is released,
the advisory is published with credit, unless you prefer otherwise.

## How releases are protected

- Every release's checksums file is signed with
  [cosign](https://docs.sigstore.dev/) keyless by this repository's release
  workflow, and the signature is recorded in Sigstore's public transparency log.
- `ytrack upgrade` always verifies that signature, pinned to the workflow
  `BrainerVirus/youtrack-cli/.github/workflows/release.yml` run for a release
  tag, then checks the archive's SHA-256 before replacing anything. Its
  downloads are https only, including every redirect.
- `install.sh` and `install.ps1` always check the SHA-256, and check the
  signature too when `cosign` is installed.
- GitHub Actions are pinned to full commit SHAs, and dependencies are updated
  by Dependabot after a 7-day cooldown.

To verify a download by hand (replace `X.Y.Z`):

```sh
cosign verify-blob --bundle ytrack_X.Y.Z_checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@refs/tags/vX.Y.Z \
  ytrack_X.Y.Z_checksums.txt
sha256sum --check --ignore-missing ytrack_X.Y.Z_checksums.txt
```

## Credentials

`ytrack auth login` stores the token in the operating system's keyring
(macOS Keychain, Windows Credential Manager, Secret Service on Linux). With no
keyring, login fails unless you pass `--insecure-storage`, which writes a
`credentials.yml` readable only by your account. `--debug` and `YTRACK_DEBUG=1` redact credentials from the
HTTP log; still review debug output before sharing it.
