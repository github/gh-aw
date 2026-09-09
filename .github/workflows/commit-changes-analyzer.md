---
private: true
emoji: "📊"
name: Commit Changes Analyzer
description: Analyzes and provides a comprehensive developer-focused report of all changes in the repository since a specified commit
on:
  workflow_dispatch:
    inputs:
      commit_url:
        description: 'GitHub commit URL to analyze changes since (e.g., https://github.com/owner/repo/commit/abc123)'
        required: true
        type: string
max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
max-turns: 100
model: openai/gpt-5.3-codex
engine:
  id: codex
  model-provider: openai
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [default]
  bash:
    - "*"
  edit:
safe-outputs:
  create-discussion:
    expires: 1d
    category: "dev"
    max: 1
timeout-minutes: 30
imports:
  - shared/reporting.md


  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: changes_analyzed
    question: Did the agent analyze all repository changes since the specified commit and produce a comprehensive report?
  - id: discussion_created
    question: Was a discussion or report created summarizing the changes since the specified commit?
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Commit Changes Analyzer

Analyze and provide a comprehensive description of all changes in the repository since a given commit.

## Mission

Generate a detailed developer-focused report analyzing all changes in the repository since the commit specified in the input URL.

## Context

- **Repository**: ${{ github.repository }}
- **Commit URL**: ${{ github.event.inputs.commit_url }}
- **Triggered by**: ${{ github.actor }}

## Task

Your task is to analyze all changes since the specified commit and create a comprehensive report for developers on the team.

### 1. Extract Commit SHA from URL

Parse the commit URL provided in the input to extract:
- Repository owner and name (validate it matches current repo)
- Commit SHA

The URL format is typically: `https://github.com/OWNER/REPO/commit/SHA`

### 2. Validate the Commit

Before proceeding, verify:
- The commit SHA exists in the repository
- The repository in the URL matches the current repository
- The commit is an ancestor of the current HEAD (can trace history from current to that commit)

Use bash commands like:
```bash
# Verify commit exists
git cat-file -t <SHA>

# Check if commit is ancestor
git merge-base --is-ancestor <SHA> HEAD
```

### 3. Analyze Changes

Collect comprehensive information about all changes since the specified commit:

#### File Changes
- **Files added**: List all new files with brief description of purpose
- **Files modified**: List changed files with summary of modifications
- **Files deleted**: List removed files
- **Files renamed/moved**: Track file movements
- **Binary files changed**: Note any binary file changes

Use commands like:
```bash
# Get list of changed files with status
git diff --name-status <SHA>..HEAD

# Get detailed statistics
git diff --stat <SHA>..HEAD

# Get number of commits
git rev-list --count <SHA>..HEAD
```

#### Commit Analysis
- **Number of commits** since the specified commit
- **Commit authors** and their contribution counts
- **Commit timeline**: First and most recent commit dates
- **Commit messages**: Extract key themes and patterns

Use commands like:
```bash
# List commits with authors
git log --pretty=format:"%h - %an, %ar : %s" <SHA>..HEAD

# Count commits by author
git shortlog -s -n <SHA>..HEAD

# Get commit timeline
git log --pretty=format:"%ai" <SHA>..HEAD | head -1  # Most recent
git log --pretty=format:"%ai" <SHA>..HEAD | tail -1  # Oldest in range
```

#### Code Impact Analysis
- **Lines added**: Total lines of code added
- **Lines removed**: Total lines of code removed
- **Net change**: Overall code delta
- **Language breakdown**: Changes by file type/language
- **Largest changes**: Files with most modifications

Use commands like:
```bash
# Detailed diff statistics
git diff --numstat <SHA>..HEAD

# Count by file extension
git diff --name-only <SHA>..HEAD | sed 's/.*\.//' | sort | uniq -c | sort -rn
```

#### Functional Areas Affected
Analyze which parts of the codebase were touched:
- **Package/module changes**: Which packages/directories had changes
- **Configuration changes**: Any config file updates
- **Documentation changes**: README, docs, comments
- **Test changes**: New or modified tests
- **Build/CI changes**: Workflow, Makefile, build script changes

### 4. GitHub Integration Analysis

Use GitHub tools to enrich the analysis:
- **Associated Pull Requests**: Find PRs that include commits in this range
- **Issues referenced**: Extract issue numbers from commit messages
- **Release context**: Check if any releases occurred in this range

Example GitHub tool usage:
```
Use list_commits to get commit details
Use search_issues or search_pull_requests to find related items
Use list_releases to check for releases in the timeframe
```

### 5. Generate Developer Report

Create a comprehensive markdown report with the following sections:

#### Executive Summary
- Brief overview of the change scope
- Time period covered
- Number of commits and authors involved
- High-level impact assessment

#### Detailed Changes

**Files Changed Summary**
- Breakdown by change type (added/modified/deleted/renamed)
- Statistics table with counts and percentages

**Code Impact**
- Lines added/removed/changed
- Net code growth/reduction
- Language/file type breakdown

**Commit History**
- Total commits in range
- Top contributors with commit counts
- Timeline (date range)
- Commit message themes/patterns

**Functional Areas**
- List of affected packages/modules
- Configuration changes
- Documentation updates
- Test coverage changes
- CI/CD modifications

**Notable Changes**
- Largest file changes (top 10)
- New files of significance
- Deleted files worth noting
- Breaking changes or major refactors

**Related Work**
- Associated pull requests (if found)
- Referenced issues
- Related releases

#### Developer Notes
- Potential migration concerns
- Breaking changes to be aware of
- New dependencies or tools introduced
- Recommended review areas for code reviewers

### 6. Output Format

Create a GitHub discussion with:
- **Title**: "Changes Analysis: Since commit [short-SHA] - [current date]"
- **Category**: "dev" (for development discussions)
- **Body**: Your complete analysis report in well-formatted markdown

Use proper markdown formatting:
- Tables for statistics
- Code blocks for examples
- Bullet lists for file changes
- Emphasis for important items
- Links to commits, PRs, issues where relevant

## Guidelines

- **Be thorough**: This is for developers who need detailed information
- **Be accurate**: Verify all data before including it
- **Be organized**: Use clear sections and formatting
- **Be actionable**: Highlight things developers need to know
- **Include context**: Don't just list changes, explain their significance
- **Handle errors gracefully**: If the commit URL is invalid or commit doesn't exist, explain the issue clearly
- **Use relative references**: When mentioning commits, include both short SHA and subject line
- **Link to GitHub**: Include links to relevant commits, PRs, files when helpful

## Security

- Validate that the commit SHA from the URL is a valid git SHA format
- Ensure the repository in the URL matches the current repository
- Don't execute any code files during analysis
- Focus on metadata and diffs, not file contents unless relevant

## Examples of Good Analysis

When describing a commit:
- ✅ `abc1234 - Refactor parser to use streaming approach (reduces memory by 40%)`
- ❌ `abc1234 - parser changes`

When listing files:
- ✅ `pkg/parser/stream.go - New streaming parser implementation to handle large files`
- ❌ `pkg/parser/stream.go - added`

When describing impact:
- ✅ `Breaking change: CLI flag --output renamed to --format (affects all users)`
- ❌ `CLI changes made`

## Error Handling

If any of these conditions occur, explain clearly in the discussion:
- Invalid commit URL format
- Commit SHA not found in repository
- Repository mismatch between URL and current repo
- Commit is not an ancestor of HEAD
- No commits found in the range (commit is already at HEAD)

Make the error message helpful so the user knows how to correct the input.


### Output Format

Wrap long content with `<details><summary><b>View Details</b></summary>...</details>`.