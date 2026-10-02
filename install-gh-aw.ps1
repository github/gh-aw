#!/usr/bin/env pwsh

# Script sync note: install-gh-aw.ps1 is canonical. actions/setup-cli/install.ps1 is copied from install-gh-aw.ps1.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$SkipChecksum = $false
$TryGhInstall = $false
$Version = ""

function Test-EnvVarPresent {
    param([Parameter(Mandatory)][string]$Name)

    return $null -ne (Get-Item "Env:$Name" -ErrorAction SilentlyContinue)
}

if ($env:INPUT_VERSION) {
    $Version = $env:INPUT_VERSION
    $TryGhInstall = $true
}

foreach ($arg in $args) {
    switch -Regex ($arg) {
        "^(--skip-checksum|-SkipChecksum)$" {
            $SkipChecksum = $true
            continue
        }
        "^(--gh-install|-GhInstall)$" {
            $TryGhInstall = $true
            continue
        }
        default {
            if (-not $Version) {
                $Version = $arg
            }
        }
    }
}

$NoColor = $false
if ($env:CI -or (Test-EnvVarPresent "NO_COLOR") -or (Test-EnvVarPresent "NO_COLORS") -or [Console]::IsOutputRedirected -or $env:TERM -eq "dumb") {
    $NoColor = $true
}

function Write-LogLine {
    param(
        [Parameter(Mandatory)][string]$Level,
        [Parameter(Mandatory)][string]$Message,
        [string]$Color = ""
    )

    if ($NoColor -or [string]::IsNullOrEmpty($Color)) {
        Write-Host "[$Level] $Message"
        return
    }

    Write-Host "[$Level]" -ForegroundColor $Color -NoNewline
    Write-Host " $Message"
}

function Write-Info { param([string]$Message) Write-LogLine -Level "INFO" -Message $Message -Color "Blue" }
function Write-Success { param([string]$Message) Write-LogLine -Level "SUCCESS" -Message $Message -Color "Green" }
function Write-WarningMessage { param([string]$Message) Write-LogLine -Level "WARNING" -Message $Message -Color "Yellow" }
function Write-ErrorMessage { param([string]$Message) Write-LogLine -Level "ERROR" -Message $Message -Color "Red" }

$HomePath = if ($HOME) { $HOME } elseif ($env:USERPROFILE) { $env:USERPROFILE } else { "" }
if (-not $HomePath) {
    Write-ErrorMessage "HOME environment variable is not set. Cannot determine installation directory."
    exit 1
}

$Architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
switch ($Architecture) {
    "X64" { $ArchName = "amd64" }
    "Arm64" { $ArchName = "arm64" }
    "Arm" { $ArchName = "arm" }
    "X86" { $ArchName = "386" }
    default {
        Write-ErrorMessage "Unsupported architecture: $Architecture"
        Write-Info "Supported architectures: X64/amd64, Arm64/arm64, Arm/arm, X86/386"
        exit 1
    }
}

$OSDescription = [System.Runtime.InteropServices.RuntimeInformation]::OSDescription
if ($IsLinux) {
    if ($env:ANDROID_ROOT) {
        $OSName = "android"
    } else {
        $OSName = "linux"
    }
} elseif ($IsMacOS) {
    $OSName = "darwin"
} elseif ($IsWindows) {
    $OSName = "windows"
} elseif ($OSDescription -match "FreeBSD") {
    $OSName = "freebsd"
} else {
    Write-ErrorMessage "Unsupported operating system: $OSDescription"
    Write-Info "Supported operating systems: Linux, macOS (Darwin), FreeBSD, Windows, Android (Termux)"
    exit 1
}

$Platform = "$OSName-$ArchName"
$BinaryName = if ($OSName -eq "windows") { "gh-aw.exe" } else { "gh-aw" }

Write-Info "Detected OS: $OSDescription -> $OSName"
Write-Info "Detected architecture: $Architecture -> $ArchName"
Write-Info "Platform: $Platform"

