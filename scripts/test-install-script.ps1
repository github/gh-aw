param(
    [switch]$Inner,
    [string]$CaseName,
    [string]$InstallerPath
)

if (-not $Inner) {
    $scriptPath = $PSCommandPath
    $installer = if ($InstallerPath) { $InstallerPath } else { Join-Path (Split-Path $PSScriptRoot -Parent) "install-gh-aw.ps1" }
    $testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("gh-aw-powershell-installer-test-" + [guid]::NewGuid().ToString("N"))
    $homePath = Join-Path $testRoot "home"
    $installDir = Join-Path $homePath ".local/share/gh/extensions/gh-aw"
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null

    $oldHome = $env:HOME
    $oldUserProfile = $env:USERPROFILE
    try {
        foreach ($testCase in @("channel-page-and-semver", "channel-no-v1-release", "exact-tag", "default-latest")) {
            if ($testCase -eq "channel-no-v1-release") {
                $binaryName = if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)) { "gh-aw.exe" } else { "gh-aw" }
                $existingBinary = Join-Path $installDir $binaryName
                Set-Content -Path $existingBinary -Value "existing binary" -NoNewline
                $existingBytes = [System.IO.File]::ReadAllBytes($existingBinary)
            }

            $env:HOME = $homePath
            $env:USERPROFILE = $homePath
            $env:TEST_API_LOG = Join-Path $testRoot "$testCase-api.log"
            $env:TEST_PROCESS_LOG = Join-Path $testRoot "$testCase-process.log"
            Remove-Item $env:TEST_API_LOG, $env:TEST_PROCESS_LOG -Force -ErrorAction SilentlyContinue
            $arguments = @("-NoProfile", "-File", $scriptPath, "-Inner", "-CaseName", $testCase, "-InstallerPath", $installer)
            $output = & pwsh @arguments 2>&1 | Out-String

            switch ($testCase) {
                "channel-page-and-semver" {
                    if ($output -notmatch "Resolved version channel v0 to v0\.10\.0") { throw "PowerShell channel selection did not choose v0.10.0`n$output" }
                    if ($output -notmatch "Download URL: https://github\.com/github/gh-aw/releases/download/v0\.10\.0/") { throw "PowerShell download did not use the resolved tag`n$output" }
                    if ($output -notmatch "Failed to download binary") { throw "PowerShell test did not reach the mocked download`n$output" }
                    $apiCalls = Get-Content $env:TEST_API_LOG
                    if ($apiCalls.Count -ne 2) { throw "PowerShell channel resolution did not paginate all matching releases" }
                    if ((Get-Content $env:TEST_PROCESS_LOG -Raw) -notmatch "--pin v0\.10\.0") { throw "PowerShell gh install did not use the resolved tag" }
                }
                "channel-no-v1-release" {
                    if ($output -notmatch "No stable release found for version channel v1") { throw "PowerShell missing-channel error was not clear`n$output" }
                    if ($output -match "Download URL:") { throw "PowerShell proceeded to download without a matching stable release" }
                    $actualBytes = [System.IO.File]::ReadAllBytes($existingBinary)
                    if ([Convert]::ToBase64String($actualBytes) -ne [Convert]::ToBase64String($existingBytes)) { throw "PowerShell missing-channel failure changed the existing binary" }
                    if (Test-Path $env:TEST_PROCESS_LOG) { throw "PowerShell gh install ran before resolving the missing channel" }
                }
                "exact-tag" {
                    if ($output -notmatch "Download URL: https://github\.com/github/gh-aw/releases/download/v0\.37\.18/") { throw "PowerShell exact tag was not used for download`n$output" }
                    if ($output -notmatch "Failed to download binary") { throw "PowerShell exact-tag test did not reach the mocked download`n$output" }
                    if (Test-Path $env:TEST_API_LOG) { throw "PowerShell queried the Releases API for an exact tag" }
                    if ((Get-Content $env:TEST_PROCESS_LOG -Raw) -notmatch "--pin v0\.37\.18") { throw "PowerShell gh install did not use the exact tag" }
                }
                "default-latest" {
                    if ($output -notmatch "No version specified, using 'latest'") { throw "PowerShell omitted-version default was not latest`n$output" }
                    if ($output -notmatch "Download URL: https://github\.com/github/gh-aw/releases/latest/download/") { throw "PowerShell latest download behavior changed`n$output" }
                    if ($output -notmatch "Failed to download binary") { throw "PowerShell latest test did not reach the mocked download`n$output" }
                }
            }

            Write-Host "PowerShell installer test passed: $testCase"
            Remove-Item $env:TEST_API_LOG -Force -ErrorAction SilentlyContinue
        }
    } finally {
        $env:HOME = $oldHome
        $env:USERPROFILE = $oldUserProfile
        Remove-Item $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
    exit 0
}

