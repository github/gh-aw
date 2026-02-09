---
title: Dependabot Support
description: Automatic dependency manifest generation for tracking runtime dependencies in agentic workflows, enabling Dependabot to detect and update outdated tools.
sidebar:
  order: 750
---

The `gh aw compile --dependabot` command automatically generates dependency manifests for runtime tools used in your workflows, enabling GitHub Dependabot to monitor and update outdated dependencies.

## Overview

When workflows use runtime tools like `npx`, `pip install`, or `go install`, these dependencies should be tracked to ensure security updates and compatibility. The `--dependabot` flag analyzes your compiled workflows and generates appropriate manifest files.

**Key features:**
- Automatic detection of npm, pip, and Go dependencies from workflow commands
- Generation of ecosystem-specific manifests (`package.json`, `requirements.txt`, `go.mod`)
- Creation and update of `.github/dependabot.yml` configuration
- Merge-friendly updates that preserve existing configurations
- Lock file generation for reliable dependency resolution

## How It Works

The compiler scans all workflows for runtime dependency commands:

1. **Dependency extraction**: Identifies `npx`, `pip install`, and `go install` commands
2. **Version parsing**: Extracts package names and version specifiers
3. **Manifest generation**: Creates or updates ecosystem manifests in `.github/workflows/`
4. **Dependabot configuration**: Updates `.github/dependabot.yml` with detected ecosystems
5. **Lock file creation**: Generates lockfiles (e.g., `package-lock.json`) for version pinning

## Usage

### Basic Command

Compile all workflows and generate dependency manifests:

```bash
gh aw compile --dependabot
```

This command:
- Compiles all workflow markdown files to `.lock.yml`
- Scans workflows for runtime dependencies
- Generates manifests in `.github/workflows/`
- Creates or updates `.github/dependabot.yml`

### Command Requirements

> [!IMPORTANT]
> The `--dependabot` flag requires compiling **all workflows** and cannot be used with:
> - Specific workflow files (e.g., `gh aw compile my-workflow --dependabot` is invalid)
> - Custom workflow directories (`--dir` flag)

This ensures accurate dependency tracking across your entire workflow collection.

### Prerequisites

**For npm dependencies:**
- Node.js and npm must be installed
- Used to generate `package-lock.json` via `npm install --package-lock-only`
- If npm is unavailable, only `package.json` is created (with a warning)

**For pip and Go:**
- No additional tools required
- Manifests are generated directly

## Generated Files

### npm Dependencies

When workflows use `npx` commands (e.g., `npx playwright@latest`):

**`.github/workflows/package.json`**
```json
{
  "name": "gh-aw-workflows-deps",
  "private": true,
  "license": "MIT",
  "dependencies": {
    "@playwright/test": "1.41.0",
    "@sentry/mcp-server": "0.27.0"
  }
}
```

**`.github/workflows/package-lock.json`**
- Generated automatically via `npm install --package-lock-only`
- Locks transitive dependencies for reproducibility
- Required for Dependabot updates

### pip Dependencies

When workflows use `pip install` commands:

**`.github/workflows/requirements.txt`**
```txt
pandas==2.0.0
requests>=2.28.0
```

### Go Dependencies

When workflows use `go install` commands:

**`.github/workflows/go.mod`**
```go
module github.com/github/gh-aw-workflows-deps

go 1.21

require (
	github.com/spf13/cobra v1.8.0
	golang.org/x/tools v0.17.0
)
```

### Dependabot Configuration

**`.github/dependabot.yml`**
```yaml
version: 2
updates:
  - package-ecosystem: npm
    directory: /.github/workflows
    schedule:
      interval: weekly
  - package-ecosystem: pip
    directory: /.github/workflows
    schedule:
      interval: weekly
  - package-ecosystem: gomod
    directory: /.github/workflows
    schedule:
      interval: weekly
```

The configuration is created or merged with existing settings. Only missing ecosystems are added.

## Dependabot Pull Requests

Once manifests are generated, Dependabot monitors dependencies and opens PRs when updates are available.

### Example Dependabot PR