$Repo = "github/gh-aw"
if (-not $Version) {
    Write-Info "No version specified, using 'latest'..."
    $Version = "latest"
} else {
    Write-Info "Using specified version: $Version"
}

$GitHubHeaders = @{
    Accept                 = "application/vnd.github+json"
    "User-Agent"           = "gh-aw-install-gh-aw.ps1"
    "X-GitHub-Api-Version" = "2022-11-28"
}
if ($env:GH_TOKEN) {
    $GitHubHeaders.Authorization = ("Bearer " + $env:GH_TOKEN)
}

function Compare-SemVerTags {
    param(
        [Parameter(Mandatory)][string]$Candidate,
        [Parameter(Mandatory)][string]$Current
    )

    $candidateParts = $Candidate.Substring(1).Split(".")
    $currentParts = $Current.Substring(1).Split(".")
    $candidateParts[2] = $candidateParts[2].Split("+")[0]
    $currentParts[2] = $currentParts[2].Split("+")[0]
    for ($index = 0; $index -lt 3; $index++) {
        if ($candidateParts[$index].Length -gt $currentParts[$index].Length) {
            return 1
        }
        if ($candidateParts[$index].Length -lt $currentParts[$index].Length) {
            return -1
        }

        $comparison = [string]::CompareOrdinal($candidateParts[$index], $currentParts[$index])
        if ($comparison -gt 0) {
            return 1
        }
        if ($comparison -lt 0) {
            return -1
        }
    }

    return 0
}

function Resolve-MajorVersionChannel {
    param([Parameter(Mandatory)][string]$Major)

    $bestTag = ""
    $page = 1
    $perPage = 100
    do {
        $releasesUrl = "https://api.github.com/repos/$Repo/releases?per_page=$perPage&page=$page"
        try {
            $releases = @(Invoke-RestMethod -Uri $releasesUrl -Headers $GitHubHeaders -TimeoutSec 30)
        } catch {
            Write-ErrorMessage "Failed to resolve stable releases for version channel v$Major from GitHub API."
            return ""
        }

        foreach ($release in $releases) {
            if ($release.draft -or $release.prerelease) {
                continue
            }

            $tag = [string]$release.tag_name
            if ($tag -cmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$' -and $Matches[1] -ceq $Major) {
                if (-not $bestTag -or (Compare-SemVerTags -Candidate $tag -Current $bestTag) -gt 0) {
                    $bestTag = $tag
                }
            }
        }

        $releaseCount = $releases.Count
        $page++
    } while ($releaseCount -ge $perPage)

    return $bestTag
}

if ($Version -cmatch '^v(0|[1-9][0-9]*)$') {
    $channelMajor = $Matches[1]
    $resolvedTag = Resolve-MajorVersionChannel -Major $channelMajor
    if (-not $resolvedTag) {
        Write-ErrorMessage "No stable release found for version channel $Version."
        exit 1
    }

    Write-Info "Resolved version channel $Version to $resolvedTag"
    $Version = $resolvedTag
}

function Invoke-ProcessWithTimeout {
    param(
        [Parameter(Mandatory)][string]$FilePath,
        [string[]]$ArgumentList = @(),
        [int]$TimeoutSeconds = 30
    )

    $stdoutPath = [System.IO.Path]::GetTempFileName()
    $stderrPath = [System.IO.Path]::GetTempFileName()

    try {
        $process = Start-Process -FilePath $FilePath -ArgumentList $ArgumentList -PassThru -NoNewWindow -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
        if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
            try {
                $process.Kill($true)
                $process.WaitForExit(5000) | Out-Null
            } catch {
            }
            $stdout = (Get-Content $stdoutPath -Raw -ErrorAction SilentlyContinue) ?? ''
            $stderr = (Get-Content $stderrPath -Raw -ErrorAction SilentlyContinue) ?? ''
            return [pscustomobject]@{
                ExitCode = 124
                TimedOut = $true
                Stdout   = $stdout
                Stderr   = $stderr
            }
        }

        $stdout = (Get-Content $stdoutPath -Raw -ErrorAction SilentlyContinue) ?? ''
        $stderr = (Get-Content $stderrPath -Raw -ErrorAction SilentlyContinue) ?? ''
        return [pscustomobject]@{
            ExitCode = $process.ExitCode
            TimedOut = $false
            Stdout   = $stdout
            Stderr   = $stderr
        }
    } finally {
        Remove-Item $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
    }
}

