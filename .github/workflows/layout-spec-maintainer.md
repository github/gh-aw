---
name: Layout Specification Maintainer
description: Maintains scratchpad/layout.md with patterns of file paths, folder names, and artifact names used in lock.yml files
on:
  schedule:
    # Daily at 7am UTC on weekdays only
    - cron: "0 7 * * 1-5"
  workflow_dispatch:

# Minimal permissions: only what's needed for the workflow
# - contents: read - to read repository files and analyze workflows
# - issues: read - required by GitHub toolsets (default includes issues)
# - pull-requests: read - required by GitHub toolsets (default includes pull_requests)
# Note: PR creation handled by safe-outputs job with its own write permissions
permissions:
  contents: read
  issues: read
  pull-requests: read

tracker-id: layout-spec-maintainer
engine: copilot
strict: true

cache:
  - key: layout-spec-cache-${{ github.run_id }}
    path: /tmp/gh-aw/layout-cache
    restore-keys: |
      layout-spec-cache-

network:
  allowed:
    - defaults
    - github

safe-outputs:
  create-pull-request:
    expires: 2d
    title-prefix: "[specs] "
    labels: [documentation, automation]
    draft: false

tools:
  github:
    toolsets: [default]
  edit:
  bash:
    - "find .github/workflows -name '*.lock.yml'"
    - "yq '.*' .github/workflows/*.lock.yml"
    - "grep -r '.*' pkg/workflow/js/"
    - "grep -r '.*' pkg/workflow/*.go"
    - "git status"
    - "git diff scratchpad/layout.md"
    - "cat scratchpad/layout.md"

timeout-minutes: 20

imports:
  - shared/mood.md
---

# Layout Specification Maintainer

You are an AI agent that maintains a comprehensive specification file documenting all patterns of file paths, folder names, and artifact names used in the compiled lock.yml files in this repository.

## Your Mission

Scan all `.lock.yml` files in `.github/workflows/` and analyze Go and JavaScript source code to extract patterns, then maintain an up-to-date specification document at `scratchpad/layout.md`.

## Task Steps

### 1. Scan Lock Files for Patterns

Start by finding all lock files:

```bash
find .github/workflows -name "*.lock.yml" | wc -l
```

For each lock file, extract the following patterns using `yq`:

**Action Uses Patterns** (GitHub Actions being used):
```bash
yq '.jobs.*.steps[].uses' .github/workflows/*.lock.yml | grep -v "^---$" | grep -v "^null$" | sort -u
```

**Artifact Names** (uploaded/downloaded artifacts):
```bash
yq '.jobs.*.steps[] | select(.uses | contains("upload-artifact")) | .with.name' .github/workflows/*.lock.yml | grep -v "^---$" | grep -v "^null$" | sort -u
yq '.jobs.*.steps[] | select(.uses | contains("download-artifact")) | .with.name' .github/workflows/*.lock.yml | grep -v "^---$" | grep -v "^null$" | sort -u
```

**Job Names** (common job patterns):
```bash
yq '.jobs | keys' .github/workflows/*.lock.yml | grep -v "^---$" | sort -u
```

**File Paths Referenced** (paths in checkout, setup steps, etc.):
```bash
yq '.jobs.*.steps[].with.path' .github/workflows/*.lock.yml | grep -v "^---$" | grep -v "^null$" | sort -u
```

**Working Directory Patterns**:
```bash
yq '.jobs.*.steps[]."working-directory"' .github/workflows/*.lock.yml | grep -v "^---$" | grep -v "^null$" | sort -u
```

### 2. Review Go Code Patterns

Search Go files in `pkg/workflow/` for common patterns:

**Artifact name constants**:
```bash
grep -h "artifact" pkg/workflow/*.go | grep -E "(const|var|string)" | head -20
```

**File path patterns**:
```bash
grep -h '".github' pkg/workflow/*.go | grep -v "//" | head -20
grep -h '"pkg/' pkg/workflow/*.go | grep -v "//" | head -20
```

**Folder references**:
```bash
grep -rh "filepath.Join" pkg/workflow/*.go | head -20
```

### 3. Review JavaScript Code Patterns

Search JavaScript files in `pkg/workflow/js/` for patterns:

**Artifact references**:
```bash
grep -h "artifact" pkg/workflow/js/*.cjs | head -20
```

**File path patterns**:
```bash
grep -h "path" pkg/workflow/js/*.cjs | grep -E "(const|let|var)" | head -20
```

### 4. Generate Markdown Specification

Create or update `scratchpad/layout.md` with a comprehensive table organized by category:

**Format**:

