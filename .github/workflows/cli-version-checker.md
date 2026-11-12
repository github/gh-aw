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
model: copilot/gpt-5
strict: false
engine:
  id: pi
  model-provider: github
network: 
   allowed: [defaults, node, go, "api.github.com", containers]
imports:
  - shared/pr-safe-output-defaults.md
  - shared/lock-safe-pull-request.md
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
  steer: false
  create-pull-request:
    draft: true
    exclude-lock-yml: true
    title-prefix: "[ca] "
    labels: [automation, dependencies, cookie]
    close-older-pull-requests: true
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
2. If cached versions exist and are recent (< 24h), re-evaluate their publication timestamps against this run's cooldown cutoff before deciding whether updates are needed; cached "latest" values never bypass the cooldown
3. If no CLI version, Docker image version, or Docker image digest changes are detected, call `noop` as the terminal safe output and exit successfully

**CRITICAL**: If version or digest changes are detected, implement and validate them and create a pull request using `create_pull_request`, including minor updates. Report concrete blockers with `report_incomplete`.

For each CLI/MCP server and threat-detect:
1. Resolve the newest stable version eligible under the global 3-day release cooldown below (use npm view commands for package metadata)
2. Compare with the current version constant in `./pkg/constants/`
3. If newer version exists, research changes and prepare update

### Global 3-Day Release Cooldown

Apply this policy to **all release resolution**: every CLI, MCP server, threat-detect, and Docker image, including cached candidates and fallback sources.

1. Capture one UTC check time for the run and compute `cutoff = check_time - 259200 seconds` (72 hours).
2. Only select stable releases with a valid publication timestamp **at or before the cutoff**. A release exactly 72 hours old is eligible; one 71 hours 59 minutes old is not. Never use creation/modification time as a substitute for publication time.
3. If the newest release is too recent, search older stable releases and select the newest eligible version instead. Paginate GitHub release lists as needed; exclude drafts and prereleases. Never downgrade an existing pin.
4. For npm, fetch `npm view <package> versions time --json`, compare stable semantic versions, and check the selected version's `time[version]`. Do not resolve from the unfiltered `version` or `dist-tags.latest` value, or use the package-wide `created`/`modified` timestamps.
5. For GitHub releases, use the releases list API (`/repos/OWNER/REPO/releases?per_page=100`) and each candidate's `published_at`, not `/releases/latest`. Preserve each project's tag convention.
6. If a candidate's publication timestamp is missing, invalid, or in the future, do not select it. Report unavailable metadata with `report_incomplete`; do not guess an age or treat a failed lookup as "up to date".
7. Record the selected version, publication timestamp, check time, and cooldown decision in cache-memory and the pull request. When all newer releases are cooling down and no other eligible changes exist, save the deferred candidates and call `noop`.

### Version Sources
- **Claude Code**: Use `npm view @anthropic-ai/claude-code versions time --json` and apply the global cooldown
  - No public GitHub repository
- **Copilot CLI**: Use `npm view @github/copilot versions time --json` and apply the global cooldown
  - Repository: https://github.com/github/copilot-cli
  - **CRITICAL**: Always attempt to fetch and deeply analyze Copilot repository content
  - Release Notes: https://github.com/github/copilot-cli/releases
  - Changelog: https://github.com/github/copilot-cli/blob/main/CHANGELOG.md (or similar)
  - README: https://github.com/github/copilot-cli/blob/main/README.md
- **Codex**: Use `npm view @openai/codex versions time --json` and apply the global cooldown
  - Repository: https://github.com/openai/codex
  - Release Notes: https://github.com/openai/codex/releases
- **GitHub MCP Server**: `https://api.github.com/repos/github/github-mcp-server/releases?per_page=100` (apply the global cooldown)
  - Release Notes: https://github.com/github/github-mcp-server/releases
- **Playwright CLI**: Use `npm view @playwright/cli versions time --json` and apply the global cooldown
  - Repository: https://github.com/microsoft/playwright-cli
  - Package: https://www.npmjs.com/package/@playwright/cli
- **MCP Gateway**: `https://api.github.com/repos/github/gh-aw-mcpg/releases?per_page=100` (apply the global cooldown)
  - Repository: https://github.com/github/gh-aw-mcpg
  - Release Notes: https://github.com/github/gh-aw-mcpg/releases
  - Docker Image: `ghcr.io/github/gh-aw-mcpg:v{VERSION}`
  - Used as default sandbox.agent container (see `pkg/constants/constants.go`)
- **Pi**: Use `npm view @earendil-works/pi-coding-agent versions time --json` and apply the global cooldown
  - Package: https://www.npmjs.com/package/@earendil-works/pi-coding-agent
  - Constant: `DefaultPiVersion` in `pkg/constants/version_constants.go`
- **Threat-detect**: Check `https://api.github.com/repos/github/gh-aw-threat-detection/releases?per_page=100` (apply the global cooldown)
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
- Keep the version selected by the global cooldown; fallback metadata is for research, not permission to select an unfiltered latest release
- Compare CLI help outputs between versions
- Check for version changelog in package description

### Tool Installation & Discovery
Check cache-memory first (`/tmp/gh-aw/cache-memory/`). Only install and run `--help` if the version changed; then save outputs to cache.

For each CLI tool update, install (`npm install -g <package>@<version>`), run `--help` on the main command and key subcommands (Copilot: `config`, `environment`), and compare with the cached output to identify new flags, removed features, or behavior changes.

