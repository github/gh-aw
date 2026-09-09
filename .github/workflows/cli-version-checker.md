---
private: true
emoji: "🔢"
description: Monitors and updates agentic CLI tools (Claude Code, GitHub Copilot CLI, OpenAI Codex, GitHub MCP Server, Playwright CLI, MCP Gateway, Pi, threat-detect) and Docker images (actionlint, syft, grype, grant, zizmor, poutine, runner-guard, yamllint) for new versions
on:
  schedule: daily
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  pull-requests: read
  issues: read
  copilot-requests: write
model: copilot/gpt-5.4
strict: false
engine:
  id: pi
  model-provider: github
network: 
   allowed: [defaults, node, go, "api.github.com", containers]
imports:
  - ../skills/jqschema/SKILL.md
  - shared/reporting.md
  - shared/otlp.md
  - shared/graders.md
sandbox:
  agent:
    id: awf
tools:
  cli-proxy: true
  web-fetch:
  cache-memory: true
  bash:
    - "*"
  edit:
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[ca] "
    labels: [automation, dependencies, cookie]
    close-older-issues: true
timeout-minutes: 45
features:
  gh-aw-detection: true
evals:
  - id: cli_versions_checked
    question: Did the agent check for new versions of agentic CLI tools (Claude Code, GitHub Copilot CLI, Codex, MCP servers, etc.)?
  - id: docker_images_checked
    question: Did the agent check for new versions and digest changes of Docker images in pkg/cli/docker_images.go (actionlint, syft, grype, grant, zizmor, poutine, runner-guard, yamllint)?
  - id: updates_applied_or_noop
    question: Were version or digest updates applied and a PR created, or was noop used when all tools were already up to date?

---

# CLI Version Checker

Monitor and update agentic CLI tools: Claude Code, GitHub Copilot CLI, OpenAI Codex, GitHub MCP Server, Playwright CLI, MCP Gateway, Pi, and threat-detect.

**Repository**: ${{ github.repository }} | **Run**: ${{ github.run_id }}

## Process

**EFFICIENCY FIRST**: Before starting:
1. Check cache-memory at `/tmp/gh-aw/cache-memory/` for previous version checks and help outputs
2. If cached versions exist and are recent (< 24h), verify if updates are needed before proceeding
3. If no CLI version, Docker image version, or Docker image digest changes are detected, call `noop` as the terminal safe output and exit successfully

**CRITICAL**: If ANY version or digest changes are detected, you MUST create an issue using safe-outputs.create-issue. Do not skip issue creation even for minor updates.

For each CLI/MCP server and threat-detect:
1. Fetch latest version from NPM registry or GitHub releases (use npm view commands for package metadata)
2. Compare with the current version constant in `./pkg/constants/`
3. If newer version exists, research changes and prepare update

### Version Sources
- **Claude Code**: Use `npm view @anthropic-ai/claude-code version` (faster than web-fetch)
  - No public GitHub repository
- **Copilot CLI**: Use `npm view @github/copilot version`
  - Repository: https://github.com/github/copilot-cli
  - **CRITICAL**: Always attempt to fetch and deeply analyze Copilot repository content
  - Release Notes: https://github.com/github/copilot-cli/releases
  - Changelog: https://github.com/github/copilot-cli/blob/main/CHANGELOG.md (or similar)
  - README: https://github.com/github/copilot-cli/blob/main/README.md
- **Codex**: Use `npm view @openai/codex version`
  - Repository: https://github.com/openai/codex
  - Release Notes: https://github.com/openai/codex/releases
- **GitHub MCP Server**: `https://api.github.com/repos/github/github-mcp-server/releases/latest`
  - Release Notes: https://github.com/github/github-mcp-server/releases
- **Playwright CLI**: Use `npm view @playwright/cli version`
  - Repository: https://github.com/microsoft/playwright-cli
  - Package: https://www.npmjs.com/package/@playwright/cli
- **MCP Gateway**: `https://api.github.com/repos/github/gh-aw-mcpg/releases/latest`
  - Repository: https://github.com/github/gh-aw-mcpg
  - Release Notes: https://github.com/github/gh-aw-mcpg/releases
  - Docker Image: `ghcr.io/github/gh-aw-mcpg:v{VERSION}`
  - Used as default sandbox.agent container (see `pkg/constants/constants.go`)
- **Pi**: Use `npm view @earendil-works/pi-coding-agent version`
  - Package: https://www.npmjs.com/package/@earendil-works/pi-coding-agent
  - Constant: `DefaultPiVersion` in `pkg/constants/version_constants.go`
- **Threat-detect**: Check `https://api.github.com/repos/github/gh-aw-threat-detection/releases/latest`
  - Release notes: https://github.com/github/gh-aw-threat-detection/releases
  - Constant: `DefaultThreatDetectVersion` in `pkg/constants/version_constants.go`
  - Update the version with the `v` prefix used by GitHub release tags