Dependabot creates PRs like [#13785](https://github.com/github/gh-aw/pull/13785) that update specific dependencies:

```diff
{
  "dependencies": {
-   "@playwright/test": "1.41.0",
+   "@playwright/test": "1.42.0"
  }
}
```

> [!WARNING]
> **Do not merge Dependabot PRs that only modify manifest files.** These changes will be overwritten on the next compilation. Always update the source workflow instead.

### Proper Fix Workflow

When Dependabot opens a PR:

1. **Identify affected workflows**: Check which workflows use the outdated dependency
2. **Update workflow markdown**: Edit the workflow's `.md` file to use the new version
3. **Recompile workflows**: Run `gh aw compile --dependabot` to regenerate manifests
4. **Verify changes**: Confirm the PR's manifest changes now match your compilation
5. **Dependabot auto-closes**: Dependabot automatically closes its PR when the base branch is updated

**Example fix for `@playwright/test` update:**

```bash
# 1. Find workflows using the outdated package
grep -r "@playwright/test@1.41.0" .github/workflows/*.md

# 2. Edit the workflow markdown file
# Change: npx @playwright/test@1.41.0
# To:     npx @playwright/test@1.42.0

# 3. Recompile to regenerate manifests
gh aw compile --dependabot

# 4. Commit and push
git add .github/workflows/
git commit -m "chore: update @playwright/test to 1.42.0"
git push
```

## Generic Prompt for Dependabot PRs

Use this prompt template when asking an AI agent to handle Dependabot updates:

```markdown
A Dependabot PR has been opened updating dependencies in .github/workflows/.

The proper way to address this is:

1. Identify which workflow markdown files (.md) reference the outdated dependency
2. Update the version in those workflow files
3. Run `gh aw compile --dependabot` to regenerate manifest files
4. Verify the generated manifests match the Dependabot PR's proposed changes
5. Commit and push the changes (Dependabot will auto-close its PR)

Affected PR: [link to Dependabot PR]
Updated dependency: [package name and version]

Please update the workflows and regenerate the manifests.
```

**Why this works:**
- Ensures source workflows stay in sync with manifest files
- Prevents merge conflicts from editing generated files
- Allows Dependabot to track that the update has been applied
- Maintains the single source of truth (workflow markdown files)

## Merge Behavior

When manifests already exist:

**package.json**: Merges dependencies, preserving existing packages
```json
// Existing: {"lodash": "4.17.0"}
// New:      {"express": "4.18.0"}
// Result:   {"lodash": "4.17.0", "express": "4.18.0"}
```

**requirements.txt**: Merges and sorts all dependencies
```txt
# Existing: requests==2.28.0
# New:      pandas==2.0.0
# Result:   pandas==2.0.0
#           requests==2.28.0
```

**go.mod**: Preserves module declaration and go version, replaces dependencies
```go
// Existing: module path and go 1.21 preserved
// Dependencies replaced with new set
```

**dependabot.yml**: Adds missing ecosystems, preserves existing entries
```yaml
# Existing: pip entry preserved
# New:      npm entry added
# Result:   Both entries present
```

## Troubleshooting

### npm install fails

**Symptom**: `package-lock.json` not generated, compilation succeeds with warning

**Solutions**:
- Install Node.js and npm: `https://nodejs.org/`
- Check npm version: `npm --version`
- Manually run: `cd .github/workflows && npm install --package-lock-only`

### Dependency not detected

**Symptom**: Expected package missing from manifest

**Possible causes**:
- Command uses shell variables or expressions: `npx ${TOOL}@${VERSION}` (not parseable)
- Command in job that's conditionally skipped
- Dependency in imported workflow (imports not yet supported)

**Workarounds**:
- Use literal package names and versions
- Manually add to manifest and lock file

### Dependabot doesn't open PRs

**Checklist**:
- `.github/dependabot.yml` exists and is valid YAML
- Manifest files exist in `.github/workflows/`
- Lock files are present (e.g., `package-lock.json`)
- Dependabot is enabled in repository settings
- Check Dependabot logs: Repository → Insights → Dependency graph → Dependabot

## Best Practices

**Run after workflow changes**: Regenerate manifests whenever you add or update runtime dependencies in workflows.

**Commit manifest files**: Both source manifests and lock files should be version controlled.

**Use version pinning**: Specify exact versions in workflows (e.g., `@playwright/test@1.41.0`) for predictable builds.

**Automate with CI**: Add compilation check to ensure manifests stay in sync:
```yaml
- name: Check dependencies
  run: |
    gh aw compile --dependabot
    git diff --exit-code .github/workflows/package.json
```

**Review Dependabot PRs**: Don't auto-merge. Test the workflow with updated dependencies first.

## Related Documentation

- [CLI Commands](/gh-aw/setup/cli/#compile) - Complete compile command reference
- [Compilation Process](/gh-aw/reference/compilation-process/) - How compilation works
- [GitHub Dependabot Docs](https://docs.github.com/en/code-security/dependabot) - Official Dependabot guide
