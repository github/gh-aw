---
name: Release
description: Build, test, and release gh-aw extension, then generate and prepend release highlights
on:
  workflow_dispatch:
    inputs:
      release_type:
        description: 'Release type (patch, minor, or major)'
        required: true
        type: choice
        options:
          - patch
          - minor
          - major
permissions:
  contents: read
  pull-requests: read
  actions: read
  issues: read
roles:
  - admin
  - maintainer
engine: copilot
timeout-minutes: 20
network:
  allowed:
    - defaults
    - node
    - "github.github.io"
sandbox:
  agent: awf  # Firewall enabled (migrated from network.firewall)
tools:
  bash:
    - "*"
  edit:
safe-outputs:
  update-release:
jobs:
  config:
    needs: ["activation"]
    runs-on: ubuntu-latest
    outputs:
      release_tag: ${{ steps.compute_config.outputs.release_tag }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v6
        with:
          fetch-depth: 0
          persist-credentials: false
      
      - name: Compute release configuration
        id: compute_config
        uses: actions/github-script@v7
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
            
            const releaseTag = `v${major}.${minor}.${patch}`;
            console.log(`Computed release tag: ${releaseTag}`);
            
            // Sanity check: Verify the computed tag doesn't already exist
            const existingRelease = releases.find(r => r.tag_name === releaseTag);
            if (existingRelease) {
              core.setFailed(`Release tag ${releaseTag} already exists (created ${existingRelease.created_at}). Cannot create duplicate release. Please check existing releases.`);
              return;
            }
            
            // Also check if tag exists in git (in case release was deleted but tag remains)
            try {
              await github.rest.git.getRef({
                owner: context.repo.owner,
                repo: context.repo.repo,
                ref: `tags/${releaseTag}`
              });
              // If we get here, the tag exists
              core.setFailed(`Git tag ${releaseTag} already exists in the repository. Cannot create duplicate tag. Please delete the existing tag or use a different version.`);
              return;
            } catch (error) {
              // 404 means tag doesn't exist, which is what we want
              if (error.status !== 404) {
                throw error; // Re-throw unexpected errors
              }
            }
            
            core.setOutput('release_tag', releaseTag);
            console.log(`✓ Release tag: ${releaseTag}`);
  release:
    needs: ["activation", "config"]
    runs-on: ubuntu-latest
    permissions:
      contents: write
      packages: write
      id-token: write
      attestations: write
    outputs:
      release_id: ${{ steps.get_release.outputs.release_id }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v6
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
        uses: actions/setup-go@4dc6199c7b1a012772edbd06daecab0f50c9053c # v6.1.0
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
        uses: docker/setup-buildx-action@v3

      - name: Build Docker image (validation only)
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: linux/amd64
          push: false
          load: false
          build-args: |
            BINARY=dist/linux-amd64
          cache-from: type=gha

      - name: Create GitHub release
        id: get_release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
        run: |
          echo "Creating GitHub release: $RELEASE_TAG"
          
          # Create release with binaries (SBOM files will be added later)
          gh release create "$RELEASE_TAG" \
            dist/* \
            --title "$RELEASE_TAG" \
            --generate-notes
          
          # Get release ID
          RELEASE_ID=$(gh release view "$RELEASE_TAG" --json databaseId --jq '.databaseId')
          echo "release_id=$RELEASE_ID" >> "$GITHUB_OUTPUT"
          echo "✓ Release created: $RELEASE_TAG"
          echo "✓ Release ID: $RELEASE_ID"

      - name: Download Go modules
        run: go mod download

      - name: Generate SBOM (SPDX format)
        uses: anchore/sbom-action@v0
        with:
          artifact-name: sbom.spdx.json
          output-file: sbom.spdx.json
          format: spdx-json

      - name: Generate SBOM (CycloneDX format)
        uses: anchore/sbom-action@v0
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
        uses: actions/upload-artifact@v6
        with:
          name: sbom-artifacts
          path: |
            sbom.spdx.json
            sbom.cdx.json
          retention-days: 7  # Minimize exposure window

      - name: Upload SBOM files to release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
        run: |
          echo "Uploading SBOM files to release: $RELEASE_TAG"
          gh release upload "$RELEASE_TAG" \
            sbom.spdx.json \
            sbom.cdx.json
          echo "✓ SBOM files uploaded to release"

      - name: Setup Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata for Docker
        id: meta
        uses: docker/metadata-action@v5
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
        uses: docker/build-push-action@v6
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

steps:
  - name: Setup environment and fetch release data
    env:
      RELEASE_ID: ${{ needs.release.outputs.release_id }}
      RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      mkdir -p /tmp/gh-aw/release-data
      
      # Use the release ID and tag from the release job
      echo "Release ID from release job: $RELEASE_ID"
      echo "Release tag from release job: $RELEASE_TAG"
      
      echo "Processing release: $RELEASE_TAG"
      echo "RELEASE_TAG=$RELEASE_TAG" >> "$GITHUB_ENV"
      
      # Get the current release information
      # Use release ID to fetch release data
      gh api "/repos/${{ github.repository }}/releases/$RELEASE_ID" > /tmp/gh-aw/release-data/current_release.json
      echo "✓ Fetched current release information"
      
      # Get the previous release to determine the range
      PREV_RELEASE_TAG=$(gh release list --limit 2 --json tagName --jq '.[1].tagName // empty')
      
      if [ -z "$PREV_RELEASE_TAG" ]; then
        echo "No previous release found. This appears to be the first release."
        echo "PREV_RELEASE_TAG=" >> "$GITHUB_ENV"
        touch /tmp/gh-aw/release-data/pull_requests.json
        echo "[]" > /tmp/gh-aw/release-data/pull_requests.json
      else
        echo "Previous release: $PREV_RELEASE_TAG"
        echo "PREV_RELEASE_TAG=$PREV_RELEASE_TAG" >> "$GITHUB_ENV"
        
        # Get commits between releases
        echo "Fetching commits between $PREV_RELEASE_TAG and $RELEASE_TAG..."
        git fetch --unshallow 2>/dev/null || git fetch --depth=1000
        
        # Get all merged PRs between the two releases
        echo "Fetching pull requests merged between releases..."
        PREV_PUBLISHED_AT=$(gh release view "$PREV_RELEASE_TAG" --json publishedAt --jq .publishedAt)
        CURR_PUBLISHED_AT=$(gh release view "$RELEASE_TAG" --json publishedAt --jq .publishedAt)
        gh pr list \
          --state merged \
          --limit 1000 \
          --json number,title,author,labels,mergedAt,url,body \
          --jq "[.[] | select(.mergedAt >= \"$PREV_PUBLISHED_AT\" and .mergedAt <= \"$CURR_PUBLISHED_AT\")]" \
          > /tmp/gh-aw/release-data/pull_requests.json
        
        PR_COUNT=$(jq length "/tmp/gh-aw/release-data/pull_requests.json")
        echo "✓ Fetched $PR_COUNT pull requests"
      fi
      
      # Get the CHANGELOG.md content around this version
      if [ -f "CHANGELOG.md" ]; then
        cp CHANGELOG.md /tmp/gh-aw/release-data/CHANGELOG.md
        echo "✓ Copied CHANGELOG.md for reference"
      fi
      
      # List documentation files for linking
      find docs -type f -name "*.md" 2>/dev/null > /tmp/gh-aw/release-data/docs_files.txt || echo "No docs directory found"
      
      echo "✓ Setup complete. Data available in /tmp/gh-aw/release-data/"
imports:
  - shared/mood.md
---

# Release Highlights Generator

Generate an engaging release highlights summary for **${{ github.repository }}** release `${RELEASE_TAG}`.

**Release ID**: ${{ needs.release.outputs.release_id }}

## Data Available

All data is pre-fetched in `/tmp/gh-aw/release-data/`:
- `current_release.json` - Release metadata (tag, name, dates, existing body)
- `pull_requests.json` - PRs merged between `${PREV_RELEASE_TAG}` and `${RELEASE_TAG}` (empty array if first release)
- `CHANGELOG.md` - Full changelog for context (if exists)
- `docs_files.txt` - Available documentation files for linking

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
cat /tmp/gh-aw/release-data/current_release.json | jq

# List PRs (empty if first release)
cat /tmp/gh-aw/release-data/pull_requests.json | jq -r '.[] | "- #\(.number): \(.title) by @\(.author.login)"'

# Check CHANGELOG context
head -100 /tmp/gh-aw/release-data/CHANGELOG.md 2>/dev/null || echo "No CHANGELOG"

# View available docs
cat /tmp/gh-aw/release-data/docs_files.txt
```

### 2. Categorize & Prioritize

Group PRs by category (omit categories with no items):
- **✨ New Features** - User-facing capabilities
- **🐛 Bug Fixes** - Issue resolutions
- **⚡ Performance** - Speed/efficiency improvements
- **📚 Documentation** - Guide/reference updates
- **⚠️ Breaking Changes** - Requires user action (ALWAYS list first if present)
- **🔧 Internal** - Refactoring, dependencies (usually omit from highlights)

### 3. Write Highlights

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

---
For complete details, see [CHANGELOG](https://github.com/github/gh-aw/blob/main/CHANGELOG.md).
```

**Writing Guidelines:**
- Lead with benefits: "GitHub MCP now supports remote mode" not "Added remote mode"
- Be specific: "Reduced compilation time by 40%" not "Faster compilation"
- Skip internal changes unless they have user impact
- Use docs links: `[Learn more](https://github.github.com/gh-aw/path/)`
- Keep breaking changes prominent with action items

### 4. Handle Special Cases

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
- Write JSON files manually (e.g., `/tmp/gh-aw/safeoutputs/update_release_001.json`)
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