**Optimization**: Fetch all versions in parallel using multiple npm view or WebFetch calls in a single turn.

### Research & Analysis
For each update, analyze intermediate versions:
- Categorize changes: Breaking, Features, Fixes, Security, Performance
- Assess impact on gh-aw workflows
- Document migration requirements
- Assign risk level (Low/Medium/High)

**GitHub Release Notes (when available)**:
- **Codex**: Fetch release notes from https://github.com/openai/codex/releases/tag/rust-v{VERSION}
  - Parse the "Highlights" section for key changes
  - Parse the "PRs merged" or "Merged PRs" section for detailed changes
  - **CRITICAL**: Convert PR/issue references (e.g., `#6211`) to full URLs since they refer to external repositories (e.g., `https://github.com/openai/codex/pull/6211`)
- **GitHub MCP Server**: Fetch release notes from https://github.com/github/github-mcp-server/releases/tag/v{VERSION}
  - Parse release body for changelog entries
  - **CRITICAL**: Convert PR/issue references (e.g., `#1105`) to full URLs since they refer to external repositories (e.g., `https://github.com/github/github-mcp-server/pull/1105`)
- **Copilot CLI**: **ALWAYS attempt deep analysis** - Repository: https://github.com/github/copilot-cli
  - **CRITICAL**: Thoroughly read and analyze all available documentation:
    1. **Release Notes**: Fetch from https://github.com/github/copilot-cli/releases/tag/v{VERSION}
       - Parse release highlights and feature descriptions
       - Extract breaking changes and deprecation notices
       - Note new commands, flags, and configuration options
    2. **CHANGELOG.md**: Read from https://github.com/github/copilot-cli/blob/main/CHANGELOG.md (or equivalent)
       - Compare versions to identify all changes between current and new version
       - Categorize changes: Breaking, Features, Fixes, Security, Performance
    3. **README.md**: Review https://github.com/github/copilot-cli/blob/main/README.md
       - Check for updated usage patterns and examples
       - Note new capabilities or configuration options
    4. **Documentation Changes**: Look for changes in documentation files that indicate new features
  - If repository is inaccessible (private), document the access limitation in the issue but still:
    - Use `npm view @github/copilot --json` for detailed package metadata
    - Compare CLI help output between versions (see "Tool Installation & Discovery" section)
    - Check for any publicly available release announcements or blog posts
  - **CRITICAL**: Convert PR/issue references to full URLs (e.g., `https://github.com/github/copilot-cli/pull/123`)
- **Claude Code**: No public repository, rely on NPM metadata and CLI help output
- **Playwright CLI**: Check NPM package metadata and GitHub releases for changes
  - Fetch release notes from https://github.com/microsoft/playwright-cli/releases/tag/v{VERSION}
  - **CRITICAL**: Convert PR/issue references to full URLs (e.g., `https://github.com/microsoft/playwright-cli/pull/123`)
- **MCP Gateway**: Fetch release notes from https://github.com/github/gh-aw-mcpg/releases/tag/{VERSION}
  - Parse release body for changelog entries
  - **CRITICAL**: Convert PR/issue references to full URLs (e.g., `https://github.com/github/gh-aw-mcpg/pull/123`)
  - Note: Used as default sandbox.agent container in MCP Gateway configuration
- **Pi**: No public GitHub repository; rely on NPM metadata and CLI help output
  - Use `npm view @earendil-works/pi-coding-agent --json` for package metadata
  - Compare CLI help output between versions
- **Threat-detect**: Fetch release notes from https://github.com/github/gh-aw-threat-detection/releases
**NPM Metadata Fallback**: When GitHub release notes are unavailable, use:
- `npm view <package> --json` for package metadata
- Compare CLI help outputs between versions
- Check for version changelog in package description

### Tool Installation & Discovery
Check cache-memory first (`/tmp/gh-aw/cache-memory/`). Only install and run `--help` if the version changed; then save outputs to cache.

For each CLI tool update, install (`npm install -g <package>@<version>`), run `--help` on the main command and key subcommands (Copilot: `config`, `environment`), and compare with the cached output to identify new flags, removed features, or behavior changes.

### Update Process
1. Edit the appropriate version constant in `./pkg/constants/` with new CLI version(s)
2. Edit `./pkg/cli/docker_images.go` with new Docker image version(s) and digest(s), including digest-only changes where the tag is unchanged
3. Run `make fmt` after editing any Go files
4. **REQUIRED**: Run `make recompile` in the **foreground** — do NOT background it with `&` or follow it with `sleep`. Wait for it to finish completely before proceeding. Example: `make recompile && echo "done"`.
5. Verify changes with `git status`
6. **REQUIRED**: Create issue via safe-outputs with detailed analysis (do NOT skip this step)