function Invoke-DownloadWithRetry {
    param(
        [Parameter(Mandatory)][string]$Url,
        [Parameter(Mandatory)][string]$DestinationPath,
        [int]$MaxRetries = 3,
        [int]$InitialDelaySeconds = 2
    )

    $delay = $InitialDelaySeconds
    for ($attempt = 1; $attempt -le $MaxRetries; $attempt++) {
        try {
            Invoke-WebRequest -Uri $Url -Headers $GitHubHeaders -OutFile $DestinationPath -TimeoutSec 120 | Out-Null
            Write-Success "Binary downloaded successfully"
            return $true
        } catch {
            if ($attempt -ge $MaxRetries) {
                break
            }

            Write-WarningMessage "Download attempt $attempt failed. Retrying in ${delay}s..."
            Start-Sleep -Seconds $delay
            $delay *= 2
        }
    }

    return $false
}

function Get-RequestedOrInstalledVersion {
    param([Parameter(Mandatory)][string]$VersionOutput)

    $match = [regex]::Match($VersionOutput, "v[0-9]+\.[0-9]+\.[0-9]+")
    if ($match.Success) {
        return $match.Value
    }

    return ""
}

if ($TryGhInstall -and (Get-Command gh -ErrorAction SilentlyContinue)) {
    Write-Info "Attempting to install gh-aw using 'gh extension install'..."

    $installArgs = @("extension", "install", $Repo, "--force")
    if ($Version -and $Version -ne "latest") {
        $installArgs += @("--pin", $Version)
    }

    $ghInstallTimeoutSeconds = if ($OSName -eq "windows") { 90 } else { 300 }
    if ($OSName -eq "windows") {
        Write-Info "Windows detected: wrapping gh extension install with a 90s timeout"
    }

    $ghInstallResult = Invoke-ProcessWithTimeout -FilePath "gh" -ArgumentList $installArgs -TimeoutSeconds $ghInstallTimeoutSeconds
    $combinedGhInstallOutput = ($ghInstallResult.Stdout + $ghInstallResult.Stderr).Trim()

    if ($ghInstallResult.ExitCode -eq 124) {
        Write-WarningMessage "gh extension install timed out (${ghInstallTimeoutSeconds}s) — falling back to manual installation."
        Write-WarningMessage "This is a known issue on Windows where Defender scans the new binary."
    } elseif ($ghInstallResult.ExitCode -eq 0) {
        $verifyGhResult = Invoke-ProcessWithTimeout -FilePath "gh" -ArgumentList @("aw", "version") -TimeoutSeconds 30
        if ($verifyGhResult.ExitCode -eq 0) {
            $installedVersion = Get-RequestedOrInstalledVersion -VersionOutput ($verifyGhResult.Stdout + $verifyGhResult.Stderr)
            if (-not $installedVersion) {
                Write-WarningMessage "gh extension install completed but the installed gh-aw version could not be determined"
                Write-Info "Falling back to manual installation..."
            } elseif ($Version -ne "latest" -and $installedVersion -ne ($Version -replace '\+.*$', '')) {
                Write-WarningMessage "Version mismatch: requested $Version but gh extension install installed $installedVersion"
                Write-Info "Falling back to manual installation to install the correct version..."
            } else {
                Write-Success "Successfully installed gh-aw using gh extension install"
                Write-Info "Installed version: $installedVersion"
                if ($env:GITHUB_OUTPUT) {
                    "installed_version=$installedVersion" | Add-Content -Path $env:GITHUB_OUTPUT
                }
                exit 0
            }
        } else {
            Write-WarningMessage "gh extension install completed but verification failed"
            Write-Info "Falling back to manual installation..."
        }
    } else {
        Write-WarningMessage "gh extension install failed, falling back to manual installation..."
        if ($combinedGhInstallOutput) {
            Write-Host $combinedGhInstallOutput
        }
    }
} elseif ($TryGhInstall) {
    Write-Info "gh CLI not available, proceeding with manual installation..."
}

