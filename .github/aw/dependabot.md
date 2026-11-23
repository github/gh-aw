---
description: Instructions for fixing Dependabot PRs that update dependencies in generated workflow manifest files
disable-model-invocation: true
---

# Fixing Dependabot PRs for Agentic Workflow Dependencies

Update the Agentic Workflow Markdown sources first, then run
`gh aw compile --dependabot` to regenerate the workflow locks and dependency
manifests. For a workflow with `source:` frontmatter, use `gh aw update` to
bring in the fix from upstream instead of editing the local copy.

> [!WARNING]
> **Do not merge a Dependabot PR that changes only generated files** such as `.github/workflows/package.json`, `.github/workflows/requirements.txt`, or `.github/workflows/go.mod`. A manifest-only bump does not change the version installed by the workflow.

## Background

The `gh aw compile --dependabot` command scans all agentic workflow files (`.github/workflows/*.md`) for runtime tool dependencies and generates manifest files:

| Manifest | Ecosystem | Full Path |
|----------|-----------|-----------|
| `package.json` / `package-lock.json` | npm | `.github/workflows/package.json` / `.github/workflows/package-lock.json` |
| `requirements.txt` | pip | `.github/workflows/requirements.txt` |
| `go.mod` | Go | `.github/workflows/go.mod` |

When Dependabot opens PRs to update these dependencies, the fix must be applied to the **source `.md` workflow files**, not the generated manifests.

## Fix Strategy: Bundle Multiple PRs

Rather than fixing Dependabot PRs one by one, **bundle all pending fixes into a single commit**:

1. **Find all open Dependabot PRs** targeting the generated manifest files
2. **Identify the source `.md` files** for each dependency
3. **Apply all version updates** to the `.md` files in one pass
4. **Regenerate the manifests** with a single `gh aw compile --dependabot`
5. **Commit and push** — Dependabot will auto-close the resolved PRs

## Step-by-Step Instructions

### 1. List Open Dependabot PRs

Use GitHub tools to list all open Dependabot PRs:

```bash
gh pr list --author "app/dependabot" --state open
```

Filter for PRs affecting generated workflow manifests (title contains `Bump` or similar, files include `.github/workflows/package.json`, `.github/workflows/requirements.txt`, or `.github/workflows/go.mod`).

### 2. Identify Source `.md` Files

For each outdated dependency, find which workflow files reference it:

```bash
# For npm packages (e.g., @playwright/test)
grep -r "@playwright/test" .github/workflows/*.md .github/workflows/shared/

# For pip packages (e.g., requests)
grep -r "requests==" .github/workflows/*.md

# For Go packages
grep -r "golang.org/x/tools" .github/workflows/*.md
```

If the affected package is transitive, find the direct package declared in the
workflow or a shared import that brings it in. If the alert points to a
standalone lockfile that `gh aw compile --dependabot` does not generate, follow
that lockfile's own update procedure instead.

### 3. Update the Markdown Source

For a locally maintained workflow, edit the dependency declaration in its
`.md` file or shared import. For an upstream-managed workflow, refresh just the
affected workflow from the repository root:

```bash
gh aw update WORKFLOW_ID --no-compile
```

Replace `WORKFLOW_ID` with the Markdown filename without `.md`. `--no-compile`
lets you review the refreshed source before regenerating everything in step 4.
If the upstream version still uses the affected dependency, fix it upstream
first, then rerun `gh aw update` after the fix is available at the workflow's
source ref. Do not patch the local copy or its generated lock by hand.

For example, for a locally maintained workflow:

```bash
# Example: Update @playwright/test from 1.41.0 to 1.42.0
# Find:    npx @playwright/test@1.41.0
# Replace: npx @playwright/test@1.42.0
```

For **MCP server transitive dependencies**, update the shared MCP config:

```bash
# Locate the shared MCP configuration
grep -r "@sentry/mcp-server" .github/workflows/shared/

# Update the version in the args array:
# args: ["@sentry/mcp-server@0.27.0"] → args: ["@sentry/mcp-server@0.29.0"]
```

### 4. Regenerate Manifests

Once the Markdown sources contain the patched pins, run this from the
repository root without a workflow argument:

```bash
gh aw compile --dependabot
```

This recompiles workflows and regenerates the detected ecosystems' manifests
from their source declarations, including the npm lockfile when npm is available.
Ordinary compilation by `gh aw update` does not replace this step.

If `.github/workflows/package-lock.json` also needs updating:

```bash
(cd .github/workflows && npm install --package-lock-only --ignore-scripts)
```

### 5. Verify and Commit

Review the Markdown, compiled workflow locks, and generated manifests together.
Confirm that the workflow uses the updated direct package and that the resolved
dependency is outside the advisory's affected range. If compilation or dependency
resolution fails, fix the source pin or its compatibility constraints before
publishing; do not hand-edit generated files to make the diff look fixed.

```bash
# Review the changes
git diff .github/workflows/

# Stage and commit all dependency updates together
git add .github/workflows/ .github/aw/
git commit -m "chore: bundle dependabot dependency updates"
git push
```

After the fix lands on the default branch, verify the alert and PR status in
Dependabot.

## Bundling Decision Guide

**Bundle multiple PRs when:**
- ✅ Multiple Dependabot PRs target the same ecosystem (npm, pip, Go)
- ✅ PRs affect different workflows but update the same package
- ✅ All updates are minor or patch version bumps (low breaking-change risk)

**Handle separately when:**
- ⚠️ A PR involves a **major version bump** with potential breaking changes
- ⚠️ Different teams own different workflows with separate review requirements

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `.github/workflows/package-lock.json` not updated | Run `cd .github/workflows && npm install --package-lock-only` after compilation |
| Dependency not found in `.md` files | Check shared MCP configs in `.github/workflows/shared/` |
| Compilation fails after version update | Check if the new version has breaking API changes |
| Dependabot PR not auto-closing | Verify the exact version strings match; check for pre-release suffixes |

## Dismissed Dependabot Alerts and VEX

When a Dependabot security alert is dismissed with a substantive security reason (`not_used`, `inaccurate`, or `tolerable_risk`), consider generating a [VEX (Vulnerability Exploitability eXchange)](https://openvex.dev/) statement to record the assessment as a machine-readable OpenVEX v0.2.0 document in `.vex/<ghsa-id>.json`. Alerts dismissed as `no_bandwidth` do not represent a security decision and should not produce a VEX statement.

Learn about the OpenVEX format, purl construction, and dismissal-to-justification mappings from [openvex.dev](https://openvex.dev/) before generating statements.

## Related Documentation

- [Dependabot Support](/gh-aw/reference/dependabot/) — Full reference for `gh aw compile --dependabot`
- [OpenVEX Specification](https://openvex.dev/) — VEX standard for vulnerability exploitability exchange
- Local copy: @.github/aw/github-agentic-workflows.md