## Issue Format

**Follow the Report Structure Pattern defined in `shared/reporting.md`.**

For each updated CLI, include: version old → new, release timeline, changes categorized as Breaking/Features/Fixes/Security/Performance, impact assessment, changelog links, and any CLI/subcommand changes discovered via help output.

**Important**: Use h3 (###) or lower for all headers. Wrap full changelogs in `<details>` tags. Use plain URLs (no backticks) and convert `#1234` PR references to full external URLs like `https://github.com/owner/repo/pull/1234`.

## Guidelines
- Only update stable versions (no pre-releases)
- Prioritize security updates
- Document all intermediate versions
- **USE NPM COMMANDS**: Use `npm view` instead of web-fetch for package metadata queries
- **CHECK CACHE FIRST**: Before re-analyzing versions, check cache-memory for recent results
- **PARALLEL FETCHING**: Fetch all versions in parallel using multiple npm/WebFetch calls in one turn
- **EARLY EXIT**: If no version changes are detected, save the check timestamp to cache, call `noop` as the terminal safe output, and exit successfully
- **FETCH GITHUB RELEASE NOTES**: For tools with public GitHub repositories, fetch release notes to get detailed changelog information
  - Codex: Always fetch from https://github.com/openai/codex/releases
  - GitHub MCP Server: Always fetch from https://github.com/github/github-mcp-server/releases
  - MCP Gateway: Always fetch from https://github.com/github/gh-aw-mcpg/releases
  - Copilot CLI: Try to fetch, but may be inaccessible (private repo)
  - Playwright CLI: Fetch from https://github.com/microsoft/playwright-cli/releases
  - Pi: No public GitHub repository; rely on NPM metadata (`npm view @earendil-works/pi-coding-agent --json`)
- **EXPLORE SUBCOMMANDS**: Install and test CLI tools to discover new features via `--help` and explore each subcommand
  - For Copilot CLI, explicitly check: `config`, `environment` and any other available subcommands
  - Use commands like `copilot help <subcommand>` or `<tool> <subcommand> --help`
- Compare help output between old and new versions (both main help and subcommand help)
- **SAVE TO CACHE**: Store help outputs (main and all subcommands) and version check results in cache-memory
- **REQUIRED**: Always run `make recompile` in the **foreground** (not backgrounded) after updating constants — wait for completion before proceeding
- **DO NOT COMMIT** `*.lock.yml` or `pkg/workflow/js/*.js` files directly

## Docker Image Version Checking

After checking CLI tools, also check the Docker images defined in `./pkg/cli/docker_images.go` for version and digest updates. Resolve the registry digest for every image on every run and compare it with the digest pinned in the Go constant. A changed digest is an update even when the image tag is unchanged.

### Docker Image Sources

Fetch the latest release for each image from its GitHub repository:

| Constant | Current image | GitHub releases URL |
|---|---|---|
| `ActionlintImage` | `rhysd/actionlint:<version>@sha256:<digest>` | `https://api.github.com/repos/rhysd/actionlint/releases/latest` |
| `SyftImage` | `anchore/syft:<version>@sha256:<digest>` | `https://api.github.com/repos/anchore/syft/releases/latest` |
| `GrypeImage` | `anchore/grype:<version>@sha256:<digest>` | `https://api.github.com/repos/anchore/grype/releases/latest` |
| `GrantImage` | `anchore/grant:<version>@sha256:<digest>` | `https://api.github.com/repos/anchore/grant/releases/latest` |
| `ZizmorImage` | `ghcr.io/zizmorcore/zizmor:<version>@sha256:<digest>` | `https://api.github.com/repos/zizmorcore/zizmor/releases/latest` |
| `PoutineImage` | `ghcr.io/boostsecurityio/poutine:<version>@sha256:<digest>` | `https://api.github.com/repos/boostsecurityio/poutine/releases/latest` |
| `RunnerGuardImage` | `ghcr.io/vigilant-llc/runner-guard:<version>@sha256:<digest>` | `https://api.github.com/repos/vigilant-llc/runner-guard/releases/latest` |
| `YamllintImage` | `pipelinecomponents/yamllint:<version>@sha256:<digest>` | `https://api.github.com/repos/PipelineComponents/yamllint/releases/latest` |

**Optimization**: Fetch all GitHub release endpoints in parallel in a single turn.

### 3-Day Cooldown

Before considering any Docker image version update, check that the release is **at least 3 days old**:
1. Parse the `published_at` field from the GitHub releases API response.
2. Compute `(current date) - published_at`. If less than 3 days, **skip** that image — do not update it or include it in the issue.
3. Only proceed with images whose latest release is ≥ 3 days old.

This avoids picking up immature or quickly-retracted releases. Digest-only updates for an already-pinned version are not subject to the release cooldown.

### Fetching the Container SHA

For every Docker image, fetch the registry digest for the target tag. For a newer version, apply the 3-day cooldown first. Also fetch and compare the digest when the version is unchanged so mutable or republished tags are reflected in `docker_images.go`.

**Docker Hub images** (actionlint, syft, grype, grant, yamllint):
```bash
# Get a Docker Hub anonymous token for the repo
TOKEN=$(curl -s "https://auth.docker.io/token?scope=repository:anchore/syft:pull&service=registry.docker.io" | jq -r .token)
# Fetch the manifest digest (amd64 manifest list or single-arch)
curl -sI \
  -H "Authorization: ******" \
  -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
  -H "Accept: application/vnd.docker.distribution.manifest.list.v2+json" \
  "https://registry-1.docker.io/v2/anchore/syft/manifests/v1.49.0" \
  | grep -i "Docker-Content-Digest:" | awk '{print $2}' | tr -d '\r'
```
Replace the `scope` repository slug and the tag as appropriate for each image.

**GHCR images** (zizmor, poutine, runner-guard):
```bash
# Use docker manifest inspect (no auth required for public GHCR images)
docker manifest inspect "ghcr.io/zizmorcore/zizmor:v1.0.0" --verbose 2>/dev/null \
  | jq -r 'if type == "array" then .[0].Descriptor.digest else .config.digest end'
```
If `docker` is unavailable, fall back to the GHCR anonymous API:
```bash
TOKEN=$(curl -s "https://ghcr.io/token?scope=repository:zizmorcore/zizmor:pull&service=ghcr.io" | jq -r .token)
curl -sI \
  -H "Authorization: ******" \
  -H "Accept: application/vnd.docker.distribution.manifest.v2+json" \
  -H "Accept: application/vnd.docker.distribution.manifest.list.v2+json" \
  "https://ghcr.io/v2/zizmorcore/zizmor/manifests/v1.0.0" \
  | grep -i "Docker-Content-Digest:" | awk '{print $2}' | tr -d '\r'
```

**CRITICAL**: Always record the digest as `sha256:<hex>`. This is the value that goes after `@` in the image reference.

### Comparing and Updating docker_images.go

Compare each fetched digest with the `@sha256:...` value in `./pkg/cli/docker_images.go`. If the constant has no digest, treat it as needing an update. Edit the file whenever the version or digest differs:

- **All images**: pin the selected version tag and registry digest in the existing string format, e.g.:
  ```
  SyftImage = "anchore/syft:v1.49.0@sha256:<new-digest>"
  ```
- **Digest-only changes**: preserve the current version tag and replace only the digest, e.g.:
  ```
  ActionlintImage = "rhysd/actionlint:1.7.13@sha256:<new-digest>"
  ```
- **Images currently tagged `:latest`**: replace `latest` with the latest stable release tag that passed the cooldown and pin its digest, e.g.:
  ```
  ZizmorImage = "ghcr.io/zizmorcore/zizmor:v1.0.0@sha256:<digest>"
  ```

### Docker Image Update Process

1. Fetch all latest releases in parallel via `api.github.com`.
2. Apply the 3-day cooldown to version updates — skip a newer release if it is < 3 days old.
3. Fetch the container digest for every selected tag, including unchanged versions (see "Fetching the Container SHA" above).
4. Compare each fetched digest with `./pkg/cli/docker_images.go` and edit the constant for version changes, digest changes, or missing digests.
5. Run `make fmt` to format any changed Go files.
6. Include Docker image update details in the issue created by safe-outputs.

### Docker Image Section in the Issue

For each updated Docker image, include:
- Image name and constant (e.g., `SyftImage`)
- Version change: old → new (or "unchanged" for a digest-only update)
- Release date and cooldown confirmation (e.g., "Released 2026-07-20 — 5 days ago, cooldown passed")
- Digest change: old digest → new digest (or "missing → new digest")
- Container digest (e.g., `sha256:abc123...`)
- Full image reference: `anchore/syft:v1.49.0@sha256:abc123...`
- Link to the GitHub release
- Summary of release notes (Breaking / Features / Fixes / Security)

Wrap long changelogs in `<details>` tags. Use plain URLs (no backticks). Convert `#1234` references to full external URLs.

## JSON Parsing Tips

Filter stderr and use jq to avoid Unicode token errors from npm output:
```bash
npm view @github/copilot --json 2>/dev/null | jq -r '.version'
```

## Error Handling
- **SAVE PROGRESS**: Before exiting on errors, save current state to cache-memory
- **RESUME ON RESTART**: Check cache-memory on startup to resume from where you left off
- Retry NPM registry failures once after 30s
- Continue if individual changelog fetch fails
- Skip PR creation if recompile fails
- Exit successfully if no updates found
- Document incomplete research if rate-limited
