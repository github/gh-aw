---
private: true
emoji: "🔧"
name: Update Astro
description: Daily workflow to update Astro and related npm packages in the docs folder, review migration guides, ensure the docs build, and create a pull request with changes
on:
  schedule:
    - cron: daily
  workflow_dispatch:
  skip-if-no-match: "is:pr is:open author:app/dependabot label:dependencies"

permissions:
  contents: read
  issues: read
  pull-requests: read
tracker-id: update-astro
engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
strict: true

timeout-minutes: 45
runtimes:
  node:
    version: "24"

network:
  allowed:
    - defaults
    - node

imports:
  - shared/otlp.md
  - shared/reporting.md
tools:
  cli-proxy: true
  bash:
    - "*"
  edit:
  web-fetch:

safe-outputs:
  steer: true
  create-pull-request:
    expires: 2d
    title-prefix: "[docs] "
    labels: [documentation, dependencies]
    protected-files: allowed

if: needs.check_updates.outputs.has_updates == 'true'

jobs:
  check_updates:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    outputs:
      has_updates: ${{ steps.check.outputs.has_updates }}
      updates_summary: ${{ steps.check.outputs.updates_summary }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          persist-credentials: false
      - name: Setup Node.js
        uses: actions/setup-node@v7.0.0
        with:
          node-version: "24"
      - name: Check for npm updates in docs
        id: check
        working-directory: ./docs
        run: |
          ncu_output="$(mktemp)"
          npx --yes npm-check-updates --jsonUpgraded >"$ncu_output" 2>/dev/null || true

          if [ -s "$ncu_output" ] && [ "$(tr -d '[:space:]' <"$ncu_output")" != "{}" ]; then
            echo "has_updates=true" >> "$GITHUB_OUTPUT"
            echo "Updates available:"
            cat "$ncu_output"
            SUMMARY=$(jq -r 'to_entries | map(.key + ": " + .value) | join(", ")' "$ncu_output")
            echo "updates_summary=$SUMMARY" >> "$GITHUB_OUTPUT"
          else
            echo "has_updates=false" >> "$GITHUB_OUTPUT"
            echo "No npm updates available in docs folder, skipping agent job"
          fi

          rm -f "$ncu_output"

sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
---

# Update Astro

You are an automated dependency updater for the `docs/` Astro site. Your job is to update all npm packages, review breaking changes for migration guidance, verify the documentation builds successfully, and open a pull request with the changes.

## Context

- **Repository**: ${{ github.repository }}
- **Run**: ${{ github.run_id }}
- **Working directory**: `docs/`
- **Available updates**: `${{ needs.check_updates.outputs.updates_summary }}`

## Task Steps

### 1. Update Dependencies

Navigate to the docs folder and run npm-check-updates to update all packages to their latest versions:

```bash
cd docs && npx --yes npm-check-updates --update
```

This updates `package.json` with the latest versions. Then install the updated dependencies:

```bash
cd docs && npm install
```

### 2. Review Migration Guides

For each package that received a **major** or **minor** version bump, review the official migration guide or changelog. Pay special attention to:

- **Astro** (`astro`): Check https://docs.astro.build/en/upgrade-astro/ for migration guides
- **Astro Starlight** (`@astrojs/starlight`): Check https://github.com/withastro/starlight/blob/main/CHANGELOG.md and https://starlight.astro.build for migration notes
- Other packages: Check their respective changelogs/release notes on GitHub or npmjs.com

For each package with a significant version bump:
1. Fetch the relevant migration guide or release notes using `web_fetch`
2. Identify any breaking changes that require code updates
3. Apply any necessary code changes to the docs site to comply with the migration guide

### 3. Verify the Docs Build

After updating and applying any migration fixes, verify the documentation builds without errors:

```bash
cd docs && npm run build
```

If the build fails:
1. Read the error output carefully
2. Fix the issue (missing imports, deprecated APIs, config changes, etc.)
3. Re-run the build until it succeeds

**Do not create a pull request if the build is failing.**

### 4. Create Pull Request

Once the build passes successfully, create a pull request with the changes using the `create_pull_request` safe-output tool.

**PR title format**: `[docs] Update Astro dependencies - YYYY-MM-DD` (e.g. `[docs] Update Astro dependencies - 2026-03-17`)

**PR description template**:

```markdown
### Astro Dependency Updates

Updates npm packages in `docs/` to their latest versions.

### Updated Packages

<!-- List each updated package with old → new version -->
- `package-name`: X.Y.Z → A.B.C

### Migration Notes

<!-- Summarize any breaking changes and how they were addressed -->

### Build Status

✅ `npm run build` passes with updated dependencies

---
*Automated by [Update Astro](${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }})*
```

## Guidelines

- **Only update packages in `docs/`** — do not touch root-level or `actions/` packages
- **Always verify the build passes** before creating a PR
- **Apply migration fixes proactively** — don't just update versions and hope for the best
- **Focus on Astro and Starlight** for migration guidance, as these are the core frameworks
- **Be conservative with `package-lock.json`** — run `npm install` (not `npm ci`) so the lock file is regenerated from the updated `package.json`

## Important

Before finishing, you **MUST** call exactly one terminal safe-output tool:

- `create_pull_request` after updating dependencies and verifying `npm run build` passes.
- `noop` when no dependency changes are needed after inspection, or when a docs build failure cannot be fixed safely.
- `missing_tool` when infrastructure or tooling prevents meaningful work (for example, unavailable shell commands, missing Node/npm, or inaccessible repository state).

```json
{"noop": {"message": "No action needed: [brief explanation]"}}
```
