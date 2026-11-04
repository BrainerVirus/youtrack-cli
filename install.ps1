# Install ytrack, the unofficial YouTrack CLI, on Windows without a package
# manager or administrator rights:
#
#   irm https://raw.githubusercontent.com/BrainerVirus/youtrack-cli/main/install.ps1 | iex
#
# It downloads the release archive for this CPU, checks its SHA-256 against
# the release's checksums file, checks that file's cosign signature when
# cosign is installed, and puts ytrack.exe in %LOCALAPPDATA%\Programs\ytrack.
#
# Environment:
#   YTRACK_VERSION      release tag to install, e.g. v0.3.0 (default: latest)
#   YTRACK_INSTALL_DIR  target directory (default: %LOCALAPPDATA%\Programs\ytrack)
#   YTRACK_NO_COMPLETIONS=1  skip the shell completion instructions
#   YTRACK_RELEASES_URL, YTRACK_RELEASES_API_URL
#                       release download and API roots, for mirrors and tests
#
# Later upgrades: `ytrack upgrade`.

& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    Set-StrictMode -Version 3.0

    $repo = 'BrainerVirus/youtrack-cli'
    $releasesUrl = if ($env:YTRACK_RELEASES_URL) { $env:YTRACK_RELEASES_URL } else { "https://github.com/$repo/releases" }
    $apiUrl = if ($env:YTRACK_RELEASES_API_URL) { $env:YTRACK_RELEASES_API_URL } else { "https://api.github.com/repos/$repo" }
    $installDir = if ($env:YTRACK_INSTALL_DIR) { $env:YTRACK_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\ytrack' }
    $signerIssuer = 'https://token.actions.githubusercontent.com'
    $signerIdentity = '^https://github\.com/BrainerVirus/youtrack-cli/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'

    function Fail([string]$msg) { throw "ytrack install: $msg" }

    if ($PSVersionTable.PSVersion.Major -lt 6) {
        # Windows PowerShell 5.1 defaults to TLS 1.0, which GitHub refuses.
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    }

    $osArch = $null
    try { $osArch = [string][System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture } catch { $osArch = $null }
    if (-not $osArch) {
        $osArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    }
    $arch = switch -Regex ($osArch) {
        '^(X64|AMD64)$' { 'amd64'; break }
        '^(Arm64|ARM64)$' { 'arm64'; break }
        default { Fail "unsupported CPU architecture: $osArch" }
    }

    $tag = $env:YTRACK_VERSION
    if (-not $tag) {
        try {
            $latest = Invoke-WebRequest -UseBasicParsing -Uri "$apiUrl/releases/latest"
            $tag = ($latest.Content | ConvertFrom-Json).tag_name
        } catch {
            Fail "cannot find the latest release: $($_.Exception.Message)"
        }
    }
    if (-not $tag.StartsWith('v')) { $tag = "v$tag" }
    if ($tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$') { Fail "invalid release tag: '$tag'" }
    $version = $tag.Substring(1)

    $archive = "ytrack_${version}_windows_${arch}.zip"
    $checksums = "ytrack_${version}_checksums.txt"
    $base = "$releasesUrl/download/$tag"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("ytrack-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Downloading ytrack $version for windows/$arch..."
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$base/$checksums" -OutFile (Join-Path $tmp $checksums)
        } catch {
            Fail "cannot download $checksums for ${tag}: $($_.Exception.Message)"
        }

        if (Get-Command cosign -ErrorAction SilentlyContinue) {
            $bundle = Join-Path $tmp "$checksums.sigstore.json"
            try {
                Invoke-WebRequest -UseBasicParsing -Uri "$base/$checksums.sigstore.json" -OutFile $bundle
            } catch {
                Fail "cannot download the signature $checksums.sigstore.json for $tag"
            }
            & cosign verify-blob --bundle $bundle --certificate-oidc-issuer $signerIssuer `
                --certificate-identity-regexp $signerIdentity (Join-Path $tmp $checksums) *> $null
            if ($LASTEXITCODE -ne 0) { Fail "the signature of $checksums does not verify; nothing was installed" }
            Write-Host "Verified the cosign signature of $checksums."
        } else {
            Write-Host 'cosign not found; skipping the signature check (the SHA-256 is still checked).'
        }

        $zip = Join-Path $tmp $archive
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile $zip
        } catch {
            Fail "cannot download $archive (no build for windows/$arch in ${tag}?)"
        }
        $want = $null
        foreach ($line in Get-Content (Join-Path $tmp $checksums)) {
            $fields = $line.Trim() -split '\s+'
            if ($fields.Count -eq 2 -and $fields[1].TrimStart('*') -eq $archive) { $want = $fields[0].ToLowerInvariant(); break }
        }
        if (-not $want) { Fail "$checksums does not list $archive" }
        $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLowerInvariant()
        if ($got -ne $want) { Fail "checksum mismatch for $archive (got $got, want $want); nothing was installed" }

        $extract = Join-Path $tmp 'x'
        Expand-Archive -Path $zip -DestinationPath $extract -Force
        $exe = Join-Path $extract 'ytrack.exe'
        if (-not (Test-Path -LiteralPath $exe)) { Fail "$archive does not contain ytrack.exe" }

        New-Item -ItemType Directory -Force -Path $installDir | Out-Null
        $target = Join-Path $installDir 'ytrack.exe'
        $staged = Join-Path $installDir '.ytrack.exe.new'
        Copy-Item -LiteralPath $exe -Destination $staged -Force
        if (Test-Path -LiteralPath $target) {
            # A running ytrack.exe cannot be overwritten, but it can be renamed.
            $old = "$target.old"
            Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
            Move-Item -LiteralPath $target -Destination $old -Force
        }
        Move-Item -LiteralPath $staged -Destination $target -Force
        Write-Host "Installed ytrack $version to $target"

        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $onPath = @(($userPath, $env:Path) -join ';' -split ';' | Where-Object { $_ -and ($_.TrimEnd('\') -ieq $installDir.TrimEnd('\')) }).Count -gt 0
        if (-not $onPath) {
            Write-Host ''
            Write-Host "$installDir is not on your PATH. Add it for your user with:"
            Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"$installDir;`" + [Environment]::GetEnvironmentVariable('Path', 'User'), 'User')"
            Write-Host 'then open a new terminal.'
        }

        if ($env:YTRACK_NO_COMPLETIONS -ne '1') {
            Write-Host ''
            Write-Host 'Shell completions for PowerShell: add this line to your profile ($PROFILE),'
            Write-Host 'then open a new terminal:'
            Write-Host '  ytrack completion powershell | Out-String | Invoke-Expression'
            Write-Host 'To create the profile first: New-Item -ItemType File -Force $PROFILE'
        }
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}