### Update Process
1. Edit the appropriate version constant in `./pkg/constants/` with cooldown-eligible CLI version(s). **Copilot CLI is special**: use the paired version/checksum refresh below instead of editing its constants by hand.
2. Edit `./pkg/cli/docker_images.go` with new Docker image version(s) and digest(s), including digest-only changes where the tag is unchanged
3. Run `make fmt` after editing any Go files
4. **REQUIRED**: Run `make recompile` in the **foreground** — do NOT background it with `&` or follow it with `sleep`. Wait for it to finish completely before proceeding. Example: `make recompile && echo "done"`.
5. Verify changes with `git status`
6. **REQUIRED**: Validate the updated pins, commit the intended changes locally, and call `create_pull_request` with detailed analysis and the local branch. Compiled workflow locks are excluded from delivery automatically.

### Copilot CLI Version and Checksum Refresh

After selecting a cooldown-eligible Copilot release, run `bash scripts/update-copilot-version.sh VERSION` (for example, `bash scripts/update-copilot-version.sh 1.0.103`). This offline resolver updates `DefaultCopilotVersion` and `DefaultCopilotChecksumsSHA256` together from the reviewed pin table in `actions/setup/sh/release_integrity.sh`. It does not discover releases or enforce the cooldown, so apply the global policy first.

If the release has no reviewed pin, first obtain an independently authenticated SHA256 for its `SHA256SUMS.txt`, then run:

```bash
bash scripts/update-release-pins.sh github/copilot-cli VERSION SHA256SUMS.txt TRUSTED_MANIFEST_SHA256
bash scripts/update-copilot-version.sh VERSION
```

Never derive `TRUSTED_MANIFEST_SHA256` solely from a manifest downloaded alongside the executable, bypass pin validation, or change only the version constant. If independent authentication is unavailable, leave the Copilot pins unchanged, report the blocker with `report_incomplete`, and continue with other eligible updates.

## Pull Request Format

**Follow the Report Structure Pattern defined in `shared/reporting.md`.**

For each updated CLI, include: version old → new, release timeline, changes categorized as Breaking/Features/Fixes/Security/Performance, impact assessment, changelog links, and any CLI/subcommand changes discovered via help output.

**Important**: Use h3 (###) or lower for all headers. Wrap full changelogs in `<details>` tags. Use plain URLs (no backticks) and convert `#1234` PR references to full external URLs like `https://github.com/owner/repo/pull/1234`.

## Guidelines
- Only update stable versions (no pre-releases) that pass the global 3-day release cooldown
- Prioritize security updates among eligible releases; security fixes do not bypass the cooldown
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

Resolve the newest cooldown-eligible stable release for each image from its GitHub repository:

| Constant | Current image | GitHub releases URL |
|---|---|---|
| `ActionlintImage` | `rhysd/actionlint:<version>@sha256:<digest>` | `https://api.github.com/repos/rhysd/actionlint/releases?per_page=100` |
| `SyftImage` | `anchore/syft:<version>@sha256:<digest>` | `https://api.github.com/repos/anchore/syft/releases?per_page=100` |
| `GrypeImage` | `anchore/grype:<version>@sha256:<digest>` | `https://api.github.com/repos/anchore/grype/releases?per_page=100` |
| `GrantImage` | `anchore/grant:<version>@sha256:<digest>` | `https://api.github.com/repos/anchore/grant/releases?per_page=100` |
| `ZizmorImage` | `ghcr.io/zizmorcore/zizmor:<version>@sha256:<digest>` | `https://api.github.com/repos/zizmorcore/zizmor/releases?per_page=100` |
| `PoutineImage` | `ghcr.io/boostsecurityio/poutine:<version>@sha256:<digest>` | `https://api.github.com/repos/boostsecurityio/poutine/releases?per_page=100` |
| `RunnerGuardImage` | `ghcr.io/vigilant-llc/runner-guard:<version>@sha256:<digest>` | `https://api.github.com/repos/vigilant-llc/runner-guard/releases?per_page=100` |
| `YamllintImage` | `pipelinecomponents/yamllint:<version>@sha256:<digest>` | `https://api.github.com/repos/PipelineComponents/yamllint/releases?per_page=100` |

**Optimization**: Fetch all GitHub release endpoints in parallel in a single turn.

### Docker Release Eligibility

Apply the global 3-day release cooldown before resolving any target tag, including an already-pinned version for a digest-only update. If the newest release is cooling down, select an older eligible stable release without downgrading the current pin. A release timestamp establishes the tag's release age, not the age of a republished digest; do not claim otherwise.

### Fetching the Container SHA

For every Docker image, fetch the registry digest for the cooldown-eligible target tag. Also fetch and compare the digest when the eligible version is unchanged so mutable or republished tags are reflected in `docker_images.go`.

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

1. Fetch release lists in parallel via `api.github.com`.
2. Apply the global 3-day release cooldown to every target tag, including digest-only updates; search older stable releases when newer ones are still cooling down.
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
npm view @github/copilot versions time --json | jq '{versions, time}'
```

## Error Handling
- **SAVE PROGRESS**: Before exiting on errors, save current state to cache-memory
- **RESUME ON RESTART**: Check cache-memory on startup to resume from where you left off
- Retry NPM registry failures once after 30s
- Continue if individual changelog fetch fails
- Skip PR creation if recompile fails
- Exit successfully if no updates found
- Document incomplete research if rate-limited