```markdown
# GitHub Actions Workflow Layout Specification

> Auto-generated specification documenting patterns used in compiled `.lock.yml` files.
> Last updated: [DATE]

## Overview

This document catalogs all file paths, folder names, artifact names, and other patterns used across our compiled GitHub Actions workflows (`.lock.yml` files).

## GitHub Actions

Common GitHub Actions used across workflows:

| Action | Description | Context |
|--------|-------------|---------|
| actions/checkout@[sha] | Checks out repository code | Used in almost all workflows for accessing repo content |
| actions/upload-artifact@[sha] | Uploads build artifacts | Used for agent outputs, patches, prompts, and logs |
| actions/download-artifact@[sha] | Downloads artifacts from previous jobs | Used in safe-output jobs and conclusion jobs |
| actions/setup-node@[sha] | Sets up Node.js environment | Used in workflows requiring npm/node |
| actions/github-script@[sha] | Runs GitHub API scripts | Used for GitHub API interactions |

## Artifact Names

Artifacts uploaded/downloaded between workflow jobs:

| Name | Description | Context |
|------|-------------|---------|
| agent-output | AI agent execution output | Contains the agent's response and analysis |
| patch | Git patch file for changes | Used by create-pull-request safe-output |
| prompt | Agent prompt content | Stored for debugging and audit purposes |
| mcp-logs | MCP server logs | Debug logs from Model Context Protocol servers |
| safe-outputs-config | Safe outputs configuration | Passed from agent to safe-output jobs |

## Common Job Names

Standard job names across workflows:

| Job Name | Description | Context |
|----------|-------------|---------|
| activation | Determines if workflow should run | Uses skip-if-match and other filters |
| agent | Main AI agent execution job | Runs the copilot/claude/codex engine |
| detection | Post-agent analysis job | Analyzes agent output for patterns |
| conclusion | Final status reporting job | Runs after all other jobs complete |
| create_pull_request | Creates PR from agent changes | Safe-output job for PR creation |
| add_comment | Adds comment to issue/PR | Safe-output job for commenting |

## File Paths

Common file paths referenced in workflows:

| Path | Description | Context |
|------|-------------|---------|
| .github/workflows/ | Workflow definition directory | Contains all .md and .lock.yml files |
| .github/aw/ | Agentic workflow configuration | Contains actions-lock.json and other configs |
| pkg/workflow/ | Workflow compilation code | Go package for compiling workflows |
| pkg/workflow/js/ | JavaScript runtime code | CommonJS modules for GitHub Actions |
| scratchpad/ | Specification documents | Documentation and specs directory |

## Folder Patterns

Key directories used across the codebase:

| Folder | Description | Context |
|--------|-------------|---------|
| .github/workflows/ | Workflow files (source and compiled) | Primary location for all workflows |
| .github/workflows/shared/ | Shared workflow components | Reusable workflow imports |
| pkg/cli/ | CLI command implementations | gh-aw command handlers |
| pkg/parser/ | Markdown frontmatter parsing | Schema validation and parsing |
| pkg/workflow/js/ | JavaScript bundles | MCP servers, safe-output handlers |

## Constants and Patterns

Patterns found in Go and JavaScript code:

### Go Constants
[List extracted Go constants related to paths, artifacts, folders]

### JavaScript Patterns
[List extracted JavaScript patterns from .cjs files]

## Usage Guidelines

- **Artifact naming**: Use descriptive hyphenated names (e.g., `agent-output`, `mcp-logs`)
- **Job naming**: Use snake_case for job names (e.g., `create_pull_request`)
- **Path references**: Use relative paths from repository root
- **Action pinning**: Always pin actions to full commit SHA for security

---

*This document is automatically maintained by the Layout Specification Maintainer workflow.*
```

### 5. Detect Changes and Create PR

After generating the specification:

```bash
git status
```

Check if `scratchpad/layout.md` was created or modified.

If changes detected:

```bash
git diff scratchpad/layout.md
```

Review the changes to ensure they're accurate.

### 6. Create Pull Request

If `scratchpad/layout.md` has changes, use the **create-pull-request** safe-output:

**PR Title**: `[specs] Update layout specification - [DATE]`

**PR Body**:
```markdown
## Layout Specification Update

This PR updates `scratchpad/layout.md` with the latest patterns extracted from compiled workflow files.

### What Changed

[Summarize the key changes, such as:]
- Added X new action patterns
- Updated artifact names list
- Added Y new file path references
- Refreshed job name patterns

### Extraction Summary

- **Lock files analyzed**: [count]
- **Actions cataloged**: [count]
- **Artifacts documented**: [count]
- **Job patterns found**: [count]
- **File paths listed**: [count]

### Source Analysis

- Scanned all `.lock.yml` files in `.github/workflows/`
- Reviewed Go code in `pkg/workflow/`
- Reviewed JavaScript code in `pkg/workflow/js/`

---

*Auto-generated by Layout Specification Maintainer workflow*
```

### 7. Use Cache Memory

Use the cache to remember successful search strategies:

- Store patterns that were found and their extraction commands
- Remember which yq queries worked best
- Cache the list of common patterns to look for
- Store optimization strategies for next run

This helps improve efficiency over time and avoids re-discovering the same patterns.

## Important Guidelines

1. **Be thorough**: Scan ALL lock.yml files, not just a sample
2. **Extract real data**: Don't make up patterns - extract from actual files
3. **Provide context**: For each pattern, explain where and why it's used
4. **Organize clearly**: Use tables for easy reading and reference
5. **Include counts**: Show how many files, actions, artifacts were found
6. **Update date**: Always include the current date in the document
7. **Cache learnings**: Store successful strategies in cache-memory
8. **Deduplication**: Remove duplicates from extracted patterns
9. **Sort alphabetically**: Keep lists organized and easy to scan
10. **Real SHA values**: When listing actions, use actual commit SHAs found

## Success Criteria

- `scratchpad/layout.md` exists and is up-to-date
- All major patterns are documented
- Tables are complete and well-formatted
- PR is created when changes are detected
- Cache helps improve performance over time
- Document is useful as a reference for developers

Good luck maintaining our layout specification!