if ($Version -eq "latest") {
    $DownloadURL = "https://github.com/$Repo/releases/latest/download/$Platform"
    $ChecksumsURL = "https://github.com/$Repo/releases/latest/download/checksums.txt"
} else {
    $DownloadURL = "https://github.com/$Repo/releases/download/$Version/$Platform"
    $ChecksumsURL = "https://github.com/$Repo/releases/download/$Version/checksums.txt"
}
if ($OSName -eq "windows") {
    $DownloadURL = "$DownloadURL.exe"
}

$LatestTag = ""
$FallbackDownloadURL = ""
$FallbackChecksumsURL = ""
if ($Version -eq "latest") {
    try {
        $latestRelease = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $GitHubHeaders -TimeoutSec 30
        $LatestTag = $latestRelease.tag_name
    } catch {
        Write-WarningMessage "Failed to resolve latest release tag from GitHub API."
    }

    if ($LatestTag) {
        $FallbackDownloadURL = "https://github.com/$Repo/releases/download/$LatestTag/$Platform"
        $FallbackChecksumsURL = "https://github.com/$Repo/releases/download/$LatestTag/checksums.txt"
        if ($OSName -eq "windows") {
            $FallbackDownloadURL = "$FallbackDownloadURL.exe"
        }
        Write-Info "Prepared latest fallback URL for resolved tag: $LatestTag"
    } else {
        Write-WarningMessage "Could not resolve latest release tag; install will use latest redirect URL only."
    }
}

$InstallDir = [System.IO.Path]::Combine($HomePath, ".local", "share", "gh", "extensions", "gh-aw")
$BinaryPath = Join-Path $InstallDir $BinaryName
$ChecksumsPath = Join-Path $InstallDir "checksums.txt"

Write-Info "Download URL: $DownloadURL"
Write-Info "Installation directory: $InstallDir"

if (-not (Test-Path $InstallDir)) {
    Write-Info "Creating installation directory..."
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

if (Test-Path $BinaryPath) {
    Write-WarningMessage "Binary '$BinaryPath' already exists. It will be overwritten."
}

Write-Info "Downloading gh-aw binary..."
if (-not (Invoke-DownloadWithRetry -Url $DownloadURL -DestinationPath $BinaryPath)) {
    if ($FallbackDownloadURL) {
        Write-WarningMessage "Failed to download from latest redirect URL. Retrying with resolved tag URL ($LatestTag)."
        $DownloadURL = $FallbackDownloadURL
        $ChecksumsURL = $FallbackChecksumsURL
        if (-not (Invoke-DownloadWithRetry -Url $DownloadURL -DestinationPath $BinaryPath)) {
            Write-ErrorMessage "Failed to download binary from $DownloadURL after 3 attempts"
            Write-Info "Please check if the version and platform combination exists in the releases."
            exit 1
        }
    } else {
        Write-ErrorMessage "Failed to download binary from $DownloadURL after 3 attempts"
        Write-Info "Please check if the version and platform combination exists in the releases."
        exit 1
    }
}

if (-not $SkipChecksum) {
    Write-Info "Downloading checksums file..."
    $checksumsDownloaded = $false

    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            Invoke-WebRequest -Uri $ChecksumsURL -Headers $GitHubHeaders -OutFile $ChecksumsPath -TimeoutSec 60 | Out-Null
            $checksumsDownloaded = $true
            Write-Success "Checksums file downloaded successfully"
            break
        } catch {
            if ($attempt -eq 3) {
                Write-WarningMessage "Failed to download checksums file after 3 attempts"
                Write-WarningMessage "Checksum verification will be skipped for this version."
                Write-Info "This may occur for older releases that don't include checksums."
            } else {
                Write-WarningMessage "Checksum download attempt $attempt failed. Retrying in 2s..."
                Start-Sleep -Seconds 2
            }
        }
    }

    if ($checksumsDownloaded) {
        Write-Info "Verifying binary checksum..."

        $expectedFilename = if ($OSName -eq "windows") { "$Platform.exe" } else { $Platform }
        $expectedChecksum = ""
        foreach ($line in Get-Content $ChecksumsPath) {
            $parts = $line -split "\s+", 2
            if ($parts.Count -eq 2 -and $parts[1].Trim() -eq $expectedFilename) {
                $expectedChecksum = $parts[0].Trim()
                break
            }
        }

        if (-not $expectedChecksum) {
            Write-WarningMessage "Checksum for $expectedFilename not found in checksums file"
            Write-WarningMessage "Checksum verification will be skipped."
        } else {
            $actualChecksum = (Get-FileHash -Path $BinaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
            $expectedChecksum = $expectedChecksum.ToLowerInvariant()

            if ($actualChecksum -eq $expectedChecksum) {
                Write-Success "Checksum verification passed!"
                Write-Info "Expected: $expectedChecksum"
                Write-Info "Actual:   $actualChecksum"
            } else {
                Write-ErrorMessage "Checksum verification failed!"
                Write-ErrorMessage "Expected: $expectedChecksum"
                Write-ErrorMessage "Actual:   $actualChecksum"
                Write-ErrorMessage "The downloaded binary may be corrupted or tampered with."
                Write-Info "To skip checksum verification, use: ./install-gh-aw.ps1 $Version --skip-checksum"
                Remove-Item $BinaryPath -Force -ErrorAction SilentlyContinue
                exit 1
            }
        }

        Remove-Item $ChecksumsPath -Force -ErrorAction SilentlyContinue
    }
} else {
    Write-WarningMessage "Checksum verification skipped (--skip-checksum flag used)"
}

