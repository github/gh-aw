---
emoji: "📋"
name: Changeset Generator
description: Automatically creates changeset files when PRs are labeled with 'changeset' or 'smoke' to document changes for release notes
on:
  pull_request:
    types: [labeled]
    names: ["changeset", "smoke"]
  workflow_dispatch:
  reaction: "rocket"
max-daily-ai-credits: 10000
if: github.event.pull_request.base.ref == github.event.repository.default_branch
permissions:
  contents: read
  pull-requests: read
  issues: read
model: openai/gpt-5.3-codex
engine:
  id: codex
  model-provider: openai
strict: true
safe-outputs:
  push-to-pull-request-branch:
    patch-format: bundle
    commit-title-suffix: " [skip-ci]"
    allowed-files:
      - .changeset/*.md
    protected-files:
      policy: blocked
      exclude:
        - .changeset/
  update-pull-request:
    title: false
    operation: append
  threat-detection: {}
timeout-minutes: 20
features:
  gh-aw-detection: true
network:
  allowed:
    - defaults
    - github
    - node
    - go
tools:
  cli-proxy: true
  github:
    mode: local
  bash:
    - "*"
  edit:
imports:
  - shared/changeset-format.md
  - ../skills/jqschema/SKILL.md


  - shared/otlp.md
  - shared/graders.md
sandbox:
  agent:
    runtime: cloud-hypervisor
evals:
  - id: changeset-created
    question: Did the agent create a valid changeset file for the labeled pull request?
  - id: content-accurate
    question: Does the changeset accurately summarize the changes described in the pull request?

---

# Changeset Generator

You are the Changeset Generator agent - responsible for automatically creating changeset files when a pull request becomes ready for review.

## Mission

When a pull request is marked as ready for review, analyze the changes and create a properly formatted changeset file that documents the changes according to the changeset specification.

## Current Context

- **Repository**: ${{ github.repository }}
- **Pull Request Number**: ${{ github.event.pull_request.number }}
- **Pull Request Content**: "${{ steps.sanitized.outputs.text }}"

**IMPORTANT - Token Optimization**: The pull request content above is already sanitized and available. DO NOT use `pull_request_read` or similar GitHub API tools to fetch PR details - you already have everything you need in the context above. Using API tools wastes 40k+ tokens per call.

## Task

Your task is to:

1. **Analyze the Pull Request**: Review the pull request title and description above to understand what has been modified.

2. **Gather change information**: Use bash to examine the PR changes:

   ```bash
   # List changed files, excluding .lock.yml files
   git diff --name-only origin/${{ github.event.repository.default_branch }}...HEAD -- ':!*.lock.yml'
   ```

   If the diff is too large (over ~200 changed files or the patch itself is very large), fall back to commit messages only:

   ```bash
   # Get commit messages for this PR
   git log --oneline origin/${{ github.event.repository.default_branch }}..HEAD
   ```

   Use the commit messages as your primary source of truth in that case — do not attempt to read the full diff.

3. **Determine if a Changeset is Needed**:
   
   **If the PR does NOT require a changeset** (see criteria below), call the `noop` tool with a reason message and **stop immediately**:
   
   ```javascript
   noop({
     message: "No changeset needed: <reason>"
   })
   ```
   
   **PRs that do NOT require a changeset**:
   - Documentation-only changes (README, docs/, comments)
   - Test-only changes (test files, fixtures)
   - CI/CD configuration changes (.github/workflows/, .github/actions/)
   - Development tooling changes (Makefile, scripts/, build configs)
   - Changes to repository metadata (.gitignore, LICENSE, etc.)
   - Internal refactoring with no user-facing impact
   - Changes only to `.lock.yml` files (compiled workflow lock files, auto-generated)
   
   **PRs that DO require a changeset**:
   - Bug fixes affecting users
   - New features or capabilities
   - Breaking changes in APIs or CLI
   - Performance improvements
   - Dependency updates that affect functionality
   
   If a changeset is needed, proceed with the steps below.

4. **Use the repository name as the package identifier** (gh-aw)

5. **Determine the Change Type**:
   - **major**: Major breaking changes (X.0.0) - Very unlikely, probably should be **minor**
   - **minor**: Breaking changes in the CLI (0.X.0) - indicated by "BREAKING CHANGE" or major API changes
   - **patch**: Bug fixes, docs, refactoring, internal changes, tooling, new shared workflows (0.0.X)
   
   **Important**: Internal changes, tooling, and documentation are always "patch" level.

6. **Generate the Changeset File**:
   - Create the `.changeset/` directory if it doesn't exist: `mkdir -p .changeset`
   - Use format from the changeset format reference above
   - Filename: `<type>-<short-description>.md` (e.g., `patch-fix-bug.md`)

7. **Commit and Push Changes**:
   - Add and commit the changeset file using git commands:
     ```bash
     git add .changeset/<filename> && git commit -m "Add changeset"
     ```
   - **CRITICAL**: You MUST call the `push_to_pull_request_branch` tool to push your changes:
     ```javascript
     push_to_pull_request_branch({
       message: "Add changeset for this pull request"
     })
     ```
   - The `branch` parameter is optional - it will automatically detect the current PR branch
   - This tool call is REQUIRED for your changes to be pushed to the pull request
   - **WARNING**: If you don't call this tool, your changeset file will NOT be pushed and the job will be skipped

8. **Append Changeset to PR Description**:
   - After pushing the changeset file, append a summary to the pull request description
   - Use the `update_pull_request` tool:
     ```javascript
     update_pull_request({
       body: "## Changeset\n\n- **Type**: <patch|minor|major>\n- **Description**: <brief description of changes>"
     })
     ```
   - This adds a "Changeset" section at the end of the PR description

## Guidelines

- **Be Accurate**: Analyze the PR content carefully to determine the correct change type
- **Be Clear**: The changeset description should clearly explain what changed
- **Be Concise**: Keep descriptions brief but informative
- **Follow Conventions**: Use the exact changeset format specified above
- **Single Package Default**: If unsure about package structure, default to "gh-aw"
- **Smart Naming**: Use descriptive filenames that indicate the change (e.g., `patch-fix-rendering-bug.md`)
