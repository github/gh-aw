---
description: Guidance for creating release agentic workflows that combine classic Action jobs (build, test, publish) with an agent job that generates release highlights.
---

# Release Workflow Pattern

Use this guidance when the user asks to create a workflow that:

- Builds, tests, and publishes a GitHub release
- Generates or prepends release highlights / changelog summaries to the release description

> [!IMPORTANT]
> Agent-generated release notes and all other agent-driven release-description changes must use the `update-release` safe output. The agent must not mutate releases directly with `gh`, the GitHub API, or a GitHub write tool.

## Pattern Overview

A release workflow follows the **Classic + Agent** hybrid structure:

1. **Classic jobs** — deterministic pipeline executed as standard GitHub Actions jobs: compute the next semantic version, build binaries, run tests, scan for security issues, create the GitHub release.
2. **Agent job** — runs after the classic `release` job; reads merged PR data and changelog; generates human-readable release highlights; updates the release description using the `update-release` safe output.

```
workflow_dispatch (release_type: patch | minor | major)
├── config        — compute next semver tag; output: release_tag
├── build         — build binaries; upload artifact
├── test          — run test suite (may be parallel to build)
├── [security]    — optional: virus scan, SBOM, attestation
├── release       — create prerelease; upload binaries; output: release_id
└── agent         — fetch PRs + changelog; generate highlights; update_release(prepend)
```

The agent job is the **only** job that uses the agentic engine. All other jobs are standard GitHub Actions steps.

## Frontmatter Template

```yaml
---
private: true
name: Release
emoji: "🚀"
description: Build, test, and release, then generate release highlights
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
        options: [patch, minor, major]
permissions:
  contents: read
  pull-requests: read
  actions: read
safe-outputs:
  update-release:
  threat-detection: false   # release bodies often contain code snippets; disable threat scanning
network:
  allowed:
    - defaults
    - <ecosystem>           # add: go, node, python, rust, etc. based on project
---
```

Key frontmatter decisions:

- `private: true` — release workflows should not be visible in the public agentic workflow gallery
- `roles: [admin, maintainer]` — restrict triggering to trusted collaborators
- `threat-detection: false` — disable threat scanning on `update-release` because release notes intentionally include code snippets and technical content that can trigger false positives
- Global `permissions: contents: read` with per-job overrides for write operations

## Classic Jobs

Design these jobs exactly as you would a standard GitHub Actions workflow. The compiler handles action version pinning automatically.

### config

Computes the next semantic version from existing GitHub releases/tags using `actions/github-script`. Must output `release_tag`:

```yaml
  config:
    needs: ["pre_activation", "activation"]
    runs-on: ubuntu-latest
    outputs:
      release_tag: ${{ steps.compute_config.outputs.release_tag }}
    steps:
      - name: Compute Release Config
        id: compute_config
        uses: actions/github-script@v9
        with:
          script: |
            const releaseType = context.payload.inputs.release_type;
            const { data: releases } = await github.rest.repos.listReleases({
              owner: context.repo.owner, repo: context.repo.repo, per_page: 100
            });
            // parse, sort semver, bump, check for collision, set output
            // core.setOutput('release_tag', releaseTag);
```

### build

Checks out the repository, builds binaries, and uploads them as a GitHub Actions artifact for downstream jobs:

```yaml
  build:
    needs: ["pre_activation", "activation", "config"]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - name: Build
        run: bash scripts/build-release.sh ${{ needs.config.outputs.release_tag }}
      - uses: actions/upload-artifact@v7
        with:
          name: release-binaries-${{ needs.config.outputs.release_tag }}
          path: dist/
          retention-days: 1
```

### release

Creates the GitHub release using `gh release create`. Use `--prerelease --latest=false` initially so the release is visible but not promoted until verification is complete. Must output `release_id`:

```yaml
  release:
    needs: ["pre_activation", "activation", "config", "build"]
    runs-on: ubuntu-latest
    permissions:
      contents: write       # override: required to create tags and releases
    outputs:
      release_id: ${{ steps.create_release.outputs.release_id }}
    steps:
      - uses: actions/checkout@v7
        with: { fetch-depth: 0, persist-credentials: true }
      - uses: actions/download-artifact@v8
        with:
          name: release-binaries-${{ needs.config.outputs.release_tag }}
          path: dist/
      - name: Create GitHub release
        id: create_release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
        run: |
          gh release create "$RELEASE_TAG" dist/* \
            --title "$RELEASE_TAG" \
            --generate-notes \
            --prerelease \
            --latest=false
          RELEASE_ID=$(gh release view "$RELEASE_TAG" --json databaseId --jq .databaseId)
          echo "release_id=$RELEASE_ID" >> "$GITHUB_OUTPUT"
```

## Agent Job

The agent job must depend on the `release` job (so the release exists before the agent runs) and run with the global read-only permissions.

### Pre-step: Fetch Release Context