if ($OSName -ne "windows") {
    Write-Info "Making binary executable..."
    & chmod +x -- $BinaryPath
}

$BinaryExecTimeoutSeconds = 30
if ($OSName -eq "windows") {
    Write-Info "Windows detected: wrapping binary verification with a 30s timeout"
}

Write-Info "Verifying binary..."
$helpResult = Invoke-ProcessWithTimeout -FilePath $BinaryPath -ArgumentList @("--help") -TimeoutSeconds $BinaryExecTimeoutSeconds
if ($helpResult.ExitCode -eq 0) {
    Write-Success "Binary is working correctly!"
} elseif ($OSName -eq "windows" -and $helpResult.ExitCode -eq 124) {
    Write-WarningMessage "Binary verification timed out — Windows Defender may still be scanning the binary."
    Write-WarningMessage "Installation is complete. Verify manually with: '$BinaryPath' --help"
} else {
    Write-ErrorMessage "Binary verification failed. The downloaded file may be corrupted or incompatible."
    exit 1
}

$fileSize = (Get-Item $BinaryPath).Length
Write-Success "Installation complete!"
Write-Info "Binary location: $BinaryPath"
Write-Info "Binary size: $fileSize bytes"
Write-Info "Version: $Version"

Write-Info ""
Write-Info "You can now use gh-aw with the gh CLI:"
Write-Info "  gh aw --help"
Write-Info "  gh aw version"

Write-Info ""
Write-Info "Running gh-aw version check..."
$versionCheckResult = Invoke-ProcessWithTimeout -FilePath $BinaryPath -ArgumentList @("version") -TimeoutSeconds $BinaryExecTimeoutSeconds
if ($versionCheckResult.ExitCode -eq 124 -and $OSName -eq "windows") {
    Write-WarningMessage "Version check timed out (Windows Defender may still be scanning the binary)."
} elseif ($versionCheckResult.Stdout) {
    Write-Host $versionCheckResult.Stdout.TrimEnd()
} elseif ($versionCheckResult.Stderr) {
    Write-Host $versionCheckResult.Stderr.TrimEnd()
}

if ($env:GITHUB_OUTPUT) {
    "installed_version=$Version" | Add-Content -Path $env:GITHUB_OUTPUT
}
