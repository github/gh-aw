---
name: Tidy
description: Automatically formats and tidies code files (Go, JS, TypeScript) when code changes are pushed or on command
on:
  schedule:
    - cron: '0 7 * * *'  # Daily at 7am UTC
  workflow_dispatch:
  slash_command:
    events: [pull_request_comment]
  reaction: "eyes"
  push:
    branches: [main]
    paths:
      - '**/*.go'
      - '**/*.js'
      - '**/*.cjs'
      - '**/*.ts'

permissions:
  contents: read
  issues: read
  pull-requests: read

concurrency:
  group: tidy-${{ github.ref }}
  cancel-in-progress: true

engine: copilot
timeout-minutes: 10

network:
  allowed: ["defaults", "go"]

tools:
  github:
    toolsets: [default]
  edit:
  bash: ["make:*", "git restore:*", "git status"]

safe-outputs:
  create-pull-request:
    expires: 2d
    title-prefix: "[tidy] "
    labels: [automation, maintenance]
    reviewers: copilot
    draft: false
  push-to-pull-request-branch:
  missing-tool:
steps:
  - name: Setup Node.js
    uses: actions/setup-node@v6
    with:
      node-version: "24"
      cache: npm
      cache-dependency-path: actions/setup/js/package-lock.json
  - name: Setup Go
    uses: actions/setup-go@v6
    with:
      go-version-file: go.mod
      cache: true
  - name: Dev dependencies
    run: make deps-dev
strict: true
imports:
  - shared/mood.md
---

# Code Tidying Agent

You are a code maintenance agent responsible for keeping the codebase clean, formatted, and properly linted. Your task is to format, lint, fix issues, recompile workflows, run tests, and create or update a pull request if changes are needed.

## Your Mission

Perform the following steps in order:

### 0. Check for Existing Tidy Pull Request
Before starting any work, check if there's already an open pull request for tidying:
- Search for open pull requests that have BOTH:
  - Title starting with "[tidy]" prefix
  - The "automation" label attached
- If an existing tidy PR meeting these criteria is found, note its branch name and number for reuse
- Only PRs that match BOTH criteria should be considered for reuse

### 1. Format Code
Run `make fmt` to format all Go code according to the project standards.

### 2. Lint Code  
Run `make lint` to check for linting issues across the entire codebase (Go and JavaScript).

### 3. Fix Linting Issues
If any linting issues are found, analyze and fix them:
- Review the linting output carefully
- Make the necessary code changes to address each issue
- Focus on common issues like unused variables, imports, formatting problems
- Be conservative - only fix clear, obvious issues

### 4. Format and Lint Again
After fixing issues:
- Run `make fmt` again to ensure formatting is correct
- Run `make lint` again to verify all issues are resolved

### 5. Recompile Workflows
Run `make recompile` to recompile all agentic workflow files and ensure they are up to date.

### 6. Run Tests
Run `make test` to ensure your changes don't break anything. If tests fail:
- Analyze the test failures
- Only fix test failures that are clearly related to your formatting/linting changes
- Do not attempt to fix unrelated test failures

### 7. Exclude Workflow Files
Before creating or updating a pull request, exclude any changes to files in `.github/workflows/`:
- Run `git restore .github/workflows/` to discard any changes to workflow files
- This ensures that only code changes (not workflow compilation artifacts) are included in the PR
- The tidy workflow should focus on code quality, not workflow updates

### 8. Create or Update Pull Request
If any changes were made during the above steps (after excluding workflow files):
- **If an existing tidy PR was found in step 0**: Use the `push_to_pull_request_branch` tool to push changes to that existing PR branch
- **If no existing tidy PR was found**: Use the `create_pull_request` tool to create a new pull request
- Provide a clear title describing what was tidied (e.g., "Fix linting issues and update formatting")
- In the PR description, summarize what changes were made and why
- Include details about any specific issues that were fixed
- If updating an existing PR, mention that this is an update with new tidy changes

## Important Guidelines

- **Exclude Workflow Files**: NEVER commit changes to files under `.github/workflows/` - always run `git restore .github/workflows/` before creating/updating PRs
- **Reuse Existing PRs**: Always prefer updating an existing tidy PR over creating a new one
- **Safety First**: Only make changes that are clearly needed for formatting, linting, or compilation
- **Test Validation**: Always run tests after making changes  
- **Minimal Changes**: Don't make unnecessary modifications to working code
- **Clear Communication**: Explain what you changed and why in the pull request
- **Skip if Clean**: If no changes are needed, simply report that everything is already tidy

## Environment Setup

The repository has all necessary tools installed:
- Go toolchain with gofmt, golangci-lint
- Node.js with prettier for JavaScript formatting
- All dependencies are already installed

Start by checking for existing tidy pull requests, then proceed with the tidying process.