$global:TestCaseName = $CaseName
$global:PageOne = [System.Collections.Generic.List[object]]::new()
if ($CaseName -eq "channel-page-and-semver") {
    for ($patch = 0; $patch -lt 97; $patch++) {
        $global:PageOne.Add([pscustomobject]@{ tag_name = "v1.0.$patch"; prerelease = $false })
    }
    $global:PageOne.Add([pscustomobject]@{ tag_name = "v0.9.0"; prerelease = $false })
    $global:PageOne.Add([pscustomobject]@{ tag_name = "v0.99.0-rc.1"; prerelease = $true })
    $global:PageOne.Add([pscustomobject]@{ tag_name = "v1.99.0"; prerelease = $false })
    $global:PageTwo = @([pscustomobject]@{ tag_name = "v0.10.0"; prerelease = $false })
} elseif ($CaseName -eq "channel-no-v1-release") {
    $global:PageOne.Add([pscustomobject]@{ tag_name = "v0.37.18"; prerelease = $false })
    $global:PageOne.Add([pscustomobject]@{ tag_name = "v1.0.0-rc.1"; prerelease = $true })
    $global:PageOne.Add([pscustomobject]@{ tag_name = "v2.0.0"; prerelease = $false })
}

function Invoke-RestMethod {
    param([string]$Uri, [hashtable]$Headers, [int]$TimeoutSec)

    Add-Content -Path $env:TEST_API_LOG -Value $Uri
    if ($Uri.EndsWith("page=1")) {
        return $global:PageOne.ToArray()
    }
    if ($Uri.EndsWith("page=2") -and $global:TestCaseName -eq "channel-page-and-semver") {
        return $global:PageTwo
    }
    if ($Uri.Contains("per_page=100")) {
        return @()
    }
    if ($Uri -match "/releases/latest$") {
        return [pscustomobject]@{ tag_name = "v0.37.18" }
    }
    throw "Unexpected Releases API request: $Uri"
}

function Invoke-WebRequest {
    param([string]$Uri, [hashtable]$Headers, [string]$OutFile, [int]$TimeoutSec)

    throw "Mock download: $Uri"
}

function Start-Sleep {
    param([int]$Seconds)
}

function gh {}

function Start-Process {
    param(
        [string]$FilePath,
        [string[]]$ArgumentList,
        [switch]$PassThru,
        [switch]$NoNewWindow,
        [string]$RedirectStandardOutput,
        [string]$RedirectStandardError
    )

    Add-Content -Path $env:TEST_PROCESS_LOG -Value "$FilePath $($ArgumentList -join ' ')"
    $process = [pscustomobject]@{ ExitCode = 1 }
    $process | Add-Member -MemberType ScriptMethod -Name WaitForExit -Value { return $true }
    return $process
}

Remove-Item Env:INPUT_VERSION -ErrorAction SilentlyContinue
switch ($CaseName) {
    "channel-page-and-semver" { $env:INPUT_VERSION = "v0"; & $InstallerPath }
    "channel-no-v1-release" { $env:INPUT_VERSION = "v1"; & $InstallerPath }
    "exact-tag" { $env:INPUT_VERSION = "v0.37.18"; & $InstallerPath }
    "default-latest" { & $InstallerPath }
    default { throw "Unknown PowerShell installer test case: $CaseName" }
}
