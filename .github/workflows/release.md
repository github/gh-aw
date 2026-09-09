---
private: true
emoji: "🚀"
name: Release
description: Build, test, and release gh-aw extension, then generate and prepend release highlights
on:
  roles:
    - admin
    - maintainer
  workflow_dispatch:
    inputs:
      release_type:
        description: 'Release type (patch, minor, or major)'
        required: true
        type: choice
        default: patch
        options:
          - patch
          - minor
          - major
permissions:
  contents: read
  pull-requests: read
  actions: read
  issues: read


engine: copilot
timeout-minutes: 20
network:
  allowed:
    - defaults
    - node
    - "github.github.com"
safe-outputs:
  update-release:
  threat-detection: false
imports:
  - shared/community-attribution.md
  - shared/otlp.md
jobs:
  validate_container_pins:
    needs: ["pre_activation", "activation"]
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          persist-credentials: false
      - name: Validate container SHA pins in actions-lock.json files
        run: |
          echo "Validating container SHA pins in actions-lock.json files..."
          LOCK_FILES=()
          while IFS= read -r -d '' f; do
            LOCK_FILES+=("$f")
          done < <(find . -name "actions-lock.json" -not -path "*/node_modules/*" -print0)

          if [ ${#LOCK_FILES[@]} -eq 0 ]; then
            echo "❌ No actions-lock.json files found in the repository."
            echo "   Run 'gh aw compile' to generate lock files with resolved container SHA pins."
            exit 1
          fi

          FAILED=0
          for lock_file in "${LOCK_FILES[@]}"; do
            echo "Checking: $lock_file"
            container_count=$(jq '.containers | length // 0' "$lock_file")
            echo "  Found $container_count container entries"

            if [ "$container_count" -eq 0 ]; then
              echo "  ✓ No containers to validate"
              continue
            fi

            missing_pins=$(jq -r '
              .containers // {} |
              to_entries[] |
              select(
                ((.value.digest // "") | test("^sha256:[a-f0-9]{64}$") | not) or
                ((.value.pinned_image // "") | test("@sha256:[a-f0-9]{64}$") | not)
              ) |
              .key
            ' "$lock_file")

            if [ -n "$missing_pins" ]; then
              echo "  ❌ Missing or invalid SHA pins for:"
              echo "$missing_pins" | while read -r image; do
                echo "    - $image"
              done
              FAILED=1
            else
              echo "  ✓ All $container_count containers have valid SHA pins"
            fi
          done

          if [ "$FAILED" -eq 1 ]; then
            echo ""
            echo "❌ Validation failed: Some container images are missing SHA pins."
            echo "   Run 'gh aw compile' to resolve and cache container SHA pins before releasing."
            exit 1
          fi

          echo ""
          echo "✓ All container SHA pins are resolved and cached in actions-lock.json files."

  config:
    needs: ["pre_activation", "activation", "validate_container_pins"]
    runs-on: ubuntu-latest
    outputs:
      release_tag: ${{ steps.compute_config.outputs.release_tag }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: false
      - name: Compute Release Config
        id: compute_config
        uses: actions/github-script@v9.0.0
        with:
          script: |
            const releaseType = context.payload.inputs.release_type;
            
            console.log(`Computing next version for release type: ${releaseType}`);
            
            // Get all releases and sort by semver to find the actual latest version
            const { data: releases } = await github.rest.repos.listReleases({
              owner: context.repo.owner,
              repo: context.repo.repo,
              per_page: 100
            });
            
            // Parse semver and sort releases by version (newest first)
            const parseSemver = (tag) => {
              const match = tag.match(/^v?(\d+)\.(\d+)\.(\d+)/);
              if (!match) return null;
              return {
                tag,
                major: parseInt(match[1], 10),
                minor: parseInt(match[2], 10),
                patch: parseInt(match[3], 10)
              };
            };
            
            const sortedReleases = releases
              .map(r => parseSemver(r.tag_name))
              .filter(v => v !== null)
              .sort((a, b) => {
                if (a.major !== b.major) return b.major - a.major;
                if (a.minor !== b.minor) return b.minor - a.minor;
                return b.patch - a.patch;
              });
            
            if (sortedReleases.length === 0) {
              core.setFailed('No existing releases found. Cannot determine base version for incrementing. Please create an initial release manually (e.g., v0.1.0).');
              return;
            }
            
            const latestTag = sortedReleases[0].tag;
            console.log(`Latest release tag (semver-sorted): ${latestTag}`);
            
            // Parse version components (strip 'v' prefix)
            const version = latestTag.replace(/^v/, '');
            let [major, minor, patch] = version.split('.').map(Number);
            
            // Increment based on release type
            switch (releaseType) {
              case 'major':
                major += 1;
                minor = 0;
                patch = 0;
                break;
              case 'minor':
                minor += 1;
                patch = 0;
                break;
              case 'patch':
                patch += 1;
                break;
            }
            
            // Helper: check whether a given tag already exists (as a release or git ref)
            const tagExists = async (tagName) => {
              const releaseExists = releases.some(r => r.tag_name === tagName);
              if (releaseExists) return true;
              try {
                await github.rest.git.getRef({
                  owner: context.repo.owner,
                  repo: context.repo.repo,
                  ref: `tags/${tagName}`
                });
                return true; // tag ref exists
              } catch (error) {
                if (error.status === 404) return false;
                throw new Error(`Failed to check if tag ${tagName} exists: ${error.message}`);
              }
            };

            // Find the first available tag, bumping the minor/patch if the computed one is taken.
            // This handles the case where a release failed half-way and left a tag behind.
            const MAX_ATTEMPTS = 10;
            let releaseTag;
            for (let attempt = 0; attempt < MAX_ATTEMPTS; attempt++) {
              const candidate = `v${major}.${minor}.${patch}`;
              if (!(await tagExists(candidate))) {
                releaseTag = candidate;
                break;
              }
              console.log(`Tag ${candidate} already exists – bumping version and retrying…`);
              // For patch releases keep bumping the patch number.
              // For minor/major releases bump the minor number (patch is already 0).
              switch (releaseType) {
                case 'patch':
                  patch += 1;
                  break;
                case 'minor':
                  minor += 1;
                  break;
                case 'major':
                  minor += 1;
                  break;
              }
            }

            if (!releaseTag) {
              core.setFailed(`Could not find an available release tag after ${MAX_ATTEMPTS} attempts. Please check existing tags and releases.`);
              return;
            }

            console.log(`Computed release tag: ${releaseTag}`);
            core.setOutput('release_tag', releaseTag);
            console.log(`✓ Release tag: ${releaseTag}`);
  push_tag:
    needs: ["pre_activation", "activation", "config"]
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: true

      - name: Create or update tag
        env:
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
        run: |
          echo "Creating tag: $RELEASE_TAG"
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git tag "$RELEASE_TAG"
          git push origin "$RELEASE_TAG"
          echo "✓ Tag created: $RELEASE_TAG"

      - name: Setup Go
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e  # v7.0.0
        with:
          go-version-file: go.mod
          cache: false  # Disabled for release security - prevent cache poisoning attacks

      - name: Build binaries
        env:
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
        run: |
          echo "Building binaries for release: $RELEASE_TAG"
          bash scripts/build-release.sh "$RELEASE_TAG"
          echo "✓ Binaries built successfully"

      - name: Setup Docker Buildx (pre-validation)
        uses: docker/setup-buildx-action@v4.3.0

      - name: Build Docker image (validation only)
        uses: docker/build-push-action@v7.3.0
        with:
          context: .
          platforms: linux/amd64
          push: false
          load: false
          build-args: |
            BINARY=dist/linux-amd64
          cache-from: type=gha

      - name: Upload release binaries
        uses: actions/upload-artifact@v7.0.1
        with:
          name: release-binaries-${{ needs.config.outputs.release_tag }}
          path: dist/
          retention-days: 1

      - name: Run sync actions and merge PR
        env:
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
        run: |
          {
            echo "## Manual Sync Actions Required"
            echo ""
            echo "The following manual steps must be completed in **github/gh-aw-actions** before this release continues:"
            echo ""
            echo "1. Trigger the **sync-actions** workflow in github/gh-aw-actions:"
            echo "   https://github.com/github/gh-aw-actions/actions/workflows/sync-actions.yml"
            echo "2. Merge the PR created by the sync-actions workflow in **github/gh-aw-actions**"
            echo "3. Verify that tag **\`${RELEASE_TAG}\`** exists in github/gh-aw-actions"
            echo ""
            echo "Once the above steps are complete, approve the **gh-aw-actions-release** environment gate to continue the release."
          } >> "$GITHUB_STEP_SUMMARY"

          echo "Sync actions instructions written for release: $RELEASE_TAG"
          echo "Ensure the sync-actions job has been run and the PR merged in github/gh-aw-actions before approving."

  defender:
    needs: ["pre_activation", "activation", "config", "push_tag"]
    runs-on: windows-latest
    steps:
      - name: Download release binaries
        uses: actions/download-artifact@v8.0.1
        with:
          name: release-binaries-${{ needs.config.outputs.release_tag }}
          path: dist/

      - name: Scan Windows binaries with Microsoft Defender
        shell: pwsh
        run: |
          $binaries = Get-ChildItem -Path dist\ -Filter "windows-*.exe" -File
          if ($binaries.Count -eq 0) {
            Write-Error "No Windows binaries found in dist/"
            exit 1
          }
          Write-Host "Found $($binaries.Count) Windows binaries to scan."

          # Verify every Windows binary against the release checksum manifest
          # before asking Defender to inspect it.
          $checksumPath = Join-Path "dist" "checksums.txt"
          if (-not (Test-Path -LiteralPath $checksumPath -PathType Leaf)) {
            Write-Error "Release checksum manifest not found: $checksumPath"
            exit 1
          }
          $expectedHashes = @{}
          foreach ($line in Get-Content -LiteralPath $checksumPath) {
            if ($line -match '^([0-9a-fA-F]{64})\s+\*?(.+?)\s*$') {
              $expectedHashes[[IO.Path]::GetFileName($Matches[2])] = $Matches[1].ToUpperInvariant()
            }
          }
          foreach ($binary in $binaries) {
            $expectedHash = $expectedHashes[$binary.Name]
            if (-not $expectedHash) {
              Write-Error "No checksum entry found for $($binary.Name)"
              exit 1
            }
            $actualHash = (Get-FileHash -LiteralPath $binary.FullName -Algorithm SHA256).Hash.ToUpperInvariant()
            if ($actualHash -ne $expectedHash) {
              Write-Error "Release checksum mismatch for $($binary.Name): expected $expectedHash, got $actualHash"
              exit 1
            }
            Write-Host "Verified release checksum for $($binary.Name): $actualHash"
          }

          # Resolve MpCmdRun.exe path with fallback to ProgramFiles(x86).
          $mpCmdRun = Join-Path $env:ProgramFiles "Windows Defender\MpCmdRun.exe"
          if (-not (Test-Path $mpCmdRun)) {
            $programFilesX86 = (Get-Item -Path "Env:ProgramFiles(x86)" -ErrorAction SilentlyContinue).Value
            if ($programFilesX86) {
              $mpCmdRun = Join-Path $programFilesX86 "Windows Defender\MpCmdRun.exe"
            }
          }
          if (-not (Test-Path $mpCmdRun)) {
            Write-Error "Microsoft Defender CLI not found (MpCmdRun.exe)"
            exit 1
          }

          # Update Defender signatures before scanning.
          $signatureUpdateAttempts = 3
          $signatureUpdateDelaySeconds = 15
          $signatureUpdateSucceeded = $false
          $signatureUpdateExitCode = 1
          $mpCmdRunLogPaths = @(
            (Join-Path $env:TEMP "MpCmdRun.log"),
            (Join-Path $env:LOCALAPPDATA "Temp\MpCmdRun.log")
          ) | Select-Object -Unique
          for ($attemptNumber = 1; $attemptNumber -le $signatureUpdateAttempts; $attemptNumber++) {
            Write-Host "Updating Microsoft Defender signatures (attempt $attemptNumber/$signatureUpdateAttempts)..."
            & $mpCmdRun -SignatureUpdate
            $signatureUpdateExitCode = $LASTEXITCODE
            if ($signatureUpdateExitCode -eq 0) {
              $signatureUpdateSucceeded = $true
              break
            }

            Write-Warning "Defender signature update attempt $attemptNumber failed with exit code $signatureUpdateExitCode"
            foreach ($mpCmdRunLogPath in $mpCmdRunLogPaths) {
              if (Test-Path $mpCmdRunLogPath) {
                Write-Host "=== Tail of $mpCmdRunLogPath after attempt $attemptNumber ==="
                Get-Content -Path $mpCmdRunLogPath -Tail 200
              } else {
                Write-Host "MpCmdRun log file not found at $mpCmdRunLogPath"
              }
            }

            if ($attemptNumber -lt $signatureUpdateAttempts) {
              Write-Host "Retrying Defender signature update in $signatureUpdateDelaySeconds seconds..."
              Start-Sleep -Seconds $signatureUpdateDelaySeconds
            }
          }
          if (-not $signatureUpdateSucceeded) {
            Write-Error "Defender signature update failed after $signatureUpdateAttempts attempts (last exit code: $signatureUpdateExitCode)"
            exit $signatureUpdateExitCode
          }

          # Log Defender status, preference, and execution details for diagnostics.
          Write-Host "=== Microsoft Defender diagnostic info ==="
          try {
            $mpStatus = Get-MpComputerStatus
          } catch {
            $mpStatus = $null
            Write-Host "Could not query Microsoft Defender status: $_"
          }
          if ($mpStatus) {
            Write-Host "AntivirusEnabled:         $($mpStatus.AntivirusEnabled)"
            Write-Host "RealTimeProtectionEnabled: $($mpStatus.RealTimeProtectionEnabled)"
            Write-Host "AntivirusSignatureVersion: $($mpStatus.AntivirusSignatureVersion)"
            Write-Host "AntivirusSignatureLastUpdated: $($mpStatus.AntivirusSignatureLastUpdated)"
            Write-Host "AMProductVersion:          $($mpStatus.AMProductVersion)"
            Write-Host "AMEngineVersion:           $($mpStatus.AMEngineVersion)"
          } else {
            Write-Host "(Get-MpComputerStatus unavailable)"
          }
          try {
            $mpPreference = Get-MpPreference
          } catch {
            $mpPreference = $null
            Write-Host "Could not query Microsoft Defender preferences: $_"
          }
          if ($mpPreference) {
            Write-Host "ExclusionPath:             $(@($mpPreference.ExclusionPath) -join '; ')"
            Write-Host "ExclusionExtension:        $(@($mpPreference.ExclusionExtension) -join '; ')"
            Write-Host "ExclusionProcess:          $(@($mpPreference.ExclusionProcess) -join '; ')"
            Write-Host "ExclusionIpAddress:        $(@($mpPreference.ExclusionIpAddress) -join '; ')"
          } else {
            Write-Host "(Get-MpPreference unavailable)"
          }
          $winDefendService = Get-Service -Name WinDefend -ErrorAction SilentlyContinue
          if ($winDefendService) {
            Write-Host "WinDefend service status:  $($winDefendService.Status)"
            Write-Host "WinDefend service start:   $($winDefendService.StartType)"
          } else {
            Write-Host "(WinDefend service unavailable)"
          }
          if ($mpStatus) {
            Write-Host "AMRunningMode:             $($mpStatus.AMRunningMode)"
            Write-Host "IoavProtectionEnabled:     $($mpStatus.IoavProtectionEnabled)"
          }
          Write-Host "MpCmdRun.exe path: $mpCmdRun"
          Write-Host "=========================================="

          $scanBaseRoot = $env:TEMP
          if (-not $scanBaseRoot) {
            $scanBaseRoot = "C:\Temp"
          }
          $scanBasePath = Join-Path $scanBaseRoot "defender-scan"
          New-Item -ItemType Directory -Path $scanBasePath -Force | Out-Null

          $workspaceRoot = $null
          if ($env:GITHUB_WORKSPACE -and (Test-Path -Path $env:GITHUB_WORKSPACE -PathType Container)) {
            $workspaceRoot = (Resolve-Path -Path $env:GITHUB_WORKSPACE).Path
          }

          $failed = $false
          foreach ($binary in $binaries) {
            $workspaceBinaryPath = (Resolve-Path -Path $binary.FullName).Path

            # Stabilize file before scanning to avoid transient races.
            $stabilizationDelaySeconds = 3
            $initialBinaryItem = Get-Item -LiteralPath $workspaceBinaryPath
            Start-Sleep -Seconds $stabilizationDelaySeconds
            $stableBinaryItem = Get-Item -LiteralPath $workspaceBinaryPath
            if (
              $initialBinaryItem.Length -ne $stableBinaryItem.Length -or
              $initialBinaryItem.LastWriteTimeUtc -ne $stableBinaryItem.LastWriteTimeUtc
            ) {
              Write-Error "Binary changed during stabilization window (${stabilizationDelaySeconds}s): $workspaceBinaryPath"
              Write-Error "Initial: size=$($initialBinaryItem.Length), lastWriteUtc=$($initialBinaryItem.LastWriteTimeUtc)"
              Write-Error "Current: size=$($stableBinaryItem.Length), lastWriteUtc=$($stableBinaryItem.LastWriteTimeUtc)"
              $failed = $true
              continue
            }
            $workspaceBinaryHash = (Get-FileHash -LiteralPath $workspaceBinaryPath -Algorithm SHA256).Hash

            # Copy to a dedicated scan directory instead of scanning directly under D:\a\ workspace.
            $scanFileName = "scan-$([guid]::NewGuid().ToString('N')).exe"
            $binaryPath = Join-Path $scanBasePath $scanFileName
            Copy-Item -LiteralPath $workspaceBinaryPath -Destination $binaryPath -Force
            if (-not (Test-Path -Path $binaryPath -PathType Leaf)) {
              Write-Error "Copied scan target not found: $binaryPath"
              $failed = $true
              continue
            }
            $binaryPath = (Resolve-Path -Path $binaryPath).Path
            if ($workspaceRoot -and $binaryPath.StartsWith($workspaceRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
              Write-Error "Refusing to scan from workspace path: $binaryPath"
              $failed = $true
              continue
            }

            # Validate copied file integrity before scanning.
            $scanBinaryItem = Get-Item -LiteralPath $binaryPath
            $scanBinaryHash = (Get-FileHash -LiteralPath $binaryPath -Algorithm SHA256).Hash
            if ($scanBinaryItem.Length -ne $stableBinaryItem.Length) {
              Write-Error "Copied file size mismatch: source=$($stableBinaryItem.Length), copy=$($scanBinaryItem.Length)"
              $failed = $true
              continue
            }
            if ($scanBinaryHash -ne $workspaceBinaryHash) {
              Write-Error "Copied file hash mismatch: source=$workspaceBinaryHash, copy=$scanBinaryHash"
              $failed = $true
              continue
            }

            Write-Host "Workspace binary:  $workspaceBinaryPath"
            Write-Host "Workspace size:    $($stableBinaryItem.Length) bytes"
            Write-Host "Workspace SHA256:  $workspaceBinaryHash"
            Write-Host "Binary to scan:    $binaryPath"
            Write-Host "Binary size:       $($scanBinaryItem.Length) bytes"
            Write-Host "Binary SHA256:     $scanBinaryHash"

            # ScanType 3 = custom scan. Use -File to scan only the copied binary.
            # Capture output (2>&1 merges stderr into the output stream so all
            # MpCmdRun messages are available for strict failure checks below.
            # Retry on transient service errors (e.g. WinDefend in StopPending state,
            # hr = 0x800106ba) which can occur transiently on Windows runners.
            $scanAttempts = 3
            $scanDelaySeconds = 15
            $output = $null
            $scanExitCode = 0
            $outputText = ""
            for ($scanAttempt = 1; $scanAttempt -le $scanAttempts; $scanAttempt++) {
              if ($scanAttempt -gt 1) {
                Write-Host "Retrying Microsoft Defender scan for $($binary.Name) (attempt $scanAttempt/$scanAttempts)..."
              }
              $output = & $mpCmdRun -Scan -ScanType 3 -File $binaryPath -DisableRemediation 2>&1 | ForEach-Object { "$_" }
              $scanExitCode = $LASTEXITCODE
              $outputText = @($output) -join "`n"
              $isTransientError = $scanExitCode -ne 0 -and $outputText -imatch "0x800106ba"
              if (-not $isTransientError -or $scanAttempt -eq $scanAttempts) {
                break
              }
              Write-Warning "Defender scan failed with a transient service error (attempt $scanAttempt/$scanAttempts). Retrying in $scanDelaySeconds seconds..."
              Start-Sleep -Seconds $scanDelaySeconds
            }

            Write-Host "=== MpCmdRun output ==="
            $output | ForEach-Object { Write-Host $_ }
            Write-Host "Exit code: $scanExitCode"
            Write-Host "======================="

            # Exit code alone is not enough: explicitly parse output to confirm scan execution.
            $skipped = $output | Where-Object { $_ -imatch "\bwas skipped\b|\bcannot be scanned\b|\bnot performed\b|\b(?:file|scan).*\bexcluded\b" }
            $threatLines = $output | Where-Object { $_ -match "\bThreat\b" }
            $scanStarted = $outputText -imatch "\bScan starting\b"
            $scanFinished = $outputText -imatch "\bScan finished\b"
            if ($scanExitCode -ne 0) {
              Write-Error "Microsoft Defender scan failed for $($binary.Name) with exit code $scanExitCode"
              $failed = $true
            } elseif ($skipped) {
              Write-Error "Microsoft Defender scan was skipped for $binaryPath - the binary was NOT scanned."
              Write-Error "Possible causes: file/path exclusion, stale signatures, or protection disabled."
              # Wrap with @() so a single matched line is joined as one line, not character-by-character.
              Write-Error "Skipped output: $(@($skipped) -join '; ')"
              $failed = $true
            } elseif ($threatLines) {
              Write-Error "Microsoft Defender reported threat indicators in scan output for $($binary.Name)."
              Write-Error "Threat output: $(@($threatLines) -join '; ')"
              $failed = $true
            } elseif (-not ($scanStarted -and $scanFinished)) {
              Write-Error "Could not confirm scan completion from MpCmdRun output for $($binary.Name) (expected both 'Scan starting' and 'Scan finished')."
              $failed = $true
            } else {
              Write-Host "✅ Microsoft Defender scan completed successfully for $($binary.Name)"
            }
          }
          if ($failed) { exit 1 }

  sync_actions:
    needs: ["pre_activation", "activation", "config", "push_tag", "defender"]
    runs-on: ubuntu-latest
    environment: gh-aw-actions-release
    steps:
      - name: Await manual approval
        run: echo "Manual approval received. Continuing release."

  release:
    needs: ["pre_activation", "activation", "config", "sync_actions"]
    runs-on: ubuntu-latest
    permissions:
      contents: write
      packages: write
      id-token: write
      attestations: write
    outputs:
      release_id: ${{ steps.get_release.outputs.release_id }}
    steps:
      - name: Verify tag exists in gh-aw-actions
        env:
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          echo "Verifying tag $RELEASE_TAG exists in github/gh-aw-actions..."
          if gh api "repos/github/gh-aw-actions/git/refs/tags/$RELEASE_TAG" --jq '.ref' > /dev/null 2>&1; then
            echo "✓ Tag $RELEASE_TAG exists in github/gh-aw-actions"
          else
            echo "Error: Tag $RELEASE_TAG not found in github/gh-aw-actions after sync"
            exit 1
          fi

      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: true

      - name: Download release binaries
        uses: actions/download-artifact@v8.0.1
        with:
          name: release-binaries-${{ needs.config.outputs.release_tag }}
          path: dist/

      - name: Create GitHub release
        id: get_release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
        run: |
          log_diagnostic() {
            local description="$1"
            shift
            local output
            local status

            echo "::group::${description}"
            set +e
            output=$("$@" 2>&1)
            status=$?
            set -e
            printf '%s\n' "$output"
            echo "Exit status: $status"
            echo "::endgroup::"
          }

          echo "Creating GitHub release: $RELEASE_TAG (repository: $GITHUB_REPOSITORY)"
          echo "::group::Release creation context"
          gh --version
          echo "Commit: $(git rev-parse HEAD)"
          echo "Tag target: $(git rev-list -n 1 "$RELEASE_TAG")"
          echo "Release assets:"
          ls -lh dist/
          sha256sum dist/*
          echo "::endgroup::"
          
          # Create release with binaries (SBOM files will be added later)
          set +e
          release_create_output=$(gh release create "$RELEASE_TAG" \
            dist/* \
            --title "$RELEASE_TAG" \
            --generate-notes \
            --prerelease \
            --latest=false 2>&1)
          release_create_status=$?
          set -e
          printf '%s\n' "$release_create_output"

          if [ "$release_create_status" -ne 0 ]; then
            echo "Error: gh release create failed with exit status $release_create_status"
            log_diagnostic "Release lookup after failed creation" \
              gh release view "$RELEASE_TAG" --json databaseId,tagName,targetCommitish,isDraft,isPrerelease,url
            log_diagnostic "Release API response after failed creation" \
              gh api --include "repos/$GITHUB_REPOSITORY/releases/tags/$RELEASE_TAG"
            log_diagnostic "Tag API response after failed creation" \
              gh api --include "repos/$GITHUB_REPOSITORY/git/ref/tags/$RELEASE_TAG"
            log_diagnostic "GitHub API rate limit after failed creation" \
              gh api rate_limit --jq '.resources.core | {limit, remaining, reset}'
            exit "$release_create_status"
          fi
          
          # Get release ID (retry to handle eventual consistency)
          MAX_ATTEMPTS=5
          RELEASE_ID=""
          for attempt in $(seq 1 "$MAX_ATTEMPTS"); do
            set +e
            release_view_output=$(gh release view "$RELEASE_TAG" --json databaseId --jq '.databaseId' 2>&1)
            release_view_status=$?
            set -e
            if [ "$release_view_status" -eq 0 ] && [ -n "$release_view_output" ]; then
              RELEASE_ID="$release_view_output"
              break
            fi
            if ! echo "$release_view_output" | grep -qiE "not found|404"; then
              echo "Error: Failed to resolve release ID for $RELEASE_TAG"
              echo "$release_view_output"
              exit 1
            fi
            if [ "$attempt" -lt "$MAX_ATTEMPTS" ]; then
              echo "Release ID not available yet (attempt $attempt/$MAX_ATTEMPTS); retrying..."
              sleep $((2 ** attempt))
            fi
          done
          if [ -z "$RELEASE_ID" ]; then
            echo "Error: Failed to resolve release ID for $RELEASE_TAG after $MAX_ATTEMPTS attempts"
            exit 1
          fi
          echo "release_id=$RELEASE_ID" >> "$GITHUB_OUTPUT"
          echo "✓ Release created: $RELEASE_TAG"
          echo "✓ Release ID: $RELEASE_ID"

      - name: Download Go modules
        run: go mod download

      - name: Generate SBOM (SPDX format)
        uses: anchore/sbom-action@v0.24.2
        with:
          artifact-name: sbom.spdx.json
          output-file: sbom.spdx.json
          format: spdx-json

      - name: Generate SBOM (CycloneDX format)
        uses: anchore/sbom-action@v0.24.2
        with:
          artifact-name: sbom.cdx.json
          output-file: sbom.cdx.json
          format: cyclonedx-json

      - name: Audit SBOM files for secrets
        run: |
          echo "Auditing SBOM files for potential secrets..."
          if grep -rE "GITHUB_TOKEN|SECRET|PASSWORD|API_KEY|PRIVATE_KEY" sbom.*.json; then
            echo "Error: Potential secrets found in SBOM files"
            exit 1
          fi
          echo "✓ No secrets detected in SBOM files"

      - name: Upload SBOM artifacts
        uses: actions/upload-artifact@v7.0.1
        with:
          name: sbom-artifacts
          path: |
            sbom.spdx.json
            sbom.cdx.json
          retention-days: 90  # Long retention since SBOMs are not attached to the release

      - name: Setup Docker Buildx
        uses: docker/setup-buildx-action@v4.3.0

      - name: Log in to GitHub Container Registry
        uses: docker/login-action@v4.6.0
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata for Docker
        id: meta
        uses: docker/metadata-action@v6.2.0
        with:
          images: ghcr.io/${{ github.repository }}
          tags: |
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=semver,pattern={{major}}
            type=sha,format=long
            type=raw,value=latest,enable={{is_default_branch}}

      - name: Build and push Docker image (amd64)
        id: build
        uses: docker/build-push-action@v7.3.0
        with:
          context: .
          platforms: linux/amd64
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          build-args: |
            BINARY=dist/linux-amd64
          cache-from: type=gha
          cache-to: type=gha,mode=max
          sbom: true
          provenance: mode=max

  comment_release_prs:
    needs: ["agent", "config", "release"]
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
      issues: write
    steps:
      - name: Comment on pull requests included in release
        uses: actions/github-script@v9.0.0
        env:
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
          RELEASE_ID: ${{ needs.release.outputs.release_id }}
        with:
          script: |
            const owner = context.repo.owner;
            const repo = context.repo.repo;
            const releaseTag = process.env.RELEASE_TAG;
            const releaseId = Number(process.env.RELEASE_ID);

            if (!releaseTag || Number.isNaN(releaseId)) {
              core.setFailed(`Missing or invalid release context: RELEASE_TAG=${releaseTag}, RELEASE_ID=${process.env.RELEASE_ID}`);
              return;
            }

            const { data: currentRelease } = await github.rest.repos.getRelease({
              owner,
              repo,
              release_id: releaseId
            });

            core.info(`Current release: tag=${currentRelease.tag_name}, id=${currentRelease.id}, sha=${currentRelease.target_commitish}, published_at=${currentRelease.published_at}, url=${currentRelease.html_url}`);

            const { data: releases } = await github.rest.repos.listReleases({
              owner,
              repo,
              per_page: 100
            });

            const currentPublishedAt = new Date(currentRelease.published_at);
            const previousRelease = releases.find((release) =>
              release.id !== currentRelease.id &&
              !release.draft &&
              release.published_at &&
              new Date(release.published_at) < currentPublishedAt
            );

            if (!previousRelease) {
              core.info(`No previous release found before ${releaseTag}; skipping PR comments.`);
              return;
            }

            core.info(`Previous release: tag=${previousRelease.tag_name}, id=${previousRelease.id}, sha=${previousRelease.target_commitish}, published_at=${previousRelease.published_at}, url=${previousRelease.html_url}`);

            const query = [
              `repo:${owner}/${repo}`,
              "is:pr",
              "is:merged",
              `merged:${previousRelease.published_at}..${currentRelease.published_at}`
            ].join(" ");

            core.info(`Searching merged PRs with query: ${query}`);

            const mergedPrs = await github.paginate(github.rest.search.issuesAndPullRequests, {
              q: query,
              per_page: 100
            });

            if (mergedPrs.length === 0) {
              core.info(`No merged PRs found between ${previousRelease.tag_name} and ${releaseTag}.`);
              return;
            }

            core.info(`Found ${mergedPrs.length} merged PR(s) between ${previousRelease.tag_name} and ${releaseTag}.`);

            const marker = `<!-- gh-aw-release:${releaseTag} -->`;
            const releaseUrl = currentRelease.html_url;
            const commentBody = [
              "🎉 This pull request is included in a new release.",
              "",
              `Release: [\`${releaseTag}\`](${releaseUrl})`,
              "",
              marker
            ].join("\n");

            core.info(`Comment body to post:\n${commentBody}`);

            const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
            const commentDelayMs = 1000;
            const withRetry = async (fn, label) => {
              const maxAttempts = 3;
              for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
                try {
                  return await fn();
                } catch (error) {
                  if (attempt === maxAttempts) throw error;
                  core.warning(`${label} failed on attempt ${attempt}/${maxAttempts}: ${error.message}`);
                  await sleep(attempt * 1000);
                }
              }
            };

            let commentedCount = 0;
            let skippedCount = 0;

            for (const pr of mergedPrs) {
              const comments = await withRetry(
                () =>
                  github.paginate(github.rest.issues.listComments, {
                    owner,
                    repo,
                    issue_number: pr.number,
                    per_page: 100
                  }),
                `Listing comments for #${pr.number}`
              );

              const alreadyCommented = comments.some((comment) => comment.body?.includes(marker));
              if (alreadyCommented) {
                skippedCount += 1;
                continue;
              }

              if (commentedCount > 0) {
                await sleep(commentDelayMs);
              }

              await withRetry(
                () =>
                  github.rest.issues.createComment({
                    owner,
                    repo,
                    issue_number: pr.number,
                    body: commentBody
                  }),
                `Creating release comment for #${pr.number}`
              );
              core.info(`Commented on PR #${pr.number}: ${pr.title} — ${pr.html_url}`);
              commentedCount += 1;
            }

            core.info(`Release PR commenting complete: commented=${commentedCount}, skipped=${skippedCount}, total=${mergedPrs.length}`);

steps:
  - name: Setup release environment
    env:
      RELEASE_ID: ${{ needs.release.outputs.release_id }}
      RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      mkdir -p /tmp/gh-aw/agent/release-data
      mkdir -p /tmp/gh-aw/agent/community-data
      # Copy community issues from the agent/community-data path (written by community-attribution import step)
      cp /tmp/gh-aw/agent/community-data/community_issues.json /tmp/gh-aw/agent/community-data/community_issues.json 2>/dev/null || echo "[]" > /tmp/gh-aw/agent/community-data/community_issues.json
      
      # Use the release ID and tag from the release job
      echo "Release ID from release job: $RELEASE_ID"
      echo "Release tag from release job: $RELEASE_TAG"
      
      echo "Processing release: $RELEASE_TAG"
      echo "RELEASE_TAG=$RELEASE_TAG" >> "$GITHUB_ENV"
      
      # Get the current release information
      # Use release ID to fetch release data
      gh api "/repos/$GITHUB_REPOSITORY/releases/$RELEASE_ID" > /tmp/gh-aw/agent/release-data/current_release.json
      echo "✓ Fetched current release information"
      
      # Get the previous release to determine the range
      PREV_RELEASE_TAG=$(gh release list --limit 2 --json tagName --jq '.[1].tagName // empty')
      
      if [ -z "$PREV_RELEASE_TAG" ]; then
        echo "No previous release found. This appears to be the first release."
        echo "PREV_RELEASE_TAG=" >> "$GITHUB_ENV"
        touch /tmp/gh-aw/agent/release-data/pull_requests.json
        echo "[]" > /tmp/gh-aw/agent/release-data/pull_requests.json
      else
        echo "Previous release: $PREV_RELEASE_TAG"
        echo "PREV_RELEASE_TAG=$PREV_RELEASE_TAG" >> "$GITHUB_ENV"
        
        # Get commits between releases
        echo "Fetching commits between $PREV_RELEASE_TAG and $RELEASE_TAG..."
        git fetch --unshallow 2>/dev/null || git fetch --depth=1000
        
        # Get all merged PRs between the two releases (include closingIssuesReferences for attribution)
        echo "Fetching pull requests merged between releases..."
        PREV_PUBLISHED_AT=$(gh release view "$PREV_RELEASE_TAG" --json publishedAt --jq .publishedAt)
        CURR_PUBLISHED_AT=$(gh release view "$RELEASE_TAG" --json publishedAt --jq .publishedAt)
        gh pr list \
          --state merged \
          --limit 1000 \
          --json number,title,author,labels,mergedAt,url,body,closingIssuesReferences \
          --jq "[.[] | select(.mergedAt >= \"$PREV_PUBLISHED_AT\" and .mergedAt <= \"$CURR_PUBLISHED_AT\")]" \
          > /tmp/gh-aw/agent/release-data/pull_requests.json
        
        PR_COUNT=$(jq length "/tmp/gh-aw/agent/release-data/pull_requests.json")
        echo "✓ Fetched $PR_COUNT pull requests"
      fi
      
      # Build closing references index from GitHub-native closingIssuesReferences
      # Maps each closed issue number -> list of PR numbers that directly close it
      echo "Building closing references index from GitHub-native PR links..."
      # Use a nested reduce so the outer body always returns the accumulator,
      # even when closingIssuesReferences is empty (avoids jq setting acc to null).
      jq '
        reduce .[] as $pr (
          {};
          reduce ($pr.closingIssuesReferences // [])[] as $issue (
            .;
            ($issue.number | tostring) as $key |
            .[$key] = (.[$key] // []) + [$pr.number]
          )
        )
      ' /tmp/gh-aw/agent/release-data/pull_requests.json \
        > /tmp/gh-aw/agent/release-data/closing_refs_by_issue.json 2>/dev/null \
        || echo "{}" > /tmp/gh-aw/agent/release-data/closing_refs_by_issue.json
      # Also expose to community-data dir so shared attribution strategy can reference it
      cp /tmp/gh-aw/agent/release-data/closing_refs_by_issue.json /tmp/gh-aw/agent/community-data/closing_refs_by_issue.json
      cp /tmp/gh-aw/agent/release-data/pull_requests.json /tmp/gh-aw/agent/community-data/pull_requests.json
      
      DIRECT_CLOSE_COUNT=$(jq 'keys | length' /tmp/gh-aw/agent/release-data/closing_refs_by_issue.json)
      echo "✓ Found $DIRECT_CLOSE_COUNT issues with GitHub-native closing PR references"
      
      # Find community issues closed during this release window (candidates for attribution review)
      if [ -n "$PREV_PUBLISHED_AT" ]; then
        jq --arg prev "$PREV_PUBLISHED_AT" --arg curr "$CURR_PUBLISHED_AT" \
          '[.[] | select(.closedAt != null and .closedAt >= $prev and .closedAt <= $curr)]' \
          /tmp/gh-aw/agent/community-data/community_issues.json \
          > /tmp/gh-aw/agent/release-data/community_issues_closed_in_window.json 2>/dev/null \
          || echo "[]" > /tmp/gh-aw/agent/release-data/community_issues_closed_in_window.json
        
        CLOSED_IN_WINDOW=$(jq length /tmp/gh-aw/agent/release-data/community_issues_closed_in_window.json)
        echo "✓ Found $CLOSED_IN_WINDOW community issues closed in this release window"
      else
        echo "[]" > /tmp/gh-aw/agent/release-data/community_issues_closed_in_window.json
      fi
      
      # Get the CHANGELOG.md content around this version
      if [ -f "CHANGELOG.md" ]; then
        cp CHANGELOG.md /tmp/gh-aw/agent/release-data/CHANGELOG.md
        echo "✓ Copied CHANGELOG.md for reference"
      fi
      
      # List documentation files for linking
      find docs -type f -name "*.md" 2>/dev/null > /tmp/gh-aw/agent/release-data/docs_files.txt || echo "No docs directory found"
      
      echo "✓ Setup complete."
      echo "  Release data: /tmp/gh-aw/agent/release-data/ (current_release.json, pull_requests.json,"
      echo "    closing_refs_by_issue.json, community_issues_closed_in_window.json,"
      echo "    CHANGELOG.md (if exists), docs_files.txt)"
      echo "  Community data: /tmp/gh-aw/agent/community-data/ (community_issues.json,"
      echo "    closing_refs_by_issue.json, pull_requests.json)"

tools:
  cli-proxy: true
  bash:
    - jq
    - awk
    - sed

evals:
  - id: release-updated
    question: Did the agent update the release with generated highlights?
  - id: highlights-included
    question: Does the agent output confirm that release highlights were generated and prepended to the release notes?
  - id: community-attribution
    question: Does the agent output include attribution to community contributors in the release highlights?

sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Release Highlights Generator

Generate an engaging release highlights summary for **$GITHUB_REPOSITORY** release `${RELEASE_TAG}`.

**Release ID**: ${{ needs.release.outputs.release_id }}

## Data Available

Release-specific data is pre-fetched in `/tmp/gh-aw/agent/release-data/`:
- `current_release.json` - Release metadata (tag, name, dates, existing body)
- `pull_requests.json` - PRs merged between `${PREV_RELEASE_TAG}` and `${RELEASE_TAG}` (includes `closingIssuesReferences` for each PR; empty array if first release)
- `closing_refs_by_issue.json` - Map of `{issue_number: [pr_numbers]}` built from GitHub-native closing references in merged PRs
- `community_issues_closed_in_window.json` - Community issues whose `closedAt` falls within this release window (attribution candidates)
- `CHANGELOG.md` - Full changelog for context (if exists)
- `docs_files.txt` - Available documentation files for linking

Community data is pre-fetched in `/tmp/gh-aw/agent/community-data/` (by the shared community-attribution step):
- `community_issues.json` - All issues labeled `community` (issue number, title, author, closedAt, createdAt, url)
- `closing_refs_by_issue.json` - Same closing references index, mirrored for the shared attribution strategy
- `pull_requests.json` - Same PR list, mirrored for the shared attribution strategy

## Output Requirements

Create a **"🌟 Release Highlights"** section that:
- Is concise and scannable (users grasp key changes in 30 seconds)
- Uses professional, enthusiastic tone (not overly casual)
- Categorizes changes logically (features, fixes, docs, breaking changes)
- Links to relevant documentation where helpful
- Focuses on user impact (why changes matter, not just what changed)

## Workflow

### 1. Load Data

```bash
# View release metadata
cat /tmp/gh-aw/agent/release-data/current_release.json | jq

# List PRs (empty if first release)
cat /tmp/gh-aw/agent/release-data/pull_requests.json | jq -r '.[] | "- #\(.number): \(.title) by @\(.author.login)"'

# List community issues (fetched by shared community-attribution step)
cat /tmp/gh-aw/agent/community-data/community_issues.json | jq -r '.[] | "- #\(.number): \(.title) by @\(.author.login)"'

# View GitHub-native closing references (issue -> [PRs])
cat /tmp/gh-aw/agent/release-data/closing_refs_by_issue.json | jq

# List community issues closed in this release window (attribution candidates)
cat /tmp/gh-aw/agent/release-data/community_issues_closed_in_window.json | jq -r '.[] | "- #\(.number): \(.title) by @\(.author.login) (closed: \(.closedAt))"'

# Check CHANGELOG context
head -100 /tmp/gh-aw/agent/release-data/CHANGELOG.md 2>/dev/null || echo "No CHANGELOG"

# View available docs
cat /tmp/gh-aw/agent/release-data/docs_files.txt
```

### 2. Identify Community Contributions

The `community` label is the **primary attribution signal** — apply the
four-tier Community Attribution Strategy from the imported shared component
(`shared/community-attribution.md`) to attribute all community-labeled issues
that were closed in this release window.  Use `/tmp/gh-aw/agent/release-data/community_issues_closed_in_window.json`
as the set of candidates and `/tmp/gh-aw/agent/release-data/closing_refs_by_issue.json`
as the attribution index.

### 3. Categorize & Prioritize

Group PRs by category (omit categories with no items):
- **🐛 Bug Fixes** - Issue resolutions
- **⚡ Performance** - Speed/efficiency improvements
- **📚 Documentation** - Guide/reference updates
- **⚠️ Breaking Changes** - Requires user action (ALWAYS list first if present)
- **🔧 Internal** - Refactoring, dependencies (usually omit from highlights)

### 4. Write Highlights

Structure:
```markdown
## 🌟 Release Highlights

[1-2 sentence summary of the release theme/focus]

### ⚠️ Breaking Changes
[If any - list FIRST with migration guidance]

### ✨ What's New
[Top 3-5 features with user benefit, link docs when relevant]

### 🐛 Bug Fixes & Improvements
[Notable fixes - focus on user impact]

### 📚 Documentation
[Only if significant doc additions/improvements]

### 🌍 Community Contributions
[Only if any community-labeled issues are resolved in this release]
A huge thank you to the community members who reported issues that were resolved in this release:
- **@[author]** for [issue title] ([#number](url))
  - _(via follow-up #N)_ — include only when attribution was confirmed through a follow-up issue chain
[One entry per community issue author. Omit this section entirely if no community issues are resolved.]

### ⚠️ Attribution Candidates Need Review
[Only if Tier 4 found community issues closed in this release window with no confirmed linkage]
The following community issues were closed during this release window but could not be automatically linked to a specific merged PR. Please verify whether they should be credited:
- **@[author]** for [issue title] ([#number](url)) — closed [date], no confirmed PR linkage found
[Omit this section entirely if all closed community issues have confirmed attribution.]

```

**Writing Guidelines:**
- Lead with benefits: "GitHub MCP now supports remote mode" not "Added remote mode"
- Be specific: "Reduced compilation time by 40%" not "Faster compilation"
- Skip internal changes unless they have user impact
- Use docs links: `[Learn more](https://github.github.com/gh-aw/path/)`
- Celebrate community contributors: thank each issue author by name with a link to their issue

### 5. Handle Special Cases

**First Release** (no `${PREV_RELEASE_TAG}`):
```markdown
## 🎉 First Release

Welcome to the inaugural release! This version includes [core capabilities].

### Key Features
[List primary features with brief descriptions]
```

**Maintenance Release** (no user-facing changes):
```markdown
## 🔧 Maintenance Release

Dependency updates and internal improvements to keep things running smoothly.
```

## Output Format

**CRITICAL**: You MUST call the `update_release` MCP tool to update the release with the generated highlights.

**HOW TO CALL THE TOOL:**

The `update_release` tool is an **MCP (Model Context Protocol) tool**, not a bash command or file operation.

**✅ CORRECT - Call the MCP tool directly:**

```
safeoutputs/update_release(
  tag="v0.38.1",
  operation="prepend",
  body="## 🌟 Release Highlights\n\n[Your complete markdown highlights here]"
)
```

**❌ INCORRECT - DO NOT:**
- Write JSON files manually (e.g., `/tmp/gh-aw/agent/safeoutputs/update_release_001.json`)
- Use bash to simulate tool calls
- Create scripts that write to outputs.jsonl
- Use any file operations - the MCP tool handles everything

**Required Parameters:**
- `tag` - Release tag from `${RELEASE_TAG}` environment variable (e.g., "v0.38.1")
- `operation` - Must be `"prepend"` to add before existing notes
- `body` - Complete markdown content (include all formatting, emojis, links)

**IMPORTANT**: The tool is accessed via the MCP gateway as `safeoutputs/update_release`. When you call this tool, the MCP server automatically writes to `/opt/gh-aw/safeoutputs/outputs.jsonl`.

**WARNING**: If you don't call the MCP tool properly, the release notes will NOT be updated!

**Documentation Base URLs:**
- User docs: `https://github.github.com/gh-aw/`
- Reference: `https://github.github.com/gh-aw/reference/`
- Setup: `https://github.github.com/gh-aw/setup/`

Verify paths exist in `docs_files.txt` before linking.