Use a deterministic `steps:` block to pre-fetch all data before the agent runs. Write output to `/tmp/gh-aw/agent/release-data/` (standard agent pre-fetch path).

```yaml
steps:
  - name: Fetch release context
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      RELEASE_TAG: ${{ needs.config.outputs.release_tag }}
      RELEASE_ID: ${{ needs.release.outputs.release_id }}
    run: |
      mkdir -p /tmp/gh-aw/agent/release-data

      # Fetch the newly created release
      gh api "/repos/$GITHUB_REPOSITORY/releases/$RELEASE_ID" \
        > /tmp/gh-aw/agent/release-data/current_release.json

      # Find previous release to scope PR list
      PREV_TAG=$(gh release list --limit 2 --json tagName --jq '.[1].tagName // empty')
      if [ -n "$PREV_TAG" ]; then
        PREV_AT=$(gh release view "$PREV_TAG" --json publishedAt --jq .publishedAt)
        CURR_AT=$(gh release view "$RELEASE_TAG" --json publishedAt --jq .publishedAt)
        gh pr list --state merged --limit 500 \
          --json number,title,author,labels,mergedAt,url,body \
          --jq "[.[] | select(.mergedAt >= \"$PREV_AT\" and .mergedAt <= \"$CURR_AT\")]" \
          > /tmp/gh-aw/agent/release-data/pull_requests.json
      else
        echo "[]" > /tmp/gh-aw/agent/release-data/pull_requests.json
      fi

      [ -f CHANGELOG.md ] && cp CHANGELOG.md /tmp/gh-aw/agent/release-data/CHANGELOG.md || true

tools:
  cli-proxy: true          # allows gh and jq calls inside the agent sandbox
```

### Prompt

The markdown body (after the `---` separator) forms the agent prompt:

```markdown
# Release Highlights Generator

Generate release highlights for **$GITHUB_REPOSITORY** release `${RELEASE_TAG}`.

**Release ID**: ${{ needs.release.outputs.release_id }}

## Data

Pre-fetched in `/tmp/gh-aw/agent/release-data/`:
- `current_release.json` — release metadata and auto-generated notes body
- `pull_requests.json` — PRs merged since the previous release (empty array for first release)
- `CHANGELOG.md` — changelog content (if present)

## Task

1. Read `current_release.json` and `pull_requests.json`.
2. Categorize changes: **Breaking Changes**, **New Features**, **Bug Fixes**, **Documentation**, **Internal** (omit internal from highlights unless user-impacting).
3. Write a concise "## 🌟 Release Highlights" section that is scannable in 30 seconds.
4. Call `safeoutputs/update_release(tag="${RELEASE_TAG}", operation="prepend", body="...")` to prepend the highlights before the auto-generated notes.
5. Call `noop` with a short explanation only if there are no user-facing changes.

## Output Format

Use `operation: "prepend"` so the highlights appear before the GitHub-generated release notes.
Do not replace the auto-generated notes — prepend only.
```

## Key Rules

- **Agent job stays read-only** — all release-note and release-description writes must route through `update-release`; never use direct `gh`, API, or GitHub write-tool mutations
- **Use `operation: "prepend"`** so highlights appear before the auto-generated GitHub notes; never `replace`
- **The `release` job must output `release_id`** — the agent needs the database ID to reference the correct release
- **Pre-fetch all data in `steps:`** before the agent runs; write compact JSON to `/tmp/gh-aw/agent/release-data/`
- **Include `cli-proxy: true`** in the agent `tools:` block to allow `gh` and `jq` use inside the sandbox
- **Declare `contents: write` per-job**, not globally — only the jobs that push tags or create releases need it
- **Set `threat-detection: false`** in `safe-outputs:` — release bodies contain code snippets that trigger false positives
- **Network**: classic jobs installing packages need the ecosystem entry (e.g. `go`, `node`); the agent job itself only needs `defaults`

## Common Additions

| Addition | Where | Notes |
|---|---|---|
| Security/virus scan | After build, before release | Use `runs-on: windows-latest` with Microsoft Defender for binaries |
| SBOM generation | Inside the `release` job | Use `anchore/sbom-action`; upload as artifact, not as release asset |
| Attestation | Inside the `release` job | Use `actions/attest-build-provenance`; requires `id-token: write` + `attestations: write` |
| Gate / environment approval | Between jobs | Use `environment:` on a gate job; useful for manual sign-off before releasing |
| Comment on merged PRs | After agent job | Separate classic job using `actions/github-script` to notify PR authors; requires `pull-requests: write` + `issues: write` |
| Community attribution | Agent prefetch step | Fetch community-labeled issues closed in the release window for attribution in highlights |

## Reference Implementation

See `.github/workflows/release.md` in this repository for a complete production-grade release workflow that implements this pattern, including:
- Semantic version collision detection
- Multi-platform binary builds
- Microsoft Defender antivirus scanning
- SBOM generation (SPDX + CycloneDX)
- Build attestation
- Manual sync-actions approval gate
- Community attribution in release highlights
