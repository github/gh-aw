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
env:
  GH_AW_ANALYZED_END_SHA: ${{ github.sha }}
  GH_AW_CAMPAIGN_RUN_ATTEMPT: ${{ github.run_attempt }}
permissions:
  contents: read
  issues: read
  pull-requests: read
max-turns: 100
model: copilot/gpt-5.6-luna
engine:
  id: codex
  model-provider: github
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
experiments:
  storage: cache
  detail_level:
    variants: [comprehensive, brief]
    description: "Compare explanatory detail while preserving complete factual coverage"
    hypothesis: "H0: brief does not improve reviewer-ready rate. H1: brief improves it by 20 percentage points, from 0.60 to 0.80"
    metric: "eval:reviewer_ready"
    secondary_metrics:
      - "eval:detail_variant_visible"
      - ai_credits
      - run_duration_seconds
    guardrail_metrics:
      - name: "eval:changes_analyzed"
        direction: max
        threshold: 0.95
      - name: empty_output_rate
        direction: min
        threshold: 0.0
    min_samples: 82
    start_date: "2026-10-24"
    issue: 71628
evals:
  - id: changes_analyzed
    question: Did the agent analyze all repository changes since the specified commit and produce a comprehensive report?
  - id: discussion_created
    question: Was a discussion or report created summarizing the changes since the specified commit?
  - id: reviewer_ready
    question: "Does the report connect at least one changed file or commit, identified by a repository link, to a concrete developer implication and a specific recommended review action?"
  - id: detail_variant_visible
    question: "Does the report state its assigned detail level and follow that level in Developer Notes: brief uses at most one explanatory sentence per affected functional area, while comprehensive uses two or three?"
---

# Commit Changes Analyzer

Analyze and provide a comprehensive description of all changes in the repository since a given commit.

## Mission

Generate a detailed developer-focused report analyzing all changes in the repository since the commit specified in the input URL.

## Context

- **Repository**: ${{ github.repository }}
- **Commit URL**: ${{ github.event.inputs.commit_url }}
- **Triggered by**: ${{ github.actor }}
- **Analyzed end SHA (END_SHA)**: ${{ env.GH_AW_ANALYZED_END_SHA }}
- **Run ID / attempt**: ${{ github.run_id }} / ${{ env.GH_AW_CAMPAIGN_RUN_ATTEMPT }}

Analyze only the immutable range from the validated input commit (exclusive) to END_SHA (inclusive), never a moving branch tip. Resolve the input to its full SHA, verify END_SHA exists, and use these two SHAs for every diff, history query, statistic, and evidence link. Include both full SHAs, assigned detail level, run ID/attempt, and campaign #71628 in the report.

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
- The commit is an ancestor of END_SHA (can trace history from END_SHA to that commit)

Use bash commands like:
```bash
# Verify commit exists
git cat-file -t <SHA>

# Check if commit is ancestor
git merge-base --is-ancestor <SHA> <END_SHA>
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
git diff --name-status <SHA>..<END_SHA>

# Get detailed statistics
git diff --stat <SHA>..<END_SHA>

# Get number of commits
git rev-list --count <SHA>..<END_SHA>
```

#### Commit Analysis
- **Number of commits** since the specified commit
- **Commit authors** and their contribution counts
- **Commit timeline**: First and most recent commit dates
- **Commit messages**: Extract key themes and patterns

Use commands like:
```bash
# List commits with authors
git log --pretty=format:"%h - %an, %ar : %s" <SHA>..<END_SHA>

# Count commits by author
git shortlog -s -n <SHA>..<END_SHA>

# Get commit timeline
git log --pretty=format:"%ai" <SHA>..<END_SHA> | head -1  # Most recent
git log --pretty=format:"%ai" <SHA>..<END_SHA> | tail -1  # Oldest in range
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
git diff --numstat <SHA>..<END_SHA>

# Count by file extension
git diff --name-only <SHA>..<END_SHA> | sed 's/.*\.//' | sort | uniq -c | sort -rn
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

{{#if experiments.detail_level == 'brief' }}
State the assigned detail level as `brief` in the report.
Use at most one explanatory sentence per affected functional area in Developer Notes.
{{#else}}
State the assigned detail level as `comprehensive` in the report.
Use two or three explanatory sentences per affected functional area in Developer Notes.
{{#endif}}

Only explanatory detail in Developer Notes varies. Both levels must retain every required fact, table, file list, evidence link, validation step, and error-handling rule. Connect at least one repository-linked changed file or commit to a concrete developer implication and a specific recommended review action. Do not invent implications when the evidence is insufficient.

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
- Commit is not an ancestor of END_SHA
- No commits found in the range (commit is already at END_SHA)

Make the error message helpful so the user knows how to correct the input.


### Output Format

Wrap long content with `<details><summary><b>View Details</b></summary>...</details>`.

<!--
Campaign #71628 protocol (adopted 2026-10-24; no dispatch as part of adoption):
- Preserve the compiler's workflow-wide queued concurrency to serialize cache
  restore/pick/save. Omit weight for balanced least-used assignment.
- Before launch, verify uploaded experiment assignments.json and state.jsonl,
  usage/eval artifacts, and Actions attempt timing. Join by run ID AND attempt;
  the ledger's run_id alone does not distinguish retries. Archive evidence before
  artifact expiry. Record cache misses, eviction/reset, counter discontinuities,
  and a new assignment epoch rather than assuming continuity across resets.
- Freeze workflow/source and lock revision, harness version, evaluator model,
  question text, and rubric for both arms. reviewer_ready requires all three:
  a repository evidence link, a concrete implication, and a specific review action.
  detail_variant_visible requires the label AND the assigned sentence limit.
  Independently review an arm-balanced sample for judge consistency.
- Bind observed collector keys before launch: reviewer_ready,
  detail_variant_visible, and changes_analyzed use eval YES/NO/UNKNOWN records;
  credits require measured usage credits, never a token conversion; duration
  requires Actions attempt timestamps, not API update envelopes. empty_output_rate
  is missing/empty report bodies divided by valid, nonempty assigned ranges,
  using recorded output evidence. Missing evidence is unavailable, never zero.
  These metric names alone do not instrument collectors: keep launch blocked
  until every required observation and supported metric binding is verified.
- Quality analysis uses the first attempt of each unique repository/base/end
  range, selected without inspecting outcomes. Invalid or empty ranges are
  inapplicable, not successes; missing evals remain UNKNOWN and are reported.
  Retries and repeated ranges are not independent samples. Retain all assigned
  attempts, failures, and timeouts in cost, duration, and reliability reporting.
- Analyze once after 82 usable independent observations per arm; no early
  promotion. Report missingness/exclusions and assignment epochs by arm. Require
  brief minus comprehensive YES rate >= 0.20, fixed-horizon two-sided p < 0.05,
  and a positive 95% confidence interval. Planning rates 0.60/0.80 are assumptions.
- Also require >= 95% detail_variant_visible and changes_analyzed YES per arm,
  zero observed empty outputs on applicable ranges, and no observed failure or
  timeout increase (report counts and uncertainty). Report measured credits per
  assigned attempt, elapsed-duration distributions, coverage/failure modes, and
  working-set rebuild factor from recorded cache/rebuild evidence, or unavailable.
  Promote only when all criteria and evaluator consistency checks hold;
  otherwise record inconclusive. Later corrections are prospective only.
-->